// Package auth provides shared HTTP authentication for Bounty's web and
// channel endpoints. All endpoints are protected by BOUNTY_AUTH_TOKEN when
// it is set; with the env var empty (local development default) requests pass
// through, matching the previous behavior.
package auth

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// TokenFromRequest returns the bearer token from the Authorization header,
// falling back to the ?token= query parameter. The query form is needed for
// SSE clients (EventSource) which cannot set custom headers, but callers
// should prefer the header form to avoid token leakage into logs/history.
func TokenFromRequest(r *http.Request) string {
	if t := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); t != "" {
		return t
	}
	return r.URL.Query().Get("token")
}

// Middleware rejects requests that do not carry the configured
// BOUNTY_AUTH_TOKEN. The comparison is constant-time to avoid a timing side
// channel on the token value.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := os.Getenv("BOUNTY_AUTH_TOKEN")
		if want == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := TokenFromRequest(r)
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			http.Error(w, `{"status":"error","error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
