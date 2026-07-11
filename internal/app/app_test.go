package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzReturns200(t *testing.T) {
	h := New()
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHealthzJSONBody(t *testing.T) {
	h := New()
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf(`body["status"] = %q, want "ok"`, body["status"])
	}
}

func TestHealthzHasRequestID(t *testing.T) {
	h := New()
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	rid := resp.Header.Get("X-Request-ID")
	if rid == "" {
		t.Fatal("X-Request-ID header is empty or missing")
	}
}

func TestHealthzUsesIncomingRequestID(t *testing.T) {
	h := New()
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/healthz", nil)
	req.Header.Set("X-Request-ID", "my-custom-id-42")

	client := ts.Client()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	rid := resp.Header.Get("X-Request-ID")
	if rid != "my-custom-id-42" {
		t.Errorf("X-Request-ID = %q, want %q", rid, "my-custom-id-42")
	}
}

func TestHealthzGETOnly(t *testing.T) {
	h := New()
	ts := httptest.NewServer(h)
	defer ts.Close()

	// All non-GET methods at /healthz must return 404 with a non-empty request ID.
	// Notably, HEAD is not implicitly served by the GET handler.
	for _, method := range []string{"HEAD", "POST", "PUT", "PATCH", "DELETE"} {
		req, _ := http.NewRequest(method, ts.URL+"/healthz", nil)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("%s /healthz: %v", method, err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s /healthz status = %d, want %d", method, resp.StatusCode, http.StatusNotFound)
		}
		rid := resp.Header.Get("X-Request-ID")
		if rid == "" {
			t.Errorf("%s /healthz: X-Request-ID header is empty or missing", method)
		}
		resp.Body.Close()
	}
}

func TestUnknownRouteReturns404(t *testing.T) {
	h := New()
	ts := httptest.NewServer(h)
	defer ts.Close()

	paths := []string{"/", "/api", "/feeds", "/nonexistent"}
	for _, path := range paths {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestUnknownRouteHasRequestID(t *testing.T) {
	h := New()
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("GET /nonexistent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}

	rid := resp.Header.Get("X-Request-ID")
	if rid == "" {
		t.Fatal("X-Request-ID header is empty or missing on 404 response")
	}
}
