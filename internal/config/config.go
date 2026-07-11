package config

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// Config holds the service configuration.
type Config struct {
	Addr           string
	DataDir        string
	RequestTimeout time.Duration
	APIToken       string
}

// String returns a redacted representation of Config that does not expose
// the API token.
func (c *Config) String() string {
	token := "<unset>"
	if c.APIToken != "" {
		token = "<set>"
	}
	return fmt.Sprintf(
		"Config{Addr: %q, DataDir: %q, RequestTimeout: %v, APIToken: %s}",
		c.Addr, c.DataDir, c.RequestTimeout, token,
	)
}

// defaults for local development.
const (
	defaultAddr    = "127.0.0.1:8789"
	defaultDataDir = "./data"
	defaultTimeout = 15 * time.Second
)

// Load reads configuration from environment variables via the getenv function
// and returns a validated Config. getenv should return the value of the
// environment variable named by the key.
func Load(getenv func(string) string) (*Config, error) {
	addr := getenv("RAVEN_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	// Validate that the address has a host:port format.
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return nil, fmt.Errorf("config: RAVEN_ADDR %q: %v", addr, err)
	}

	dataDir := getenv("RAVEN_DATA_DIR")
	if dataDir == "" {
		dataDir = defaultDataDir
	}
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("config: RAVEN_DATA_DIR must not be blank")
	}

	timeoutStr := getenv("RAVEN_REQUEST_TIMEOUT")
	var timeout time.Duration
	if timeoutStr == "" {
		timeout = defaultTimeout
	} else {
		d, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("config: RAVEN_REQUEST_TIMEOUT %q: %v", timeoutStr, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("config: RAVEN_REQUEST_TIMEOUT must be positive, got %v", d)
		}
		timeout = d
	}

	token := getenv("RAVEN_API_TOKEN")

	return &Config{
		Addr:           addr,
		DataDir:        dataDir,
		RequestTimeout: timeout,
		APIToken:       token,
	}, nil
}
