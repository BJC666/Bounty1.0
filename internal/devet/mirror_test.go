package devet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDeVET emulates the three mirror-related backend endpoints.
type fakeDeVET struct {
	server   *httptest.Server
	agents   []MirrorAgent
	host     string
	tenant   string
	tampered bool
}

func newFakeDeVET(t *testing.T) *fakeDeVET {
	t.Helper()
	f := &fakeDeVET{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/chain/mirror", func(w http.ResponseWriter, r *http.Request) {
		var spec MirrorSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		f.tenant = spec.TenantID
		f.host = spec.HostName
		f.agents = spec.Agents
		json.NewEncoder(w).Encode(map[string]any{
			"status": "mirrored",
			"chain":  map[string]any{"total_agents": len(spec.Agents) + 1},
		})
	})
	mux.HandleFunc("/api/chain/verify", func(w http.ResponseWriter, r *http.Request) {
		findings := []AgentFinding{{Agent: "root", ChainOK: boolPtr(true)}}
		agents := make([]AgentState, 0, len(f.agents))
		for _, a := range f.agents {
			findings = append(findings, AgentFinding{Agent: a.Name, Authentic: boolPtr(!f.tampered)})
			agents = append(agents, AgentState{
				Name: a.Name, Endpoint: a.Endpoint, Role: a.Role,
				Model: a.Model, ResultCommitment: a.ResultCommitment, ToolCalls: a.ToolCalls,
			})
		}
		res := VerifyDetail{
			Authentic:   !f.tampered,
			BlamePath:   nil,
			ChainDepth:  1,
			TotalAgents: len(f.agents) + 1,
			Findings:    findings,
		}
		if f.tampered {
			res.FaultType = "subagent_proof_invalid"
			res.BlamePath = []string{"delegation[0]", f.agents[0].Name}
			res.Error = "AID mismatch"
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "verified", "result": res})
	})
	mux.HandleFunc("/api/chain/tamper", func(w http.ResponseWriter, r *http.Request) {
		f.tampered = true
		res := VerifyDetail{
			Authentic: false,
			FaultType: "subagent_proof_invalid",
			BlamePath: []string{"delegation[0]", f.agents[0].Name},
			Error:     "AID mismatch",
			Findings:  []AgentFinding{{Agent: "root", ChainOK: boolPtr(true)}, {Agent: f.agents[0].Name, Authentic: boolPtr(false), FaultType: "subagent_proof_invalid"}},
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "tampered", "result": res})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func boolPtr(v bool) *bool { return &v }

func spec() MirrorSpec {
	return MirrorSpec{
		HostName:     "bounty-host",
		HostEndpoint: "bounty.local",
		Agents: []MirrorAgent{
			{Name: "SubAgentA", Endpoint: "api.deepseek.com", Role: "explore", Model: "deepseek-chat", ResultCommitment: "a1b2c3", ToolCalls: 2, WrittenFiles: []string{"out/a.txt"}},
			{Name: "SubAgentB", Endpoint: "api.openai.com", Role: "general", Model: "gpt-4o", ResultCommitment: "e5f6a7", ToolCalls: 0},
		},
	}
}

func TestTenantPropagation(t *testing.T) {
	f := newFakeDeVET(t)
	client := NewMirrorClient(&Backend{baseURL: f.server.URL + "/api"}, "session-42")
	if _, err := client.MirrorAndVerify(context.Background(), spec()); err != nil {
		t.Fatalf("MirrorAndVerify: %v", err)
	}
	if f.tenant != "session-42" {
		t.Fatalf("tenant = %q, want session-42", f.tenant)
	}
	// empty tenant id keeps the backend default tenant
	f2 := newFakeDeVET(t)
	client2 := NewMirrorClient(&Backend{baseURL: f2.server.URL + "/api"}, "")
	if _, err := client2.MirrorAndVerify(context.Background(), spec()); err != nil {
		t.Fatalf("MirrorAndVerify: %v", err)
	}
	if f2.tenant != "" {
		t.Fatalf("tenant = %q, want empty", f2.tenant)
	}
}

func TestMirrorAndVerifyHappyPath(t *testing.T) {
	f := newFakeDeVET(t)
	client := NewMirrorClient(&Backend{baseURL: f.server.URL + "/api"}, "test-session")

	detail, err := client.MirrorAndVerify(context.Background(), spec())
	if err != nil {
		t.Fatalf("MirrorAndVerify: %v", err)
	}
	if !detail.Authentic {
		t.Fatalf("expected authentic chain")
	}
	snap := client.State()
	if snap == nil {
		t.Fatal("state snapshot missing")
	}
	if !snap.Available || !snap.Authentic || snap.HostName != "bounty-host" {
		t.Fatalf("snapshot wrong: %+v", snap)
	}
	if len(snap.Agents) != 2 || snap.Agents[0].ToolCalls != 2 {
		t.Fatalf("agents wrong: %+v", snap.Agents)
	}
	if snap.Agents[0].Authentic == nil || !*snap.Agents[0].Authentic {
		t.Fatalf("agent0 should be authentic: %+v", snap.Agents[0])
	}
}

func TestTamperBlameAttribution(t *testing.T) {
	f := newFakeDeVET(t)
	client := NewMirrorClient(&Backend{baseURL: f.server.URL + "/api"}, "test-session")
	if _, err := client.MirrorAndVerify(context.Background(), spec()); err != nil {
		t.Fatal(err)
	}
	detail, err := client.Tamper(context.Background(), 0, "forged-commitment")
	if err != nil {
		t.Fatalf("Tamper: %v", err)
	}
	if detail.Authentic {
		t.Fatal("tampered chain must be inauthentic")
	}
	if detail.FaultType != "subagent_proof_invalid" {
		t.Fatalf("fault=%q", detail.FaultType)
	}
	if len(detail.BlamePath) != 2 || detail.BlamePath[0] != "delegation[0]" || detail.BlamePath[1] != "SubAgentA" {
		t.Fatalf("blame path wrong: %v", detail.BlamePath)
	}
	snap := client.State()
	if snap == nil || snap.Authentic || snap.FaultType != "subagent_proof_invalid" {
		t.Fatalf("snapshot not updated after tamper: %+v", snap)
	}
}

func TestStateReturnsCopy(t *testing.T) {
	f := newFakeDeVET(t)
	client := NewMirrorClient(&Backend{baseURL: f.server.URL + "/api"}, "test-session")
	if _, err := client.MirrorAndVerify(context.Background(), spec()); err != nil {
		t.Fatal(err)
	}
	s1 := client.State()
	s1.Agents[0].Name = "MUTATED"
	s1.BlamePath = append(s1.BlamePath, "junk")
	s2 := client.State()
	if s2.Agents[0].Name == "MUTATED" || len(s2.BlamePath) != 0 {
		t.Fatalf("State must return a defensive copy: %+v", s2)
	}
}

func TestMirrorBackendErrorRecordsState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "backend down", 503)
	}))
	defer server.Close()
	client := NewMirrorClient(&Backend{baseURL: server.URL + "/api"}, "test-session")
	_, err := client.MirrorAndVerify(context.Background(), spec())
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected HTTP 503 error, got %v", err)
	}
	snap := client.State()
	if snap == nil || snap.Available || snap.LastError == "" {
		t.Fatalf("degraded snapshot wrong: %+v", snap)
	}
}
