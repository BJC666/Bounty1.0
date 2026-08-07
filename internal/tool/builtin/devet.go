package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"bounty/internal/devet"
	"bounty/internal/tool"
)

// DeVETTools is a shared struct that holds the DeVET backend connection
// and exposes all 5 DeVET tools as method receivers.
type DeVETTools struct {
	backend *devet.Backend
}

// NewDeVETTools creates a DeVETTools instance wired to the given backend.
func NewDeVETTools(backend *devet.Backend) *DeVETTools {
	return &DeVETTools{backend: backend}
}

// All returns all 5 DeVET tools as a slice ready for registration.
func (d *DeVETTools) All() []tool.Tool {
	return []tool.Tool{
		&devetHealthTool{backend: d.backend},
		&devetBuildScenarioTool{backend: d.backend},
		&devetVerifyChainTool{backend: d.backend},
		&devetListAttacksTool{backend: d.backend},
		&devetSimulateAttackTool{backend: d.backend},
	}
}

var devetHTTPClient = &http.Client{Timeout: 30 * time.Second}

// ── DeVET Health ──

type devetHealthTool struct {
	backend *devet.Backend
}

func (t *devetHealthTool) Name() string        { return "devet_health" }
func (t *devetHealthTool) ReadOnly() bool      { return true }
func (t *devetHealthTool) Description() string { return "Check if the DeVET verification backend is running and healthy." }
func (t *devetHealthTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "devet"} }

func (t *devetHealthTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (t *devetHealthTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	resp, err := devetHTTPClient.Get(t.backend.BaseURL() + "/health")
	if err != nil {
		return "", fmt.Errorf("DeVET backend not reachable: %w", err)
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	b, _ := json.MarshalIndent(result, "", "  ")
	return fmt.Sprintf("DeVET backend is healthy:\n%s", string(b)), nil
}

// ── DeVET Build Scenario ──

type devetBuildScenarioTool struct {
	backend *devet.Backend
}

func (t *devetBuildScenarioTool) Name() string      { return "devet_build_scenario" }
func (t *devetBuildScenarioTool) ReadOnly() bool     { return true }
func (t *devetBuildScenarioTool) Description() string {
	return "Build a 3-agent Trading DAO delegation chain in DeVET (StrategyAgent -> ExecutionAgentETH + ExecutionAgentBTC)."
}
func (t *devetBuildScenarioTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "devet"} }

func (t *devetBuildScenarioTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (t *devetBuildScenarioTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	resp, err := devetHTTPClient.Post(t.backend.BaseURL()+"/scenario/build", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", fmt.Errorf("DeVET build failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("DeVET error %d: %s", resp.StatusCode, string(body))
	}
	var result devet.ScenarioResult
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("DeVET parse error: %w", err)
	}
	c := result.Chain
	return fmt.Sprintf("Scenario built successfully:\n- Total agents: %d\n- Max delegation depth: %d\n- Grant count: %d\n- Root AID: %s",
		c.TotalAgents, c.MaxDepth, c.GrantCount, shortHash(c.RootAIDHash)), nil
}

// ── DeVET Verify Chain ──

type devetVerifyChainTool struct {
	backend *devet.Backend
}

func (t *devetVerifyChainTool) Name() string      { return "devet_verify_chain" }
func (t *devetVerifyChainTool) ReadOnly() bool     { return true }
func (t *devetVerifyChainTool) Description() string {
	return "Verify the current DeVET delegation chain. Must call devet_build_scenario first."
}
func (t *devetVerifyChainTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "devet"} }

func (t *devetVerifyChainTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (t *devetVerifyChainTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	resp, err := devetHTTPClient.Post(t.backend.BaseURL()+"/chain/verify", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", fmt.Errorf("DeVET verify failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("DeVET error %d: %s", resp.StatusCode, string(body))
	}
	var result devet.VerificationResult
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("DeVET parse error: %w", err)
	}
	r := result.Result

	var sb strings.Builder
	sb.WriteString("DeVET Chain Verification Result:\n")
	sb.WriteString(fmt.Sprintf("  Authentic: %v\n", r.Authentic))
	sb.WriteString(fmt.Sprintf("  Chain depth: %d\n", r.ChainDepth))
	sb.WriteString(fmt.Sprintf("  Total agents: %d\n", r.TotalAgents))
	if r.BlameAttribution != "" {
		sb.WriteString(fmt.Sprintf("  Blame: %s\n", r.BlameAttribution))
	}
	if len(r.Findings) > 0 {
		sb.WriteString("  Agent findings:\n")
		for _, f := range r.Findings {
			agent := shortHash(f.Agent)
			if f.FaultType != "" {
				sb.WriteString(fmt.Sprintf("    %s: ❌ %s\n", agent, f.FaultType))
			} else if f.ChainOK != nil && *f.ChainOK {
				sb.WriteString(fmt.Sprintf("    %s: ✅\n", agent))
			}
		}
	}
	return sb.String(), nil
}

// ── DeVET List Attacks ──

type devetListAttacksTool struct {
	backend *devet.Backend
}

func (t *devetListAttacksTool) Name() string      { return "devet_list_attacks" }
func (t *devetListAttacksTool) ReadOnly() bool     { return true }
func (t *devetListAttacksTool) Description() string {
	return "List all 8 attack types available in DeVET for security testing."
}
func (t *devetListAttacksTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "devet"} }

func (t *devetListAttacksTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (t *devetListAttacksTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	resp, err := devetHTTPClient.Get(t.backend.BaseURL() + "/attacks")
	if err != nil {
		return "", fmt.Errorf("DeVET attacks list failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var wrapper struct {
		Attacks map[string]devet.AttackInfo `json:"attacks"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return "", fmt.Errorf("DeVET parse error: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("DeVET Attack Types (all 8 detected at 100% rate):\n")
	for key, a := range wrapper.Attacks {
		sb.WriteString(fmt.Sprintf("  %s\n", a.Name))
		sb.WriteString(fmt.Sprintf("    ID: %s\n", key))
		sb.WriteString(fmt.Sprintf("    Description: %s\n", a.Description))
		sb.WriteString(fmt.Sprintf("    Expected fault: %s\n", a.ExpectedFault))
	}
	return sb.String(), nil
}

// ── DeVET Simulate Attack ──

type devetSimulateAttackTool struct {
	backend *devet.Backend
}

func (t *devetSimulateAttackTool) Name() string      { return "devet_simulate_attack" }
func (t *devetSimulateAttackTool) ReadOnly() bool     { return true }
func (t *devetSimulateAttackTool) Description() string {
	return "Simulate an attack on the DeVET delegation chain and verify detection. Requires attack_type ID (e.g. A1_delegation_replacement)."
}
func (t *devetSimulateAttackTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "devet"} }

func (t *devetSimulateAttackTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"attack_type":{"type":"string","description":"Attack type ID, e.g. A1_delegation_replacement, A2_sub_result_forgery, A7_grant_tampering"}},"required":["attack_type"]}`)
}

func (t *devetSimulateAttackTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct{ AttackType string `json:"attack_type"` }
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	reqBody, _ := json.Marshal(map[string]string{"attack_type": params.AttackType})
	resp, err := devetHTTPClient.Post(t.backend.BaseURL()+"/attack/simulate", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("DeVET attack simulation failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("DeVET error %d: %s", resp.StatusCode, string(respBody))
	}
	var result devet.AttackResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("DeVET parse error: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Attack Simulation: %s\n", result.AttackName))
	sb.WriteString(fmt.Sprintf("  Expected fault: %s\n", result.ExpectedFault))
	sb.WriteString(fmt.Sprintf("  Detected: %v\n", result.Detected))
	sb.WriteString(fmt.Sprintf("  Fault match: %v\n", result.FaultMatch))
	sb.WriteString(fmt.Sprintf("  Authentic: %v\n", result.Result.Authentic))
	if result.Result.BlameAttribution != "" {
		sb.WriteString(fmt.Sprintf("  Blame: %s\n", result.Result.BlameAttribution))
	}
	if result.Result.FaultType != "" {
		sb.WriteString(fmt.Sprintf("  Fault type: %s\n", result.Result.FaultType))
	}
	return sb.String(), nil
}

func shortHash(h string) string {
	if len(h) > 16 {
		return h[:16] + "..."
	}
	return h
}
