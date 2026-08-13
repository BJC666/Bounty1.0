package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"bounty/internal/event"
	"bounty/internal/provider"
	"bounty/internal/tool"
)

// cannedProvider streams a fixed sequence of events and records every request
// so tests can assert what was sent to the model.
type cannedProvider struct {
	events   []provider.StreamEvent
	ctxWin   int
	requests [][]provider.Message
}

func (c *cannedProvider) Stream(ctx context.Context, messages []provider.Message, tools []json.RawMessage, opts provider.StreamOpts) (<-chan provider.StreamEvent, error) {
	c.requests = append(c.requests, messages)
	ch := make(chan provider.StreamEvent, len(c.events)+1)
	for _, ev := range c.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (c *cannedProvider) ContextWindow() int { return c.ctxWin }

func (c *cannedProvider) Version() string { return "canned/1" }

// recordingSink captures emitted events.
type recordingSink struct {
	events []event.Event
}

func (r *recordingSink) Emit(ev event.Event) { r.events = append(r.events, ev) }

func newTestSession() *Session {
	return NewSession("system prompt")
}

func fillTurns(sess *Session, n int, prefix string) {
	for i := 0; i < n; i++ {
		sess.Add(provider.Message{Role: "user", Content: prefix + " user turn " + itoa(i)})
		sess.Add(provider.Message{Role: "assistant", Content: prefix + " assistant turn " + itoa(i)})
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func stubSummarizer(out string, captured *[]provider.Message, err error) Summarizer {
	return func(ctx context.Context, msgs []provider.Message) (string, error) {
		if captured != nil {
			*captured = append([]provider.Message{}, msgs...)
		}
		return out, err
	}
}

// P1-1 golden: 跨阈值才压缩，未达阈值绝不动会话、绝不调用摘要器。
func TestCompactNoOpBelowThreshold(t *testing.T) {
	sess := newTestSession()
	fillTurns(sess, 3, "small")
	before := sess.Snapshot()

	called := false
	a := New(nil, nil, sess, Options{Compact: &CompactConfig{
		MaxContext: 100000,
		Summarizer: stubSummarizer("x", nil, nil),
	}})
	_ = called
	res := a.compactWithConfig(context.Background(), sess, CompactConfig{
		MaxContext: 100000,
		Summarizer: func(ctx context.Context, msgs []provider.Message) (string, error) {
			called = true
			return "x", nil
		},
	})
	if res.Compacted {
		t.Fatal("compacted below threshold")
	}
	if called {
		t.Fatal("summarizer called below threshold")
	}
	after := sess.Snapshot()
	if len(after) != len(before) {
		t.Fatalf("session changed below threshold: %d -> %d", len(before), len(after))
	}
}

// P1-1 golden: 压缩后 = system + 模型摘要 + 尾部原文；摘要器只拿到被丢弃的
// 部分（含早期关键事实），尾部消息原样保留。
func TestCompactSummarizesDroppedAndKeepsTail(t *testing.T) {
	sess := newTestSession()
	sess.Add(provider.Message{Role: "user", Content: "请修复登录页的越权漏洞，用户 id 可以伪造"})
	sess.Add(provider.Message{Role: "assistant", Content: "好的，先读 auth.go"})
	sess.Add(provider.Message{Role: "tool", Content: "auth.go 内容...", ToolName: "read_file"})
	fillTurns(sess, 5, "mid")
	sess.Add(provider.Message{Role: "user", Content: "最后一步：补充单元测试"})
	sess.Add(provider.Message{Role: "assistant", Content: "正在写测试"})

	before := sess.Snapshot()
	var captured []provider.Message
	a := New(nil, nil, sess, Options{})
	res := a.compactWithConfig(context.Background(), sess, CompactConfig{
		MaxContext: 100, // force compaction: estimate > 80
		TailTokens: 60,  // small tail so the middle gets summarized
		Summarizer: stubSummarizer("## 任务目标\n修登录越权", &captured, nil),
	})
	if !res.Compacted {
		t.Fatal("expected compaction")
	}
	if res.UsedFallback {
		t.Fatal("unexpected fallback")
	}
	if res.AfterTokens >= res.BeforeTokens {
		t.Fatalf("tokens did not shrink: %d -> %d", res.BeforeTokens, res.AfterTokens)
	}

	joined := joinContents(captured)
	if !strings.Contains(joined, "越权漏洞") {
		t.Fatal("dropped messages given to summarizer must contain the early user goal")
	}
	if strings.Contains(joined, "补充单元测试") {
		t.Fatal("tail messages must not be sent to the summarizer")
	}

	after := sess.Snapshot()
	if after[0].Role != "system" || after[0].Content != "system prompt" {
		t.Fatalf("system prompt lost: %+v", after[0])
	}
	if !strings.Contains(after[1].Content, "[早前对话已摘要") || !strings.Contains(after[1].Content, "修登录越权") {
		t.Fatalf("summary message wrong: %q", after[1].Content)
	}
	// tail = last 2 messages of before (unchanged)
	wantTail := before[len(before)-2:]
	if len(after)-2 != len(wantTail) {
		t.Fatalf("tail size = %d, want %d", len(after)-2, len(wantTail))
	}
	for i, m := range wantTail {
		if !reflect.DeepEqual(after[2+i], m) {
			t.Fatalf("tail[%d] changed: %+v vs %+v", i, after[2+i], m)
		}
	}
}

// P1-1 golden: 强制压缩只保留最近 2 条，其余全部进摘要。
func TestForceCompactKeepsTwoTailMessages(t *testing.T) {
	sess := newTestSession()
	fillTurns(sess, 10, "long conversation about refactoring")
	var captured []provider.Message
	a := New(nil, nil, sess, Options{})
	res := a.compactWithConfig(context.Background(), sess, CompactConfig{
		MaxContext: 100,
		ForceRatio: 0.2, // estimate > 20 -> force path
		Ratio:      0.8,
		TailTokens: 1000,
		Summarizer: stubSummarizer("summary", &captured, nil),
	})
	if !res.Compacted {
		t.Fatal("expected compaction")
	}
	after := sess.Snapshot()
	if len(after) != 4 { // system + summary + 2 tail
		t.Fatalf("force compact should keep system+summary+2 tail, got %d", len(after))
	}
	if res.DroppedMessages != len(captured) {
		t.Fatalf("dropped=%d but summarizer saw %d", res.DroppedMessages, len(captured))
	}
}

// P1-1 golden: 摘要器失败时回退到占位丢弃，保证上下文仍然收缩、不崩溃。
func TestSummarizerErrorFallsBackToDrop(t *testing.T) {
	sess := newTestSession()
	fillTurns(sess, 10, "data")
	a := New(nil, nil, sess, Options{})
	res := a.compactWithConfig(context.Background(), sess, CompactConfig{
		MaxContext: 100,
		TailTokens: 60,
		Summarizer: stubSummarizer("", nil, errors.New("model timeout")),
	})
	if !res.Compacted || !res.UsedFallback {
		t.Fatalf("expected fallback compaction, got %+v", res)
	}
	after := sess.Snapshot()
	if !strings.Contains(after[1].Content, "dropped due to context length limits") {
		t.Fatalf("fallback placeholder missing: %q", after[1].Content)
	}
	if len(after) >= 20 {
		t.Fatal("fallback must still shrink the session")
	}
}

// P1-1 golden: 默认摘要器通过 provider 调用模型；请求必须包含五个小节与
// 被压缩的对话内容。
func TestDefaultSummarizerCallsProvider(t *testing.T) {
	prov := &cannedProvider{events: []provider.StreamEvent{
		{Delta: &provider.Delta{Content: "## 任务目标\n"}},
		{Delta: &provider.Delta{Content: "修 bug"}},
		{Done: true},
	}}
	sess := newTestSession()
	sess.Add(provider.Message{Role: "user", Content: "目标：修复订单模块的金额计算"})
	a := New(prov, nil, sess, Options{})

	summary, err := a.defaultSummarizer(context.Background(), []provider.Message{
		{Role: "user", Content: "目标：修复订单模块的金额计算"},
		{Role: "assistant", Content: "好的"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary != "## 任务目标\n修 bug" {
		t.Fatalf("summary = %q", summary)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("requests = %d", len(prov.requests))
	}
	req := joinContents(prov.requests[0])
	for _, section := range []string{"## 任务目标", "## 已做决策", "## 关键文件改动", "## 未完成事项", "## 错误教训"} {
		if !strings.Contains(req, section) {
			t.Fatalf("summary request missing section %q", section)
		}
	}
	if !strings.Contains(req, "订单模块的金额计算") {
		t.Fatal("summary request must include the dropped conversation")
	}
}

// P1-1 golden: MaxContext 缺省时读 provider 的 ContextWindow。
func TestEffectiveConfigReadsProviderContextWindow(t *testing.T) {
	prov := &cannedProvider{ctxWin: 12345}
	sess := newTestSession()
	a := New(prov, nil, sess, Options{})
	cfg := a.effectiveCompactConfig()
	if cfg.MaxContext != 12345 {
		t.Fatalf("MaxContext = %d, want provider window 12345", cfg.MaxContext)
	}
	// explicit config wins over the provider window
	a2 := New(prov, nil, sess, Options{Compact: &CompactConfig{MaxContext: 999}})
	if got := a2.effectiveCompactConfig().MaxContext; got != 999 {
		t.Fatalf("explicit MaxContext = %d, want 999", got)
	}
}

// P1-1 golden: 压缩触发 PreCompact hook 并发出通知事件。
func TestCompactFiresHookAndNotification(t *testing.T) {
	sess := newTestSession()
	fillTurns(sess, 10, "data")
	var hookTokens, hookDropped int
	hook := compactHookFunc(func(ctx context.Context, tokens int, dropped int) error {
		hookTokens, hookDropped = tokens, dropped
		return nil
	})
	sink := &recordingSink{}
	a := New(nil, nil, sess, Options{Sink: sink, Hooks: hook})
	res := a.compactWithConfig(context.Background(), sess, CompactConfig{
		MaxContext: 100,
		TailTokens: 60,
		Summarizer: stubSummarizer("summary", nil, nil),
	})
	if !res.Compacted {
		t.Fatal("expected compaction")
	}
	if hookTokens <= 0 || hookDropped <= 0 {
		t.Fatalf("PreCompact hook not fired: tokens=%d dropped=%d", hookTokens, hookDropped)
	}
	found := false
	for _, ev := range sink.events {
		if ev.Type == "notification" && strings.Contains(ev.TextDelta, "compacted") {
			found = true
		}
	}
	if !found {
		t.Fatal("compaction notification not emitted")
	}
}

type compactHookFunc func(ctx context.Context, tokens int, dropped int) error

func (f compactHookFunc) PreCompact(ctx context.Context, tokens int, dropped int) error {
	return f(ctx, tokens, dropped)
}

func (f compactHookFunc) PreToolUse(ctx context.Context, name string, args json.RawMessage) error {
	return nil
}

func (f compactHookFunc) PostToolUse(ctx context.Context, name string, result string, execErr error) {
}

// P1-1 验收: 10 个 200 轮模拟长会话，压缩后关键事实不丢、token 下降、二次
// 压缩幂等。
func TestCompactionPreservesCriticalFactsAcross200TurnSessions(t *testing.T) {
	for seed := 0; seed < 10; seed++ {
		sess := newTestSession()
		sess.Add(provider.Message{Role: "user", Content: "目标(seed " + itoa(seed) + ")：把支付服务从 REST 迁移到 gRPC"})
		sess.Add(provider.Message{Role: "assistant", Content: "先盘点现有接口"})
		for i := 0; i < 198; i++ {
			sess.Add(provider.Message{Role: "user", Content: "继续第 " + itoa(i) + " 步"})
			sess.Add(provider.Message{Role: "assistant", Content: "完成第 " + itoa(i) + " 步，改动 proto 文件"})
		}
		sess.Add(provider.Message{Role: "user", Content: "最后帮我写迁移检查清单"})
		sess.Add(provider.Message{Role: "assistant", Content: "好的"})

		var captured []provider.Message
		a := New(nil, nil, sess, Options{})
		res := a.compactWithConfig(context.Background(), sess, CompactConfig{
			MaxContext: 4000,
			TailTokens: 400,
			Summarizer: stubSummarizer("## 任务目标\ngRPC 迁移", &captured, nil),
		})
		if !res.Compacted {
			t.Fatalf("seed %d: 200-turn session did not compact", seed)
		}
		if res.AfterTokens >= res.BeforeTokens {
			t.Fatalf("seed %d: tokens did not shrink %d -> %d", seed, res.BeforeTokens, res.AfterTokens)
		}
		joined := joinContents(captured)
		if !strings.Contains(joined, "gRPC") {
			t.Fatalf("seed %d: 原始任务目标在压缩时丢失", seed)
		}
		if !strings.Contains(joined, "proto 文件") {
			t.Fatalf("seed %d: 关键决策/改动在压缩时丢失", seed)
		}

		// 幂等: 压缩后已在阈值之下，再次压缩应 no-op。
		second := a.compactWithConfig(context.Background(), sess, CompactConfig{
			MaxContext: 4000,
			TailTokens: 400,
			Summarizer: stubSummarizer("again", nil, nil),
		})
		if second.Compacted {
			t.Fatalf("seed %d: second compaction should be no-op", seed)
		}
	}
}

// 缓存形状: 正常追加视为命中，压缩改写中间历史视为未命中。
func TestCachedPrefixHitSemantics(t *testing.T) {
	sys := []provider.Message{{Role: "system", Content: "sys"}}
	prev := append(append([]provider.Message{}, sys...),
		provider.Message{Role: "user", Content: "u1"},
		provider.Message{Role: "assistant", Content: "a1"},
		provider.Message{Role: "tool", Content: "r1"},
	)
	// append-only: previous cached region is a prefix of the current request
	currAppend := append(append([]provider.Message{}, prev...),
		provider.Message{Role: "assistant", Content: "a2"},
	)
	if !cachedPrefixHit(prev, currAppend) {
		t.Fatal("append-only turn must be a cache hit")
	}
	// compaction: middle rewritten
	currCompact := append([]provider.Message{}, sys...)
	currCompact = append(currCompact,
		provider.Message{Role: "user", Content: "[summary]"},
		provider.Message{Role: "user", Content: "u2"},
		provider.Message{Role: "assistant", Content: "a2"},
	)
	if cachedPrefixHit(prev, currCompact) {
		t.Fatal("compacted history must be a cache miss")
	}
	// shrink: shorter current prefix must be a miss
	if cachedPrefixHit(prev, sys) {
		t.Fatal("shorter prefix must be a cache miss")
	}
}

// cachedPrefixHit reports whether the previous request's cached region (all
// messages except the last) is a prefix of the current request's cached region.
func cachedPrefixHit(prev, curr []provider.Message) bool {
	prevRegion := prev[:len(prev)-1]
	currRegion := curr[:len(curr)-1]
	if len(prevRegion) > len(currRegion) {
		return false
	}
	return provider.HashMessages(currRegion[:len(prevRegion)]) == provider.HashMessages(prevRegion)
}

func joinContents(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// bigTool returns a very long result so a few turns push the estimated token
// count past the compaction threshold.
type bigTool struct{}

func (bigTool) Name() string            { return "big" }
func (bigTool) Description() string     { return "returns a big result" }
func (bigTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (bigTool) ReadOnly() bool          { return false }
func (bigTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return strings.Repeat("x", 4000), nil
}

// loopProvider answers big-tool turns until the quota runs out, then answers
// with a final text; summary requests (single user message with the
// compression prompt) get a canned structured summary.
type loopProvider struct {
	quota       int
	summarySeen int
	lastMain    []provider.Message
}

func (l *loopProvider) Stream(ctx context.Context, messages []provider.Message, tools []json.RawMessage, opts provider.StreamOpts) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 4)
	if len(messages) == 1 && strings.Contains(messages[0].Content, "压缩成结构化中文摘要") {
		l.summarySeen++
		ch <- provider.StreamEvent{Delta: &provider.Delta{Content: "S: 任务目标=迁移到 gRPC"}}
		ch <- provider.StreamEvent{Done: true}
		close(ch)
		return ch, nil
	}
	l.lastMain = messages
	if l.quota > 0 {
		l.quota--
		ch <- provider.StreamEvent{Delta: &provider.Delta{ToolCalls: []provider.ToolCallDelta{
			{ID: "call-big", Name: "big", ArgsDelta: "{}"},
		}}}
		ch <- provider.StreamEvent{Done: true}
	} else {
		ch <- provider.StreamEvent{Delta: &provider.Delta{Content: "全部完成"}}
		ch <- provider.StreamEvent{Done: true}
	}
	close(ch)
	return ch, nil
}

func (l *loopProvider) ContextWindow() int { return 0 }
func (l *loopProvider) Version() string    { return "loop/1" }

// P1-1 端到端: 在真实 Run 循环中上下文越阈值 -> 默认摘要器经 provider 生成
// 摘要 -> 会话重建为 system+摘要+尾部 -> 后续回合继续正常完成。
func TestRunCompactsWithProviderSummaryEndToEnd(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(bigTool{})
	prov := &loopProvider{quota: 5}
	sess := NewSession("system prompt")
	a := New(prov, reg, sess, Options{
		Gate: fakeGate{dec: Allow},
		Compact: &CompactConfig{
			MaxContext: 8000,
			Ratio:      0.8,
			ForceRatio: 0.9,
			TailTokens: 300,
		},
	})
	if err := a.Run(context.Background(), "请执行大任务"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if prov.summarySeen == 0 {
		t.Fatal("default summarizer never invoked the provider")
	}
	found := false
	for _, m := range sess.Snapshot() {
		if strings.Contains(m.Content, "[早前对话已摘要") && strings.Contains(m.Content, "S: 任务目标") {
			found = true
		}
	}
	if !found {
		t.Fatalf("rebuilt session missing model summary: %+v", sess.Snapshot())
	}
	// 压缩改写了历史前缀 -> 缓存命中统计必须记一次 miss。
	if a.CacheStats().CacheMisses == 0 {
		t.Fatal("compaction must surface as a cache miss")
	}
	last := sess.Snapshot()[len(sess.Snapshot())-1]
	if last.Role != "assistant" || last.Content != "全部完成" {
		t.Fatalf("final message = %+v", last)
	}
}

func TestForceCompactMethodCompactsBelowThreshold(t *testing.T) {
	sess := newTestSession()
	big := strings.Repeat("长", 4000)
	for i := 0; i < 6; i++ {
		sess.Add(provider.Message{Role: "user", Content: big})
		sess.Add(provider.Message{Role: "assistant", Content: big})
	}
	before := len(sess.Snapshot())

	prov := &cannedProvider{events: []provider.StreamEvent{
		{Delta: &provider.Delta{Content: "摘要内容"}},
		{Done: true},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{})
	if err := a.ForceCompact(); err != nil {
		t.Fatalf("ForceCompact: %v", err)
	}
	after := len(sess.Snapshot())
	if after >= before {
		t.Fatalf("messages after ForceCompact = %d, want fewer than %d", after, before)
	}
	msgs := sess.Snapshot()
	if msgs[0].Role != "system" {
		t.Fatalf("first message must stay system, got %s", msgs[0].Role)
	}
	joined := ""
	for _, m := range msgs {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "摘要内容") {
		t.Fatalf("summary missing from compacted session:\n%s", joined)
	}
}

func TestForceCompactTooFewMessages(t *testing.T) {
	sess := newTestSession()
	sess.Add(provider.Message{Role: "user", Content: "hi"})
	a := New(&cannedProvider{}, tool.NewRegistry(), sess, Options{})
	if err := a.ForceCompact(); err == nil {
		t.Fatal("want error when there is nothing to compact")
	}
}
