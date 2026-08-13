package serve

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bounty/internal/checkpoint"
	"bounty/internal/devet"
	"bounty/internal/provider"
)

// tinyPNG is a 1x1 red pixel PNG (valid signature for DetectContentType).
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestUploadEndpoint(t *testing.T) {
	pngData, err := decodeBase64(tinyPNG)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	h := &ChatHandler{UploadDir: t.TempDir()}

	// valid upload
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("images", "shot.png")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(pngData)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/chat/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string   `json:"status"`
		Paths  []string `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "ok" || len(resp.Paths) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	if _, err := os.Stat(resp.Paths[0]); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}

	// bad extension rejected
	var buf2 bytes.Buffer
	mw2 := multipart.NewWriter(&buf2)
	fw2, _ := mw2.CreateFormFile("images", "evil.exe")
	fw2.Write(pngData)
	mw2.Close()
	req2 := httptest.NewRequest(http.MethodPost, "/chat/api/upload", &buf2)
	req2.Header.Set("Content-Type", mw2.FormDataContentType())
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("bad ext status = %d", rec2.Code)
	}
}

func TestSendWithImagesEndpoint(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "shot.png")
	pngData, _ := decodeBase64(tinyPNG)
	if err := os.WriteFile(pngPath, pngData, 0o644); err != nil {
		t.Fatal(err)
	}

	var gotText string
	var gotParts []provider.ImagePart
	h := &ChatHandler{
		SendFn: func(text string) error { return nil },
		SendImagesFn: func(text string, images []provider.ImagePart) error {
			gotText = text
			gotParts = images
			return nil
		},
	}
	body := `{"message":"看看这张图","images":["` + strings.ReplaceAll(pngPath, "\\", "\\\\") + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/chat/api/send", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if gotText != "看看这张图" {
		t.Fatalf("text = %q", gotText)
	}
	if len(gotParts) != 1 || gotParts[0].MediaType != "image/png" {
		t.Fatalf("parts = %+v", gotParts)
	}

	// SendImagesFn nil -> 501
	h2 := &ChatHandler{SendFn: func(text string) error { return nil }}
	req2 := httptest.NewRequest(http.MethodPost, "/chat/api/send", bytes.NewBufferString(`{"message":"x","images":["`+strings.ReplaceAll(pngPath, "\\", "\\\\")+`"]}`))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h2.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotImplemented {
		t.Fatalf("nil SendImagesFn status = %d", rec2.Code)
	}
}

func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

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
