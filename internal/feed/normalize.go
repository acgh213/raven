package feed

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"raven/internal/model"
)

// CanonicalizeURL validates the syntactic boundary for a subscribed feed URL
// and returns a stable representation suitable for uniqueness checks. Fetcher
// policy performs the separate DNS/IP and redirect safety checks at request
// time; this function does not claim to make a URL safe to dial.
func CanonicalizeURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("feed URL is blank")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse feed URL: %w", err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("feed URL must use http or https")
	}
	if u.User != nil {
		return "", fmt.Errorf("feed URL must not contain credentials")
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("feed URL must include a host")
	}

	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port == "" {
		if strings.Contains(host, ":") {
			u.Host = "[" + host + "]"
		} else {
			u.Host = host
		}
	} else {
		u.Host = net.JoinHostPort(host, port)
	}
	if u.Path == "" {
		u.Path = "/"
	}
	u.Fragment = ""
	u.RawFragment = ""

	return u.String(), nil
}

// ValidateCandidates returns canonical feed candidates in input order and
// records individual invalid URLs without discarding otherwise useful imports.
func ValidateCandidates(candidates []model.FeedCandidate) ([]model.FeedCandidate, []model.FeedCandidateRejection) {
	valid := make([]model.FeedCandidate, 0, len(candidates))
	var rejected []model.FeedCandidateRejection
	for _, candidate := range candidates {
		canonicalURL, err := CanonicalizeURL(candidate.URL)
		if err != nil {
			rejected = append(rejected, model.FeedCandidateRejection{
				URL:    candidate.URL,
				Reason: err.Error(),
			})
			continue
		}
		candidate.URL = canonicalURL
		valid = append(valid, candidate)
	}
	return valid, rejected
}
