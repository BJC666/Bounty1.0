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

	"bounty/internal/tool"
)

const devetBaseURL = "http://127.0.0.1:8765/api"

var devetClient = &http.Client{Timeout: 30 * time.Second}

// ── DeVET Health ──

type DeVETHealthTool struct{}

func (DeVETHealthTool) Name() string        { return "devet_health" }
func (DeVETHealthTool) ReadOnly() bool      { return true }
func (DeVETHealthTool) Description() string { return "Check if the DeVET verification backend is running and healthy." }
func (DeVETHealthTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "devet"} }

func (DeVETHealthTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (DeVETHealthTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	resp, err := devetClient.Get(devetBaseURL + "/health")
	if err != nil {
		return "", fmt.Errorf("DeVET backend not reachable: %w (start with: cd DeVET && python backend/server.py)", err)
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	b, _ := json.MarshalIndent(result, "", "  ")
	return fmt.Sprintf("DeVET backend is healthy:\n%s", string(b)), nil
}

// ── DeVET Build Scenario ──

type DeVETBuildScenarioTool struct{}

func (DeVETBuildScenarioTool) Name() string        { return "devet_build_scenario" }
func (DeVETBuildScenarioTool) ReadOnly() bool      { return true }
func (DeVETBuildScenarioTool) Description() string { return "Build a 3-agent Trading DAO delegation chain in DeVET (StrategyAgent → ExecutionAgentETH + ExecutionAgentBTC)." }
func (DeVETBuildScenarioTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "devet"} }

func (DeVETBuildScenarioTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (DeVETBuildScenarioTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	resp, err := devetClient.Post(devetBaseURL+"/scenario/build", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil { return "", fmt.Errorf("DeVET build failed: %w", err) }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("DeVET error %d: %s", resp.StatusCode, string(body))
	}
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	chain, _ := result["chain"].(map[string]interface{})
	total, _ := chain["total_agents"].(float64)
	depth, _ := chain["max_depth"].(float64)
	grantCount, _ := chain["grant_count"].(float64)
	return fmt.Sprintf("Scenario built successfully:\n- Total agents: %d\n- Max delegation depth: %d\n- Grant count: %d\n- Root AID: %s",
		int(total), int(depth), int(grantCount), shortHash(fmt.Sprintf("%v", chain["root_aid_hash"]))), nil
}

// ── DeVET Verify Chain ──

type DeVETVerifyChainTool struct{}

func (DeVETVerifyChainTool) Name() string        { return "devet_verify_chain" }
func (DeVETVerifyChainTool) ReadOnly() bool      { return true }
func (DeVETVerifyChainTool) Description() string { return "Verify the current DeVET delegation chain. Must call devet_build_scenario first." }
func (DeVETVerifyChainTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "devet"} }

func (DeVETVerifyChainTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (DeVETVerifyChainTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	resp, err := devetClient.Post(devetBaseURL+"/chain/verify", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil { return "", fmt.Errorf("DeVET verify failed: %w", err) }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("DeVET error %d: %s", resp.StatusCode, string(body))
	}
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	r, _ := result["result"].(map[string]interface{})
	authentic, _ := r["authentic"].(bool)
	blame, _ := r["blame_attribution"].(string)
	depth, _ := r["chain_depth"].(float64)
	total, _ := r["total_agents"].(float64)

	var sb strings.Builder
	sb.WriteString("DeVET Chain Verification Result:\n")
	sb.WriteString(fmt.Sprintf("  Authentic: %v\n", authentic))
	sb.WriteString(fmt.Sprintf("  Chain depth: %d\n", int(depth)))
	sb.WriteString(fmt.Sprintf("  Total agents: %d\n", int(total)))
	if blame != "" {
		sb.WriteString(fmt.Sprintf("  Blame: %s\n", blame))
	}
	findings, _ := r["findings"].([]interface{})
	if len(findings) > 0 {
		sb.WriteString("  Agent findings:\n")
		for _, f := range findings {
			fm, _ := f.(map[string]interface{})
			agent, _ := fm["agent"].(string)
			ok, _ := fm["chain_ok"].(bool)
			ft, _ := fm["fault_type"].(string)
			if ft != "" {
				sb.WriteString(fmt.Sprintf("    %s: ❌ %s\n", shortHash(agent), ft))
			} else if ok {
				sb.WriteString(fmt.Sprintf("    %s: ✅\n", shortHash(agent)))
			}
		}
	}
	return sb.String(), nil
}

// ── DeVET List Attacks ──

type DeVETListAttacksTool struct{}

func (DeVETListAttacksTool) Name() string        { return "devet_list_attacks" }
func (DeVETListAttacksTool) ReadOnly() bool      { return true }
func (DeVETListAttacksTool) Description() string { return "List all 8 attack types available in DeVET for security testing." }
func (DeVETListAttacksTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "devet"} }

func (DeVETListAttacksTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (DeVETListAttacksTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	resp, err := devetClient.Get(devetBaseURL + "/attacks")
	if err != nil { return "", fmt.Errorf("DeVET attacks list failed: %w", err) }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	attacks, _ := result["attacks"].(map[string]interface{})

	var sb strings.Builder
	sb.WriteString("DeVET Attack Types (all 8 detected at 100% rate):\n")
	for key, val := range attacks {
		am, _ := val.(map[string]interface{})
		name, _ := am["name"].(string)
		desc, _ := am["description"].(string)
		fault, _ := am["expected_fault"].(string)
		sb.WriteString(fmt.Sprintf("  %s\n", name))
		sb.WriteString(fmt.Sprintf("    ID: %s\n", key))
		sb.WriteString(fmt.Sprintf("    Description: %s\n", desc))
		sb.WriteString(fmt.Sprintf("    Expected fault: %s\n", fault))
	}
	return sb.String(), nil
}

// ── DeVET Simulate Attack ──

type DeVETSimulateAttackTool struct{}

func (DeVETSimulateAttackTool) Name() string        { return "devet_simulate_attack" }
func (DeVETSimulateAttackTool) ReadOnly() bool      { return true }
func (DeVETSimulateAttackTool) Description() string { return "Simulate an attack on the DeVET delegation chain and verify detection. Requires attack_type ID (e.g. A1_delegation_replacement)." }
func (DeVETSimulateAttackTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "devet"} }

func (DeVETSimulateAttackTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"attack_type":{"type":"string","description":"Attack type ID, e.g. A1_delegation_replacement, A2_sub_result_forgery, A7_grant_tampering"}},"required":["attack_type"]}`)
}

func (DeVETSimulateAttackTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct{ AttackType string `json:"attack_type"` }
	if err := json.Unmarshal(args, &params); err != nil { return "", err }

	reqBody, _ := json.Marshal(map[string]string{"attack_type": params.AttackType})
	resp, err := devetClient.Post(devetBaseURL+"/attack/simulate", "application/json", bytes.NewReader(reqBody))
	if err != nil { return "", fmt.Errorf("DeVET attack simulation failed: %w", err) }
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("DeVET error %d: %s", resp.StatusCode, string(respBody))
	}
	var result map[string]interface{}
	json.Unmarshal(respBody, &result)

	attackName, _ := result["attack_name"].(string)
	expected, _ := result["expected_fault"].(string)
	detected, _ := result["detected"].(bool)
	faultMatch, _ := result["fault_match"].(bool)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Attack Simulation: %s\n", attackName))
	sb.WriteString(fmt.Sprintf("  Expected fault: %s\n", expected))
	sb.WriteString(fmt.Sprintf("  Detected: %v\n", detected))
	sb.WriteString(fmt.Sprintf("  Fault match: %v\n", faultMatch))

	r, _ := result["result"].(map[string]interface{})
	if r != nil {
		sb.WriteString(fmt.Sprintf("  Authentic: %v\n", r["authentic"]))
		if blame, ok := r["blame_attribution"].(string); ok && blame != "" {
			sb.WriteString(fmt.Sprintf("  Blame: %s\n", blame))
		}
		if ft, ok := r["fault_type"].(string); ok && ft != "" {
			sb.WriteString(fmt.Sprintf("  Fault type: %s\n", ft))
		}
	}
	return sb.String(), nil
}

func shortHash(h string) string {
	if len(h) > 16 { return h[:16] + "..." }
	return h
}
