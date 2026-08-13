package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"bounty/internal/devet"
	"bounty/internal/event"
)

// DeVETVerifier auto-verifies completed sub-agent results against the DeVET
// backend (P4-1). nil verifiers disable the hook entirely.
type DeVETVerifier interface {
	MirrorAndVerify(ctx context.Context, spec devet.MirrorSpec) (devet.VerifyDetail, error)
}

// verifySubagentResult mirrors the completed sub-agent run into DeVET and
// returns the 【DeVET 验证】 summary section (empty when no verifier is
// configured). It never blocks or fails the parent task: backend errors are
// reported as an honest "unverified" note.
func (a *Agent) verifySubagentResult(ctx context.Context, role, model, final string, childSess *Session) string {
	if a.devetVerifier == nil {
		return ""
	}

	msgs := childSess.Snapshot()
	toolCalls := 0
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolName != "" {
			toolCalls++
		}
	}
	written, _ := collectChildFiles(msgs)

	sum := sha256.Sum256([]byte(final))
	commitment := hex.EncodeToString(sum[:])

	hostEndpoint := a.provLabel
	if hostEndpoint == "" {
		hostEndpoint = "bounty.local"
	}
	modelLabel := model
	if modelLabel == "" {
		modelLabel = "parent-default"
	}

	spec := devet.MirrorSpec{
		HostName:     "bounty-host",
		HostEndpoint: hostEndpoint,
		Agents: []devet.MirrorAgent{{
			Name:             role + "-subagent",
			Endpoint:         hostEndpoint,
			Role:             role,
			Model:            modelLabel,
			ResultCommitment: commitment,
			ToolCalls:        toolCalls,
			WrittenFiles:     written,
		}},
	}

	verifyCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	detail, err := a.devetVerifier.MirrorAndVerify(verifyCtx, spec)

	// Emit the verification event for live frontends (web chain panel).
	if a.sink != nil {
		ev := &event.DeVETEvent{
			HostName:  spec.HostName,
			Authentic: err == nil && detail.Authentic,
			Fault:     detail.FaultType,
			Blame:     detail.BlamePath,
			Error:     detail.Error,
			Agents: []event.DeVETAgentEvent{{
				Name:         spec.Agents[0].Name,
				Role:         role,
				Model:        modelLabel,
				Commitment:   commitment,
				ToolCalls:    toolCalls,
				WrittenFiles: written,
				FaultType:    detail.FaultType,
			}},
		}
		if err != nil {
			ev.Error = err.Error()
		}
		a.sink.Emit(event.Event{Type: "devet_verify", Devet: ev})
	}

	var sb strings.Builder
	sb.WriteString("\n【DeVET 验证】\n")
	switch {
	case err != nil:
		sb.WriteString(fmt.Sprintf("- 状态：⚠️ 未验证（DeVET 后端不可用：%v）\n", err))
	case detail.Authentic:
		sb.WriteString("- 状态：✅ 真实有效（7 项递归检查全部通过）\n")
		sb.WriteString(fmt.Sprintf("- 承诺：sha256:%s…（子代理结论哈希，防伪造锚定）\n", commitment[:16]))
	default:
		sb.WriteString(fmt.Sprintf("- 状态：❌ 检出故障：%s\n", detail.FaultType))
		if len(detail.BlamePath) > 0 {
			sb.WriteString(fmt.Sprintf("- 归因：%s\n", strings.Join(detail.BlamePath, " → ")))
		}
		if detail.Error != "" {
			sb.WriteString(fmt.Sprintf("- 详情：%s\n", detail.Error))
		}
	}
	return sb.String()
}
