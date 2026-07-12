package fetcher

import (
	"testing"
)

func TestPolicyRejectsNonHTTPSchemes(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Allow("ftp://example.com/feed.xml"); err == nil {
		t.Error("Allow(ftp://) error = nil, want rejection")
	}
	if err := p.Allow("http://example.com/feed.xml"); err != nil {
		t.Errorf("Allow(http://) error = %v, want nil", err)
	}
	if err := p.Allow("https://example.com/feed.xml"); err != nil {
		t.Errorf("Allow(https://) error = %v, want nil", err)
	}
}

func TestPolicyRejectsUserinfoURLs(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Allow("https://user:password@example.com/feed.xml"); err == nil {
		t.Error("Allow(userinfo URL) error = nil, want rejection")
	}
}

func TestPolicyRejectsLoopbackAndPrivateAddresses(t *testing.T) {
	p := DefaultPolicy()
	rejected := []string{
		"http://127.0.0.1:8080/feed",
		"http://[::1]/feed",
		"http://localhost/feed",
		"http://10.0.0.1/feed",
		"http://172.16.0.1/feed",
		"http://192.168.1.1/feed",
		"http://169.254.1.1/feed",
	}
	for _, url := range rejected {
		t.Run(url, func(t *testing.T) {
			if err := p.Allow(url); err == nil {
				t.Errorf("Allow(%s) error = nil, want rejection", url)
			}
		})
	}
}

func TestPolicyRejectsLinkLocalIPv6(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Allow("http://[fe80::1]/feed"); err == nil {
		t.Error("Allow(fe80::) error = nil, want rejection")
	}
}

func TestPolicyRejectsTailscaleAndCloudMetadata(t *testing.T) {
	p := DefaultPolicy()
	rejected := []string{
		"http://100.64.0.1/feed",        // Tailscale / CGNAT
		"http://169.254.169.254/latest", // cloud metadata
	}
	for _, url := range rejected {
		t.Run(url, func(t *testing.T) {
			if err := p.Allow(url); err == nil {
				t.Errorf("Allow(%s) error = nil, want rejection", url)
			}
		})
	}
}

func TestPolicyAllowsPublicHost(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Allow("https://example.com/feed.xml"); err != nil {
		t.Errorf("Allow(example.com) error = %v, want nil", err)
	}
}
