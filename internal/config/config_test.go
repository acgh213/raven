package config

import (
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	getenv := func(key string) string {
		return "" // all unset
	}

	cfg, err := Load(getenv)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Addr != "127.0.0.1:8789" {
		t.Errorf("default Addr = %q, want %q", cfg.Addr, "127.0.0.1:8789")
	}
	if cfg.DataDir != "./data" {
		t.Errorf("default DataDir = %q, want %q", cfg.DataDir, "./data")
	}
	if cfg.RequestTimeout <= 0 {
		t.Errorf("default RequestTimeout = %v, want positive", cfg.RequestTimeout)
	}
	if cfg.APIToken != "" {
		t.Errorf("default APIToken = %q, want empty", cfg.APIToken)
	}
}

func TestExplicitValues(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "RAVEN_ADDR":
			return "0.0.0.0:9999"
		case "RAVEN_DATA_DIR":
			return "/var/raven/data"
		case "RAVEN_REQUEST_TIMEOUT":
			return "30s"
		case "RAVEN_API_TOKEN":
			return "super-secret-token-abc123"
		default:
			return ""
		}
	}

	cfg, err := Load(getenv)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Addr != "0.0.0.0:9999" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, "0.0.0.0:9999")
	}
	if cfg.DataDir != "/var/raven/data" {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, "/var/raven/data")
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Errorf("RequestTimeout = %v, want %v", cfg.RequestTimeout, 30*time.Second)
	}
	if cfg.APIToken != "super-secret-token-abc123" {
		t.Errorf("APIToken = %q, want %q", cfg.APIToken, "super-secret-token-abc123")
	}
}

func TestRejectMissingPortInAddress(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "RAVEN_ADDR":
			return "127.0.0.1" // no port
		case "RAVEN_DATA_DIR":
			return "./data"
		case "RAVEN_REQUEST_TIMEOUT":
			return "10s"
		default:
			return ""
		}
	}

	_, err := Load(getenv)
	if err == nil {
		t.Fatal("expected error for address missing port, got nil")
	}
}

func TestRejectBlankDataDir(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "RAVEN_ADDR":
			return "127.0.0.1:8789"
		case "RAVEN_DATA_DIR":
			return "   " // whitespace-only, effectively blank
		case "RAVEN_REQUEST_TIMEOUT":
			return "10s"
		default:
			return ""
		}
	}

	_, err := Load(getenv)
	if err == nil {
		t.Fatal("expected error for blank data dir, got nil")
	}
}

func TestRejectNonPositiveTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
	}{
		{"negative", "-5s"},
		{"zero", "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				switch key {
				case "RAVEN_ADDR":
					return "127.0.0.1:8789"
				case "RAVEN_DATA_DIR":
					return "./data"
				case "RAVEN_REQUEST_TIMEOUT":
					return tt.timeout
				default:
					return ""
				}
			}

			_, err := Load(getenv)
			if err == nil {
				t.Fatalf("expected error for timeout %q, got nil", tt.timeout)
			}
		})
	}
}

func TestRejectUnparseableTimeout(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "RAVEN_ADDR":
			return "127.0.0.1:8789"
		case "RAVEN_DATA_DIR":
			return "./data"
		case "RAVEN_REQUEST_TIMEOUT":
			return "not-a-duration"
		default:
			return ""
		}
	}

	_, err := Load(getenv)
	if err == nil {
		t.Fatal("expected error for unparseable timeout, got nil")
	}
}

func TestTokenNotInError(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "RAVEN_ADDR":
			return "127.0.0.1:8789"
		case "RAVEN_DATA_DIR":
			return "./data"
		case "RAVEN_REQUEST_TIMEOUT":
			return "10s"
		case "RAVEN_API_TOKEN":
			return "this-should-not-appear-12345"
		default:
			return ""
		}
	}

	cfg, err := Load(getenv)
	if err != nil {
		t.Fatalf("unexpected error with valid config: %v", err)
	}
	_ = cfg

	// Now test with an invalid config to see if token leaks
	badgetenv := func(key string) string {
		switch key {
		case "RAVEN_ADDR":
			return "127.0.0.1:8789"
		case "RAVEN_DATA_DIR":
			return "   " // blank
		case "RAVEN_REQUEST_TIMEOUT":
			return "10s"
		case "RAVEN_API_TOKEN":
			return "this-should-not-appear-12345"
		default:
			return ""
		}
	}
	_, err = Load(badgetenv)
	if err == nil {
		t.Fatal("expected error for blank data dir, got nil")
	}
	errStr := err.Error()
	if contains(errStr, "this-should-not-appear-12345") {
		t.Errorf("token leaked into error message: %q", errStr)
	}
}

// contains reports whether substr is within s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

// searchString is a simple substring search without importing strings.
func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestTokenNotInStringMethod checks that Config.String() does not expose the token.
func TestTokenNotInString(t *testing.T) {
	cfg := &Config{
		Addr:           "127.0.0.1:8789",
		DataDir:        "./data",
		RequestTimeout: 10 * time.Second,
		APIToken:       "hidden-token-value",
	}
	s := cfg.String()
	if contains(s, "hidden-token-value") {
		t.Errorf("token leaked via String(): %q", s)
	}
}
