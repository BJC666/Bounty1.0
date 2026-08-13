package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bounty/internal/provider"
	"bounty/internal/tool"
	"bounty/internal/provider/openai"
	"bounty/internal/tool/builtin"
)

// ── P7-4b: 子代理摘要 token before/after ──
//
// 验收口径（P3-4）：结构化摘要（结论/证据/文件清单）相对子代理原始输出
// 的 token 应下降 >=50%。以下两个测试分别用离线构造数据与真实模型各验证一次。

// TestSubagentSummaryTokenRatioUnit 离线单测：长输出（ASCII 密集与 CJK 密集各一）
// 经 buildSubagentSummary 后 token 估算均须 <= 原始输出的 50%。
func TestSubagentSummaryTokenRatioUnit(t *testing.T) {
	cases := []struct {
		name  string
		final string
	}{
		{"ascii-long", strings.Repeat("The quick brown fox jumps over the lazy dog and keeps running through the codebase. ", 300)},
		{"cjk-long", strings.Repeat("中文超长报告：我们详细分析了系统架构、委托链验证流程与实现细节，并给出了完整的逐步解释和证据清单。", 300)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sess := NewSession("child")
			sess.Add(provider.Message{Role: "assistant", Content: c.final})
			summary := buildSubagentSummary(c.final, sess)
			rawTok := estimateTokens([]provider.Message{{Role: "assistant", Content: c.final}})
			sumTok := estimateTokens([]provider.Message{{Role: "assistant", Content: summary}})
			ratio := float64(sumTok) / float64(rawTok)
			t.Logf("raw=%d tok (%d runes), summary=%d tok, ratio=%.1f%%", rawTok, len([]rune(c.final)), sumTok, ratio*100)
			if ratio > 0.50 {
				t.Fatalf("summary token ratio = %.1f%%, want <= 50%% (raw=%d, summary=%d)", ratio*100, rawTok, sumTok)
			}
		})
	}
}

// TestSubagentSummaryTokenRatioRealModel 真实模型验证：门控环境变量
// BOUNTY_REAL_TOKEN_TEST=1 时运行（需要 QWEN_TOKENPLAN_API_KEY）。
// 在 go-todo fixture 中派一个 explore 子代理输出超长报告，对比其原始
// 输出与结构化摘要的 token 估算，并把证据写入 docs/eval/p7-4-subagent-token.txt。
func TestSubagentSummaryTokenRatioRealModel(t *testing.T) {
	if os.Getenv("BOUNTY_REAL_TOKEN_TEST") == "" {
		t.Skip("set BOUNTY_REAL_TOKEN_TEST=1 to run the real-model check")
	}
	key := os.Getenv("QWEN_TOKENPLAN_API_KEY")
	if key == "" {
		t.Fatal("QWEN_TOKENPLAN_API_KEY not set")
	}
	baseURL := "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode"
	prov := openai.New(baseURL, key, "qwen3.8-max", 128000)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	reg := tool.NewRegistry()
	reg.Add(&builtin.ReadFileTool{})
	reg.Add(&builtin.GlobTool{})
	reg.Add(&builtin.GrepTool{})
	reg.Add(&builtin.CodeIndexTool{})

	parent := New(prov, reg, NewSession("parent"), Options{MaxSteps: 20, Gate: fakeGate{dec: Allow}})

	fixture, err := filepath.Abs(filepath.Join("..", "..", "scripts", "eval", "fixtures", "go-todo"))
	if err != nil {
		t.Fatal(err)
	}
	evidenceAbs, err := filepath.Abs(filepath.Join("..", "..", "docs", "eval", "p7-4-subagent-token.txt"))
	if err != nil {
		t.Fatal(err)
	}
	oldWD, _ := os.Getwd()
	if err := os.Chdir(fixture); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)

	childSystem := buildChildSystemPrompt("explore", true, nil)
	childSession := NewSession(childSystem)
	taskPrompt := "请用只读工具调研本仓库，输出一份超长中文报告：把 README.md 与 main.go 的每一节都翻译成中文并展开解读，包含每个函数签名、参数与返回值的完整解释，报告正文不少于 3000 字。"
	childSession.Add(provider.Message{Role: "user", Content: taskPrompt})
	childReg := SubagentToolRegistry(parent.tools, true)
	child := New(prov, childReg, childSession, Options{
		MaxSteps:    15,
		Temperature: 0.3,
		Gate:        fakeGate{dec: Allow},
	})
	if err := child.Run(ctx, taskPrompt); err != nil {
		t.Fatalf("child run: %v", err)
	}

	final := lastAssistantText(childSession)
	summary := buildSubagentSummary(final, childSession)
	rawTok := estimateTokens([]provider.Message{{Role: "assistant", Content: final}})
	sumTok := estimateTokens([]provider.Message{{Role: "assistant", Content: summary}})
	ratio := float64(sumTok) / float64(rawTok)
	t.Logf("child final raw=%d tok (%d runes), summary=%d tok, ratio=%.1f%%", rawTok, len([]rune(final)), sumTok, ratio*100)

	evidence := "P7-4b real-model subagent summary token before/after (qwen/qwen3.8-max)\n" +
		"child raw output: " + itoa(rawTok) + " tok (" + itoa(len([]rune(final))) + " runes)\n" +
		"structured summary: " + itoa(sumTok) + " tok\n" +
		"ratio: " + ftos(ratio) + "\n" +
		"--- raw final head ---\n" + truncRunes(final, 600) + "\n" +
		"--- summary ---\n" + summary + "\n"
	if err := os.WriteFile(evidenceAbs, []byte(evidence), 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	t.Logf("evidence saved: %s", evidenceAbs)

	if ratio > 0.50 {
		t.Fatalf("summary token ratio = %.1f%%, want <= 50%% (raw=%d, summary=%d)", ratio*100, rawTok, sumTok)
	}
}

func ftos(f float64) string { return fmt.Sprintf("%.1f%%", f*100) }
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
