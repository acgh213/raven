package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
)

// New returns an http.Handler that routes requests to the appropriate
// handlers. It applies middleware for request-ID injection.
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)

	var h http.Handler = mux
	h = withRequestID(h)
	return h
}

// handleHealthz responds with a 200 OK JSON status for GET requests only.
// All other methods receive 404 to avoid Go ServeMux's implicit HEAD handling
// and 405 responses for method-pattern patterns.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}

// withRequestID wraps an http.Handler to ensure every response has a
// non-empty X-Request-ID header. If the incoming request carries an
// X-Request-ID header, it is preserved; otherwise a new random ID is
// generated.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			rid = generateID()
		}
		w.Header().Set("X-Request-ID", rid)
		next.ServeHTTP(w, r)
	})
}

// generateID creates a random hex-encoded identifier.
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: should practically never happen.
		return "fallback-request-id"
	}
	return hex.EncodeToString(b)
}

// writeJSON is a helper for writing JSON responses (used by future handlers).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Logging not available at this layer; safe to ignore for error
		// responses since the encoder only fails on pathological types.
		_ = err
	}
}
