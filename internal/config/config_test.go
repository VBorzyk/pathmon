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

func TestLoadTelegramDefaults(t *testing.T) {
	path := writeConfig(t, `
targets:
  - host: 1.1.1.1
telegram:
  chat_id: 123456789
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Telegram.Enabled() {
		t.Fatalf("telegram should be enabled when chat_id is set")
	}
	if cfg.Telegram.ChatID != 123456789 {
		t.Errorf("chat_id: got %d, want 123456789", cfg.Telegram.ChatID)
	}
	if cfg.Telegram.APIBase != DefaultTelegramAPIBase {
		t.Errorf("api_base: got %q, want %q", cfg.Telegram.APIBase, DefaultTelegramAPIBase)
	}
	if cfg.Telegram.Cooldown != 15*time.Minute {
		t.Errorf("cooldown: got %v, want 15m", cfg.Telegram.Cooldown)
	}
}

func TestLoadTelegramOverrides(t *testing.T) {
	path := writeConfig(t, `
targets:
  - host: 1.1.1.1
telegram:
  chat_id: -1001234567890
  api_base: https://tg-relay.example.com
  cooldown: 5m
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Telegram.ChatID != -1001234567890 {
		t.Errorf("chat_id: got %d, want -1001234567890", cfg.Telegram.ChatID)
	}
	if cfg.Telegram.APIBase != "https://tg-relay.example.com" {
		t.Errorf("api_base: got %q", cfg.Telegram.APIBase)
	}
	if cfg.Telegram.Cooldown != 5*time.Minute {
		t.Errorf("cooldown: got %v, want 5m", cfg.Telegram.Cooldown)
	}
}

func TestLoadWithoutTelegramIsDisabled(t *testing.T) {
	path := writeConfig(t, `
targets:
  - host: 1.1.1.1
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Telegram.Enabled() {
		t.Errorf("telegram should be disabled when chat_id is absent")
	}
}

func TestLoadRejectsBadTelegram(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"negative cooldown", `
targets:
  - host: 1.1.1.1
telegram:
  chat_id: 1
  cooldown: -1m
`},
		{"empty api_base", `
targets:
  - host: 1.1.1.1
telegram:
  chat_id: 1
  api_base: ""
`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, tc.body)); err == nil {
				t.Errorf("expected an error, got nil")
			}
		})
	}
}

func TestLoadJournal(t *testing.T) {
	off, err := Load(writeConfig(t, "targets:\n  - host: 1.1.1.1\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if off.Journal.Enabled() {
		t.Errorf("journal should be off when path is absent, got %q", off.Journal.Path)
	}

	on, err := Load(writeConfig(t, `
targets:
  - host: 1.1.1.1
journal:
  path: /var/lib/pathmon/events.jsonl
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !on.Journal.Enabled() {
		t.Fatalf("journal should be enabled when path is set")
	}
	if on.Journal.Path != "/var/lib/pathmon/events.jsonl" {
		t.Errorf("path: got %q", on.Journal.Path)
	}
}
