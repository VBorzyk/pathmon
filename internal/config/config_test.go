package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeConfig drops a config file into a temporary directory that the
// testing package removes automatically, and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pathmon.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	return path
}

func TestLoadFillsDefaults(t *testing.T) {
	path := writeConfig(t, `
host_id: observer-1
targets:
  - host: 1.1.1.1
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HostID != "observer-1" {
		t.Errorf("host_id: got %q, want %q", cfg.HostID, "observer-1")
	}

	if cfg.Interval != 60*time.Second {
		t.Errorf("interval: got %v, want 60s", cfg.Interval)
	}
	if cfg.Timeout != 2*time.Second {
		t.Errorf("timeout: got %v, want 2s", cfg.Timeout)
	}
	if cfg.Targets[0].Port != 443 {
		t.Errorf("port: got %d, want 443", cfg.Targets[0].Port)
	}
	if got, want := cfg.Targets[0].Address(), "1.1.1.1:443"; got != want {
		t.Errorf("address: got %q, want %q", got, want)
	}
}

func TestLoadReadsValues(t *testing.T) {
	path := writeConfig(t, `
host_id: observer-1
interval: 30s
timeout: 5s
targets:
  - host: example.com
    port: 8443
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Interval != 30*time.Second {
		t.Errorf("interval: got %v, want 30s", cfg.Interval)
	}
	if got, want := cfg.Targets[0].Address(), "example.com:8443"; got != want {
		t.Errorf("address: got %q, want %q", got, want)
	}
}

func TestLoadRejectsBadConfigs(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"no targets", "host_id: a\ntargets: []\n"},
		{"empty host", "host_id: a\ntargets:\n  - port: 443\n"},
		{"port too large", "host_id: a\ntargets:\n  - host: x\n    port: 70000\n"},
		{"negative interval", "host_id: a\ninterval: -5s\ntargets:\n  - host: x\n"},
		{"timeout above interval", "host_id: a\ninterval: 1s\ntimeout: 5s\ntargets:\n  - host: x\n"},
		{"broken yaml", "host_id: [unclosed\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, tt.body)); err == nil {
				t.Error("expected an error, got none")
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("expected an error for a missing file, got none")
	}
}
