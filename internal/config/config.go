// Package config loads and validates the pathmon configuration file.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultPath is where pathmon looks for its configuration
// when no other path is given.
const DefaultPath = "pathmon.yaml"

// Target is a single host to probe.
type Target struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// Address returns the target in "host:port" form, ready for net.Dial.
func (t Target) Address() string {
	return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

// Config is the whole configuration file.
type Config struct {
	// HostID tags every alert so you can tell observers apart.
	HostID string `yaml:"host_id"`
	// Interval is the pause between probe rounds.
	Interval time.Duration `yaml:"interval"`
	// Timeout is how long a single connection attempt may take.
	Timeout time.Duration `yaml:"timeout"`
	// Targets is the list of hosts to watch.
	Targets []Target `yaml:"targets"`
}

// Load reads the configuration file at path, fills in defaults
// for anything the user left out, and checks that the result makes sense.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	// Defaults are set before parsing: the YAML parser only overwrites
	// keys that are actually present in the file.
	cfg := Config{
		Interval: 60 * time.Second,
		Timeout:  2 * time.Second,
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if cfg.HostID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return Config{}, fmt.Errorf("host_id is not set and hostname is unavailable: %w", err)
		}
		cfg.HostID = hostname
	}

	// Port defaults to 443. Ranging by index is required here:
	// "for _, t := range" would hand out copies, and writing to a copy
	// would not change the slice.
	for i := range cfg.Targets {
		if cfg.Targets[i].Port == 0 {
			cfg.Targets[i].Port = 443
		}
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// validate rejects configurations that would make the watcher useless
// or crash it later, and says exactly what is wrong.
//
// Load calls it on every config it returns. It is exported because a command
// line flag can override a field after loading and break an invariant again.
func (cfg Config) Validate() error {
	if cfg.Interval <= 0 {
		return fmt.Errorf("interval must be positive, got %v", cfg.Interval)
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got %v", cfg.Timeout)
	}
	if cfg.Timeout >= cfg.Interval {
		return fmt.Errorf("timeout (%v) must be shorter than interval (%v)", cfg.Timeout, cfg.Interval)
	}
	if len(cfg.Targets) == 0 {
		return fmt.Errorf("no targets configured")
	}

	for i, t := range cfg.Targets {
		if t.Host == "" {
			return fmt.Errorf("target %d: host is empty", i+1)
		}
		if t.Port < 1 || t.Port > 65535 {
			return fmt.Errorf("target %d (%s): port %d is out of range 1-65535", i+1, t.Host, t.Port)
		}
	}

	return nil
}
