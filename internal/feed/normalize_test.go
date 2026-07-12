package feed

import (
	"testing"

	"raven/internal/model"
)

func TestCanonicalizeURLNormalizesHostDefaultPortAndFragment(t *testing.T) {
	got, err := CanonicalizeURL(" HTTPS://EXAMPLE.COM:443/News?lang=en#latest ")
	if err != nil {
		t.Fatalf("CanonicalizeURL() error: %v", err)
	}
	want := "https://example.com/News?lang=en"
	if got != want {
		t.Errorf("CanonicalizeURL() = %q, want %q", got, want)
	}
}

func TestCanonicalizeURLPreservesIPv6Brackets(t *testing.T) {
	got, err := CanonicalizeURL("HTTP://[2001:DB8::1]:80/feed#fragment")
	if err != nil {
		t.Fatalf("CanonicalizeURL() error: %v", err)
	}
	want := "http://[2001:db8::1]/feed"
	if got != want {
		t.Errorf("CanonicalizeURL() = %q, want %q", got, want)
	}
}

func TestCanonicalizeURLRejectsUnsafeOrMalformedInput(t *testing.T) {
	tests := []string{
		"ftp://example.com/feed.xml",
		"https://user:password@example.com/feed.xml",
		"/relative/feed.xml",
		"https://",
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if _, err := CanonicalizeURL(raw); err == nil {
				t.Errorf("CanonicalizeURL(%q) error = nil, want error", raw)
			}
		})
	}
}

func TestValidateCandidatesCanonicalizesAndRetainsRejections(t *testing.T) {
	valid, rejected := ValidateCandidates([]model.FeedCandidate{
		{URL: "HTTPS://EXAMPLE.COM:443/feed#fragment", Title: "Valid"},
		{URL: "ftp://example.com/not-allowed", Title: "Invalid"},
	})

	if len(valid) != 1 || valid[0].URL != "https://example.com/feed" {
		t.Errorf("valid candidates = %+v, want canonicalized HTTPS feed", valid)
	}
	if len(rejected) != 1 {
		t.Fatalf("rejections = %+v, want one rejected candidate", rejected)
	}
	if rejected[0].URL != "ftp://example.com/not-allowed" || rejected[0].Reason == "" {
		t.Errorf("rejection = %+v, want original URL and reason", rejected[0])
	}
}
