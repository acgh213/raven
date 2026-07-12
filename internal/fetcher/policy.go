// Package fetcher provides a safe, bounded HTTP client for feed and article
// retrieval. A Policy rejects destinations known to be unsafe before a single
// byte is sent.
package fetcher

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Policy decides whether a feed or article URL may be fetched.
type Policy interface {
	Allow(raw string) error
}

// defaultPolicy is the real, resolver-backed policy.
type defaultPolicy struct {
	resolver Resolver
}

// Resolver resolves a hostname to one or more IP addresses.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// DefaultPolicy creates a Policy backed by the system resolver.
func DefaultPolicy() Policy {
	return &defaultPolicy{resolver: net.DefaultResolver}
}

// Allow returns nil if u is a safe, allowed destination and an error otherwise.
func (p *defaultPolicy) Allow(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("feed URL is blank")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse feed URL: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("scheme %q not allowed for fetching", u.Scheme)
	}

	if u.User != nil {
		return fmt.Errorf("feed URL must not contain credentials")
	}

	host := u.Hostname()
	if isUnsafeHostname(host) {
		return fmt.Errorf("host %q is not valid for fetching", host)
	}

	addrs, err := p.resolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("host %q resolved to no addresses", host)
	}

	for _, addr := range addrs {
		if isUnsafeIP(addr.IP) {
			return fmt.Errorf("host %q resolves to unsafe address %s", host, addr.IP)
		}
	}

	return nil
}

func isUnsafeHostname(host string) bool {
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return true
	}
	return false
}

func isUnsafeIP(ip net.IP) bool {
	if ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() {
		return true
	}
	// Shared address space (CGNAT / Tailscale): 100.64.0.0/10
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 100 && (ip4[1]&0xC0) == 64
	}
	return false
}
