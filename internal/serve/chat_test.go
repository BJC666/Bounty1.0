package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
