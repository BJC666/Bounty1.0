package devet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// MirrorAgent describes one Bounty sub-agent delegation to be mirrored into
// the DeVET backend as a sealed DelegationGrant + CompositeProof.
type MirrorAgent struct {
	Name             string            `json:"name"`
	Endpoint         string            `json:"endpoint"`
	Role             string            `json:"role"`
	Model            string            `json:"model"`
	ResultCommitment string            `json:"result_commitment"` // sha256 hex of the sub-agent final text
	ToolCalls        int               `json:"tool_calls"`
	WrittenFiles     []string          `json:"written_files,omitempty"`
	// P8-4: 声称的远端 API 调用与真实会话承诺证明（web_fetch --proof 输出，
	// 原样透传给 DeVET /chain/mirror；有 web_calls 无证明 → webproof_missing）。
	WebCalls  []string          `json:"web_calls,omitempty"`
	WebProofs []json.RawMessage `json:"web_proofs,omitempty"`
}

// MirrorSpec is the full delegation to verify: one host plus its sub-agents.
// TenantID scopes the chain on the DeVET backend (multi-tenant sharding) so
// concurrent Bounty sessions never overwrite each other's chains.
type MirrorSpec struct {
	TenantID     string        `json:"tenant_id,omitempty"`
	HostName     string        `json:"host_name"`
	HostEndpoint string        `json:"host_endpoint"`
	Agents       []MirrorAgent `json:"agents"`
}

// mirrorHTTPClient carries a short timeout so a stalled backend cannot hang
// sub-agent completion.
var mirrorHTTPClient = &http.Client{Timeout: 10 * time.Second}

// StateSnapshot is the last verification state served to the web console
// (GET /chat/api/devet/state) and rendered by the chain-visualisation panel.
type StateSnapshot struct {
	Available    bool         `json:"available"`
	Time         time.Time    `json:"time"`
	HostName     string       `json:"host_name"`
	HostEndpoint string       `json:"host_endpoint"`
	Agents       []AgentState `json:"agents"`
	Authentic    bool         `json:"authentic"`
	FaultType    string       `json:"fault_type,omitempty"`
	BlamePath    []string     `json:"blame_path,omitempty"`
	Error        string       `json:"error,omitempty"`
	// LastError records a backend/transport failure for honest degradation.
	LastError string `json:"last_error,omitempty"`
}

// AgentState is one delegate row in the chain panel.
type AgentState struct {
	Name             string   `json:"name"`
	Endpoint         string   `json:"endpoint"`
	Role             string   `json:"role"`
	Model            string   `json:"model"`
	ResultCommitment string   `json:"result_commitment"`
	ToolCalls        int      `json:"tool_calls"`
	WrittenFiles     []string `json:"written_files,omitempty"`
	Authentic        *bool    `json:"authentic,omitempty"`
	FaultType        string   `json:"fault_type,omitempty"`
}

// MirrorClient is the Bounty-side client for the DeVET /chain/mirror,
// /chain/verify and /chain/tamper endpoints. It also keeps the latest
// snapshot for the web chain-visualisation panel.
type MirrorClient struct {
	backend  *Backend
	tenantID string
	mu       sync.Mutex
	snap     *StateSnapshot
}

// NewMirrorClient wires a client to an existing backend connection. tenantID
// scopes every chain operation on the backend (typically the Bounty session
// id); pass "" for the legacy shared "default" tenant.
func NewMirrorClient(backend *Backend, tenantID string) *MirrorClient {
	if backend == nil {
		return nil
	}
	return &MirrorClient{backend: backend, tenantID: tenantID}
}

// MirrorAndVerify posts the delegation spec, verifies it, and records the
// snapshot. It implements agent.DeVETVerifier.
func (c *MirrorClient) MirrorAndVerify(ctx context.Context, spec MirrorSpec) (VerifyDetail, error) {
	spec.TenantID = c.tenantID
	body, err := json.Marshal(spec)
	if err != nil {
		return VerifyDetail{}, fmt.Errorf("devet mirror marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.backend.BaseURL()+"/chain/mirror", bytes.NewReader(body))
	if err != nil {
		return VerifyDetail{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := mirrorHTTPClient.Do(req)
	if err != nil {
		c.recordError(spec, fmt.Errorf("mirror: %w", err))
		return VerifyDetail{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		err := fmt.Errorf("devet mirror HTTP %d: %s", resp.StatusCode, buf.String())
		c.recordError(spec, err)
		return VerifyDetail{}, err
	}

	verify, err := c.Verify(ctx)
	if err != nil {
		c.recordError(spec, err)
		return VerifyDetail{}, err
	}
	c.record(spec, verify, "")
	return verify, nil
}

// Verify re-verifies the current chain on the backend.
func (c *MirrorClient) Verify(ctx context.Context) (VerifyDetail, error) {
	body, _ := json.Marshal(map[string]string{"tenant_id": c.tenantID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.backend.BaseURL()+"/chain/verify", bytes.NewReader(body))
	if err != nil {
		return VerifyDetail{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := mirrorHTTPClient.Do(req)
	if err != nil {
		return VerifyDetail{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		return VerifyDetail{}, fmt.Errorf("devet verify HTTP %d: %s", resp.StatusCode, buf.String())
	}
	var wrapper VerificationResult
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return VerifyDetail{}, fmt.Errorf("devet verify parse: %w", err)
	}
	return wrapper.Result, nil
}

// Tamper simulates a forgery of one delegate's proof and returns the
// verification result with blame attribution.
func (c *MirrorClient) Tamper(ctx context.Context, delegateIndex int, forgedCommitment string) (VerifyDetail, error) {
	body, _ := json.Marshal(map[string]any{
		"tenant_id":      c.tenantID,
		"delegate_index": delegateIndex,
		"commitment":     forgedCommitment,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.backend.BaseURL()+"/chain/tamper", bytes.NewReader(body))
	if err != nil {
		return VerifyDetail{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := mirrorHTTPClient.Do(req)
	if err != nil {
		return VerifyDetail{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		return VerifyDetail{}, fmt.Errorf("devet tamper HTTP %d: %s", resp.StatusCode, buf.String())
	}
	var wrapper struct {
		Status string       `json:"status"`
		Result VerifyDetail `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return VerifyDetail{}, fmt.Errorf("devet tamper parse: %w", err)
	}
	c.recordFromVerify(wrapper.Result, "forged:"+forgedCommitment)
	return wrapper.Result, nil
}

// State returns the latest snapshot (nil until the first verification).
func (c *MirrorClient) State() *StateSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snap == nil {
		return nil
	}
	cp := *c.snap
	cp.Agents = append([]AgentState(nil), c.snap.Agents...)
	cp.BlamePath = append([]string(nil), c.snap.BlamePath...)
	return &cp
}

func (c *MirrorClient) record(spec MirrorSpec, verify VerifyDetail, lastErr string) {
	agents := make([]AgentState, 0, len(spec.Agents))
	for _, a := range spec.Agents {
		agents = append(agents, AgentState{
			Name:             a.Name,
			Endpoint:         a.Endpoint,
			Role:             a.Role,
			Model:            a.Model,
			ResultCommitment: a.ResultCommitment,
			ToolCalls:        a.ToolCalls,
			WrittenFiles:     append([]string(nil), a.WrittenFiles...),
		})
	}
	// Match findings back onto the agents (findings carry per-agent authentic
	// flags; the first faulty one drives the blame path). The backend emits
	// the root agent as findings[0], then one entry per delegate.
	for i, f := range verify.Findings {
		if i == 0 {
			continue
		}
		ai := i - 1
		if ai >= len(agents) {
			break
		}
		if f.Authentic != nil {
			agents[ai].Authentic = f.Authentic
		}
		agents[ai].FaultType = f.FaultType
	}
	c.mu.Lock()
	c.snap = &StateSnapshot{
		Available:    true,
		Time:         time.Now(),
		HostName:     spec.HostName,
		HostEndpoint: spec.HostEndpoint,
		Agents:       agents,
		Authentic:    verify.Authentic,
		FaultType:    verify.FaultType,
		BlamePath:    verify.BlamePath,
		Error:        verify.Error,
		LastError:    lastErr,
	}
	c.mu.Unlock()
}

func (c *MirrorClient) recordFromVerify(verify VerifyDetail, lastErr string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snap == nil {
		return
	}
	c.snap.Time = time.Now()
	c.snap.Authentic = verify.Authentic
	c.snap.FaultType = verify.FaultType
	c.snap.BlamePath = verify.BlamePath
	c.snap.Error = verify.Error
	c.snap.LastError = lastErr
}

func (c *MirrorClient) recordError(spec MirrorSpec, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snap != nil {
		c.snap.LastError = err.Error()
		return
	}
	agents := make([]AgentState, 0, len(spec.Agents))
	for _, a := range spec.Agents {
		agents = append(agents, AgentState{
			Name: a.Name, Endpoint: a.Endpoint, Role: a.Role,
			Model: a.Model, ResultCommitment: a.ResultCommitment,
			ToolCalls: a.ToolCalls,
		})
	}
	c.snap = &StateSnapshot{
		Available:    false,
		Time:         time.Now(),
		HostName:     spec.HostName,
		HostEndpoint: spec.HostEndpoint,
		Agents:       agents,
		LastError:    err.Error(),
	}
}
