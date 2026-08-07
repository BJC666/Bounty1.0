package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMiddlewareNoTokenConfigured(t *testing.T) {
	os.Unsetenv("BOUNTY_AUTH_TOKEN")
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 when no token configured", rec.Code)
	}
}

func TestMiddlewareRejectsMissingToken(t *testing.T) {
	os.Setenv("BOUNTY_AUTH_TOKEN", "secret-token")
	defer os.Unsetenv("BOUNTY_AUTH_TOKEN")
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMiddlewareAcceptsHeaderToken(t *testing.T) {
	os.Setenv("BOUNTY_AUTH_TOKEN", "secret-token")
	defer os.Unsetenv("BOUNTY_AUTH_TOKEN")
	ok := false
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !ok {
		t.Fatalf("status = %d ok=%v, want 200/true", rec.Code, ok)
	}
}

func TestMiddlewareAcceptsQueryToken(t *testing.T) {
	os.Setenv("BOUNTY_AUTH_TOKEN", "secret-token")
	defer os.Unsetenv("BOUNTY_AUTH_TOKEN")
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/events?token=secret-token", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for query token", rec.Code)
	}
}

func TestMiddlewareRejectsWrongToken(t *testing.T) {
	os.Setenv("BOUNTY_AUTH_TOKEN", "secret-token")
	defer os.Unsetenv("BOUNTY_AUTH_TOKEN")
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
