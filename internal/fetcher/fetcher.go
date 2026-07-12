package fetcher

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
)

const ravenUserAgent = "Raven/0.1.0"

const defaultMaxResponseBytes = 10 << 20 // 10 MiB
const defaultMaxRedirects = 5

// Client is a bounded, policy-enforcing HTTP client for feed and article
// retrieval. It checks every URL (initial and redirect targets) against its
// Policy before making a request, caps response bodies, and limits redirect
// chains.
type Client struct {
	policy           Policy
	MaxResponseBytes int64
	MaxRedirects     int
}

// NewClient creates a Client that enforces p on every request.
func NewClient(p Policy) *Client {
	return &Client{
		policy:           p,
		MaxResponseBytes: defaultMaxResponseBytes,
		MaxRedirects:     defaultMaxRedirects,
	}
}

// Fetch retrieves url and returns its response. The caller must close
// resp.Body. Returns an error if the URL is rejected by the policy, the
// request fails, the response body exceeds the configured cap, or any
// redirect target is unsafe.
func (c *Client) Fetch(url string) (*http.Response, error) {
	if err := c.policy.Allow(url); err != nil {
		return nil, fmt.Errorf("unsafe initial URL: %w", err)
	}

	redirectCount := 0
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirectCount++
			if redirectCount > c.MaxRedirects {
				return fmt.Errorf("exceeded maximum of %d redirects", c.MaxRedirects)
			}
			if err := c.policy.Allow(req.URL.String()); err != nil {
				return fmt.Errorf("unsafe redirect target: %w", err)
			}
			return nil
		},
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", ravenUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %q: %w", url, err)
	}

	resp.Body = &cappedReader{
		ReadCloser: resp.Body,
		limit:      c.MaxResponseBytes,
	}

	return resp, nil
}

type cappedReader struct {
	io.ReadCloser
	limit int64
	read  int64
}

func (r *cappedReader) Read(p []byte) (int, error) {
	if r.read >= r.limit {
		return 0, fmt.Errorf("response body exceeds limit of %s bytes",
			formatBytes(r.limit))
	}
	n, err := r.ReadCloser.Read(p)
	r.read += int64(n)
	if r.read > r.limit {
		return n, fmt.Errorf("response body exceeds limit of %s bytes",
			formatBytes(r.limit))
	}
	return n, err
}

func formatBytes(n int64) string {
	if n >= 1<<20 {
		return strconv.FormatInt(n>>20, 10) + " MiB"
	}
	if n >= 1<<10 {
		return strconv.FormatInt(n>>10, 10) + " KiB"
	}
	return strconv.FormatInt(n, 10)
}
