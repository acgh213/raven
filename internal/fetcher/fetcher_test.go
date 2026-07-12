package fetcher

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testPolicy is a permissive policy for localhost test servers.
type testPolicy struct{}

func (testPolicy) Allow(_ string) error { return nil }

// redirectRejectingPolicy allows the first call (initial fetch) and rejects
// subsequent calls containing the configured substring. This lets us test
// redirect-chain policy enforcement without blocking the test server itself.
type redirectRejectingPolicy struct {
	contains string
	calls    int
}

func (p *redirectRejectingPolicy) Allow(raw string) error {
	p.calls++
	if p.calls > 1 && strings.Contains(raw, p.contains) {
		return &testError{msg: "blocked"}
	}
	return nil
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestFetcherReturnsBodyAndHeadersForSafePublicURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "<rss></rss>")
	}))
	defer server.Close()

	p := &testPolicy{}
	client := NewClient(p)

	resp, err := client.Fetch(server.URL)
	if err != nil {
		t.Fatalf("Fetch(%s) error: %v", server.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "<rss>") {
		t.Errorf("body = %q, want <rss> content", string(body))
	}
}

func TestFetcherSendsDescriptiveUserAgent(t *testing.T) {
	var gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := &testPolicy{}
	client := NewClient(p)
	resp, err := client.Fetch(server.URL)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	resp.Body.Close()
	if gotUA == "" {
		t.Error("User-Agent header was not sent")
	}
	if !strings.HasPrefix(gotUA, "Raven/") {
		t.Errorf("User-Agent = %q, want Raven/ prefix", gotUA)
	}
}

func TestFetcherEnforcesBodySizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, strings.Repeat("x", 1000))
	}))
	defer server.Close()

	p := &testPolicy{}
	client := NewClient(p)
	client.MaxResponseBytes = 10

	resp, err := client.Fetch(server.URL)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	defer resp.Body.Close()

	_, err = io.ReadAll(resp.Body)
	if err == nil {
		t.Fatal("reading body error = nil, want body-too-large error")
	}
	if !strings.Contains(err.Error(), "response body exceeds") {
		t.Errorf("error = %v, want 'response body exceeds' message", err)
	}
}

func TestFetcherRejectsRedirectToUnsafeTarget(t *testing.T) {
	safeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/danger", http.StatusFound)
	}))
	defer safeServer.Close()

	p := &redirectRejectingPolicy{contains: "127.0.0.1"}
	client := NewClient(p)

	_, err := client.Fetch(safeServer.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want redirect rejection")
	}
	if !strings.Contains(err.Error(), "unsafe redirect") {
		t.Errorf("error = %v, want 'unsafe redirect' message", err)
	}
}

func TestFetcherRejectsExcessiveRedirects(t *testing.T) {
	redirectCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		if redirectCount <= 6 {
			http.Redirect(w, r, "/loop", http.StatusFound)
		} else {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "finally here")
		}
	}))
	defer server.Close()

	p := &testPolicy{}
	client := NewClient(p)
	client.MaxRedirects = 3

	_, err := client.Fetch(server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want redirect limit error")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Errorf("error = %v, want redirect limit message", err)
	}
}
