package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bounty/internal/event"
	"bounty/internal/provider"
)

const (
	charsPerToken          = 4
	defaultTailTokens      = 16384
	minTailMessages        = 2
	defaultMaxContext      = 200000
	defaultMaxSummaryToken = 4096
	summaryCallTimeout     = 90 * time.Second
	maxDumpedRunes         = 1500 // per-message cap in the summary request
)

// Summarizer turns dropped conversation history into a compact structured
// summary. The default implementation calls the active provider; tests and
// custom integrations may inject their own.
type Summarizer func(ctx context.Context, msgs []provider.Message) (string, error)

// CompactHooks receives a notification right before a compaction rewrites the
// conversation. Implemented by hookAdapter in boot to fire hook.PreCompact.
type CompactHooks interface {
	PreCompact(ctx context.Context, tokens int, dropped int) error
}

// CompactConfig holds compaction thresholds. Zero fields fall back to the
// defaults below; MaxContext==0 means "read the provider context window".
type CompactConfig struct {
	SoftRatio        float64
	Ratio            float64
	ForceRatio       float64
	TailTokens       int
	MaxContext       int
	MaxSummaryTokens int
	Summarizer       Summarizer
	SummaryPrompt    string
}

func defaultCompactConfig() CompactConfig {
	return CompactConfig{
		SoftRatio:        0.5,
		Ratio:            0.8,
		ForceRatio:       0.9,
		TailTokens:       defaultTailTokens,
		MaxContext:       defaultMaxContext,
		MaxSummaryTokens: defaultMaxSummaryToken,
	}
}

// CompactResult describes what a compaction did (used by tests and telemetry).
type CompactResult struct {
	Compacted       bool
	BeforeTokens    int
	AfterTokens     int
	DroppedMessages int
	SummaryTokens   int
	UsedFallback    bool
}

// estimateTokens provides a rough token count (~4 ASCII chars per token,
// ~1 CJK char per token). Uses rune-level iteration for accurate CJK counting.
func estimateTokens(msgs []provider.Message) int {
	total := 0
	for _, m := range msgs {
		total += 4 // per-message overhead
		runes := []rune(m.Content)
		for _, r := range runes {
			if r <= 0x7F {
				total++
			} else {
				total += 2 // CJK and other multibyte
			}
		}
		total += len(runes) / 4 // blended estimate
		total += len(m.ToolName) / 4
		for _, tc := range m.ToolCalls {
			total += len(tc.Name)/4 + len(tc.Args)/4
		}
	}
	if total == 0 {
		total = 1
	}
	return total
}

// mergeCompactConfig overlays non-zero fields of over onto base.
func mergeCompactConfig(base, over CompactConfig) CompactConfig {
	if over.SoftRatio > 0 {
		base.SoftRatio = over.SoftRatio
	}
	if over.Ratio > 0 {
		base.Ratio = over.Ratio
	}
	if over.ForceRatio > 0 {
		base.ForceRatio = over.ForceRatio
	}
	if over.TailTokens > 0 {
		base.TailTokens = over.TailTokens
	}
	if over.MaxContext > 0 {
		base.MaxContext = over.MaxContext
	}
	if over.MaxSummaryTokens > 0 {
		base.MaxSummaryTokens = over.MaxSummaryTokens
	}
	if over.Summarizer != nil {
		base.Summarizer = over.Summarizer
	}
	if over.SummaryPrompt != "" {
		base.SummaryPrompt = over.SummaryPrompt
	}
	return base
}

// effectiveCompactConfig merges Options.Compact over the defaults and resolves
// MaxContext from the provider context window when not explicitly configured.
func (a *Agent) effectiveCompactConfig() CompactConfig {
	cfg := defaultCompactConfig()
	explicitMax := false
	if a.compactCfg != nil {
		cfg = mergeCompactConfig(cfg, *a.compactCfg)
		explicitMax = a.compactCfg.MaxContext > 0
	}
	if !explicitMax {
		if cw := a.provider().ContextWindow(); cw > 0 {
			cfg.MaxContext = cw
		}
	}
	return cfg
}

func (a *Agent) maybeCompact(ctx context.Context, sess *Session) {
	a.compactWithConfig(ctx, sess, a.effectiveCompactConfig())
}

// compactWithConfig rewrites the session as system + model-generated summary +
// tail when the estimated token count crosses the threshold. On summarizer
// failure it falls back to the legacy placeholder-drop so the context still
// shrinks.
func (a *Agent) compactWithConfig(ctx context.Context, sess *Session, cfg CompactConfig) CompactResult {
	res := CompactResult{}
	cfg = mergeCompactConfig(defaultCompactConfig(), cfg)
	msgs := sess.Snapshot()
	if len(msgs) <= 2 {
		return res
	}

	tokens := estimateTokens(msgs)
	res.BeforeTokens = tokens
	threshold := int(float64(cfg.MaxContext) * cfg.Ratio)
	forceThreshold := int(float64(cfg.MaxContext) * cfg.ForceRatio)

	if tokens < threshold {
		return res
	}

	// PreCompact hook notification (fire-and-forget semantics).
	if hooks, ok := a.hooks.(CompactHooks); ok {
		_ = hooks.PreCompact(ctx, tokens, len(msgs)-1)
	}

	var tail []provider.Message
	if tokens >= forceThreshold {
		// Force path: keep only the last 2 messages so the context shrinks hard.
		tail = msgs[len(msgs)-2:]
	} else {
		tail = tailMessages(msgs, cfg.TailTokens, minTailMessages)
	}
	if len(tail) >= len(msgs)-1 {
		return res // nothing to compact
	}

	dropped := msgs[1 : len(msgs)-len(tail)]
	summaryText, usedFallback := a.buildSummary(ctx, dropped, cfg)

	newMsgs := make([]provider.Message, 0, len(tail)+2)
	newMsgs = append(newMsgs, msgs[0]) // system prompt (immutable, cache-friendly)
	newMsgs = append(newMsgs, provider.Message{Role: "user", Content: summaryText})
	newMsgs = append(newMsgs, tail...)
	sess.ReplaceMessages(newMsgs)

	res.Compacted = true
	res.AfterTokens = estimateTokens(newMsgs)
	res.DroppedMessages = len(dropped)
	res.SummaryTokens = estimateTokens([]provider.Message{{Role: "user", Content: summaryText}})
	res.UsedFallback = usedFallback

	a.sink.Emit(event.Event{Type: "notification", TextDelta: fmt.Sprintf(
		"上下文已压缩（Context compacted）: %d -> %d tokens, %d 条消息生成摘要, 保留最近 %d 条",
		tokens, res.AfterTokens, len(dropped), len(tail))})
	return res
}

// buildSummary runs the summarizer (default: the active model) over the
// dropped messages and bounds the result to MaxSummaryTokens.
func (a *Agent) buildSummary(ctx context.Context, dropped []provider.Message, cfg CompactConfig) (string, bool) {
	sum := cfg.Summarizer
	if sum == nil {
		sum = a.defaultSummarizer
	}
	text, err := sum(ctx, dropped)
	if err != nil || strings.TrimSpace(text) == "" {
		return "[Earlier conversation was dropped due to context length limits. Key information from earlier turns may be lost.]", true
	}
	text = "[早前对话已摘要（summary of earlier conversation）]\n" + text
	if estimateTokens([]provider.Message{{Role: "user", Content: text}}) > cfg.MaxSummaryTokens {
		text = truncateToTokenBudget(text, cfg.MaxSummaryTokens)
	}
	return text, false
}

// defaultSummarizer asks the active provider to summarize the dropped history
// using the structured five-section prompt.
func (a *Agent) defaultSummarizer(ctx context.Context, msgs []provider.Message) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, summaryCallTimeout)
	defer cancel()

	ch, err := a.provider().Stream(ctx, []provider.Message{
		{Role: "user", Content: buildSummaryRequest(msgs)},
	}, nil, provider.StreamOpts{Temperature: 0})
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for ev := range ch {
		if ev.Err != nil {
			return b.String(), ev.Err
		}
		if ev.Delta != nil && ev.Delta.Content != "" {
			b.WriteString(ev.Delta.Content)
		}
		if ev.Done {
			break
		}
	}
	return b.String(), nil
}

// buildSummaryRequest renders the dropped messages into the structured
// five-section summarization prompt. Long tool outputs are truncated per
// message so the summary call stays bounded.
func buildSummaryRequest(msgs []provider.Message) string {
	var b strings.Builder
	b.WriteString("把下面这段对话压缩成结构化中文摘要，只输出摘要本身，不要解释、不要客套。必须包含以下五个小节，信息不足的小节写「无」：\n")
	b.WriteString("## 任务目标\n## 已做决策\n## 关键文件改动\n## 未完成事项\n## 错误教训\n\n")
	b.WriteString("对话（按时间顺序）：\n")
	for _, m := range msgs {
		label := m.Role
		if m.Role == "tool" && m.ToolName != "" {
			label = "tool(" + m.ToolName + ")"
		}
		content := m.Content
		if r := []rune(content); len(r) > maxDumpedRunes {
			content = string(r[:maxDumpedRunes]) + "...[截断]"
		}
		b.WriteString(fmt.Sprintf("[%s] %s\n", label, content))
	}
	return b.String()
}

// truncateToTokenBudget cuts the summary down until it fits the token budget.
// It always terminates: every iteration removes at least one rune.
func truncateToTokenBudget(text string, maxTokens int) string {
	for estimateTokens([]provider.Message{{Role: "user", Content: text}}) > maxTokens {
		r := []rune(text)
		if len(r) <= 1 {
			return text
		}
		cut := len(r) / 4
		if cut < 1 {
			cut = 1
		}
		text = string(r[:len(r)-cut]) + "\n[摘要过长，已截断]"
	}
	return text
}

// tailMessages returns the last messages that fit within tokenBudget, keeping at
// least minMessages (excluding the system prompt at index 0).
func tailMessages(msgs []provider.Message, tokenBudget int, minMessages int) []provider.Message {
	if len(msgs) <= minMessages+1 {
		return msgs[1:] // exclude system prompt
	}

	tokens := 0
	end := len(msgs) - 1
	start := end
	for ; start >= 1; start-- {
		msgTokens := estimateTokens([]provider.Message{msgs[start]})
		if tokens+msgTokens > tokenBudget && (end-start) >= minMessages {
			break
		}
		tokens += msgTokens
	}
	return msgs[start+1:]
}

// SoftCompactNotice returns a warning if context is above the soft threshold.
func SoftCompactNotice(tokens int, maxContext int, softRatio float64) string {
	threshold := int(float64(maxContext) * softRatio)
	if tokens > threshold {
		used := float64(tokens) / float64(maxContext) * 100
		return fmt.Sprintf("Context is %.0f%% full (%d/%d tokens). Consider starting a new session or compacting.",
			used, tokens, maxContext)
	}
	return ""
}
