package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bounty/internal/checkpoint"
	"bounty/internal/devet"
)

func TestModelSwitchEndpoint(t *testing.T) {
	cases := []struct {
		name       string
		switchFn   func(req ModelSwitchRequest) error
		body       string
		wantStatus int
		wantModel  string
	}{
		{
			name:       "success",
			switchFn:   func(req ModelSwitchRequest) error { return nil },
			body:       `{"kind":"openai","base_url":"http://localhost:9999/v1","api_key":"sk-test","model":"gpt-x"}`,
			wantStatus: http.StatusOK,
			wantModel:  "gpt-x",
		},
		{
			name:       "missing model",
			switchFn:   func(req ModelSwitchRequest) error { return nil },
			body:       `{"base_url":"http://localhost:9999/v1"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "switch error",
			switchFn:   func(req ModelSwitchRequest) error { return errors.New("boom") },
			body:       `{"model":"gpt-x"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "no switch fn",
			switchFn:   nil,
			body:       `{"model":"gpt-x"}`,
			wantStatus: http.StatusNotImplemented,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &ChatHandler{SwitchFn: tc.switchFn}
			req := httptest.NewRequest(http.MethodPost, "/chat/api/model", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusOK {
				var resp map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("bad json: %v", err)
				}
				if resp["status"] != "ok" || resp["model"] != tc.wantModel {
					t.Fatalf("resp = %v", resp)
				}
			}
		})
	}
}

func TestChatSendEndpoint(t *testing.T) {
	h := &ChatHandler{SendFn: func(text string) error {
		if text != "hello" {
			t.Fatalf("text = %q", text)
		}
		return nil
	}}
	req := httptest.NewRequest(http.MethodPost, "/chat/api/send", strings.NewReader(`{"message":"hello"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCheckpointListEndpoint(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		h := &ChatHandler{CheckpointListFn: func() ([]checkpoint.Info, error) {
			return []checkpoint.Info{{MsgIndex: 0, Prompt: "first"}, {MsgIndex: 1, Prompt: "second"}}, nil
		}}
		req := httptest.NewRequest(http.MethodGet, "/chat/api/checkpoints", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Status      string            `json:"status"`
			Checkpoints []checkpoint.Info `json:"checkpoints"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		if resp.Status != "ok" || len(resp.Checkpoints) != 2 || resp.Checkpoints[0].MsgIndex != 0 {
			t.Fatalf("resp = %+v", resp)
		}
	})
	t.Run("list error", func(t *testing.T) {
		h := &ChatHandler{CheckpointListFn: func() ([]checkpoint.Info, error) {
			return nil, errors.New("boom")
		}}
		req := httptest.NewRequest(http.MethodGet, "/chat/api/checkpoints", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
	})
	t.Run("unavailable", func(t *testing.T) {
		h := &ChatHandler{}
		req := httptest.NewRequest(http.MethodGet, "/chat/api/checkpoints", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("status = %d, want 501", rec.Code)
		}
	})
}

func TestCheckpointRestoreEndpoint(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var got int = -1
		h := &ChatHandler{CheckpointRestoreFn: func(i int) error { got = i; return nil }}
		req := httptest.NewRequest(http.MethodPost, "/chat/api/checkpoints/restore", bytes.NewBufferString(`{"msg_index":3}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		if got != 3 {
			t.Fatalf("restore called with %d, want 3", got)
		}
	})
	t.Run("missing msg_index", func(t *testing.T) {
		h := &ChatHandler{CheckpointRestoreFn: func(i int) error { return nil }}
		req := httptest.NewRequest(http.MethodPost, "/chat/api/checkpoints/restore", bytes.NewBufferString(`{}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
	t.Run("restore error", func(t *testing.T) {
		h := &ChatHandler{CheckpointRestoreFn: func(i int) error { return errors.New("msg-9 missing") }}
		req := httptest.NewRequest(http.MethodPost, "/chat/api/checkpoints/restore", bytes.NewBufferString(`{"msg_index":9}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "msg-9 missing") {
			t.Fatalf("body = %s", rec.Body.String())
		}
	})
	t.Run("unavailable", func(t *testing.T) {
		h := &ChatHandler{}
		req := httptest.NewRequest(http.MethodPost, "/chat/api/checkpoints/restore", bytes.NewBufferString(`{"msg_index":0}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("status = %d, want 501", rec.Code)
		}
	})
}

func TestDeVETStateEndpoint(t *testing.T) {
	t.Run("ok with snapshot", func(t *testing.T) {
		ok := true
		h := &ChatHandler{DeVETStateFn: func() *devet.StateSnapshot {
			return &devet.StateSnapshot{
				Available: true, HostName: "bounty-host", Authentic: true,
				Agents: []devet.AgentState{{Name: "explore-subagent", Role: "explore", Model: "deepseek-chat", ToolCalls: 3, Authentic: &ok}},
			}
		}}
		req := httptest.NewRequest(http.MethodGet, "/chat/api/devet/state", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Status   string               `json:"status"`
			Snapshot *devet.StateSnapshot `json:"snapshot"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		if resp.Status != "ok" || resp.Snapshot == nil || !resp.Snapshot.Authentic || len(resp.Snapshot.Agents) != 1 {
			t.Fatalf("resp = %+v", resp)
		}
	})
	t.Run("empty before first verification", func(t *testing.T) {
		h := &ChatHandler{DeVETStateFn: func() *devet.StateSnapshot { return nil }}
		req := httptest.NewRequest(http.MethodGet, "/chat/api/devet/state", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"status":"empty"`) {
			t.Fatalf("body = %s", rec.Body.String())
		}
	})
	t.Run("unavailable when not wired", func(t *testing.T) {
		h := &ChatHandler{}
		req := httptest.NewRequest(http.MethodGet, "/chat/api/devet/state", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"unavailable"`) {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
	})
}
