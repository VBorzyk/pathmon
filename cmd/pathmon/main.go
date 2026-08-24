package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/VBorzyk/pathmon/internal/config"
	"github.com/VBorzyk/pathmon/internal/probe"
)

// version is injected at build time via -ldflags (see Makefile).
var version = "dev"

func main() {
	// os.Args[0] is always the program path, so the first user argument
	// lives at index 1. Without this guard, indexing panics.
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "watch":
		if err := runWatch(args); err != nil {
			fmt.Fprintln(os.Stderr, "pathmon:", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println("pathmon", version)
	case "help":
		printUsage()
	default:
		// Errors go to stderr so they stay visible even when stdout
		// is redirected to a file.
		fmt.Fprintf(os.Stderr, "pathmon: unknown command %q\n\n", command)
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Println(`pathmon monitors host reachability and reports problems.

Usage:
  pathmon <command> [flags]

Commands:
  watch     run continuous monitoring
  version   print version information
  help      show this help

Flags for watch:
  -c, -config path    configuration file (default pathmon.yaml)
  -interval duration  override the interval from the config
  -once               run a single round and exit`)
}

// runWatch parses the flags of the watch command and then probes every
// target on a fixed schedule until the process is stopped.
func runWatch(args []string) error {
	// A FlagSet is a private set of flags for one subcommand, so "watch"
	// and future commands cannot collide.
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we print flag errors ourselves

	var (
		path     string
		interval time.Duration
		once     bool
	)
	// Registering the same variable twice is how stdlib flag does aliases.
	fs.StringVar(&path, "c", config.DefaultPath, "configuration file")
	fs.StringVar(&path, "config", config.DefaultPath, "configuration file")
	fs.DurationVar(&interval, "interval", 0, "override the configured interval")
	fs.BoolVar(&once, "once", false, "run a single round and exit")

	if err := fs.Parse(args); err != nil {
		printUsage()
		// -h and -help are not failures, they are a request for help.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("invalid flags: %w", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	// A flag beats the config file: that is the usual precedence and it
	// lets you re-run with a short interval without editing the file.
	if interval > 0 {
		cfg.Interval = interval
		// Load validated the file, but the override can break an invariant again.
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("-interval: %w", err)
		}
	}

	fmt.Printf("pathmon %s | host_id=%s | targets=%d | interval=%v\n\n",
		version, cfg.HostID, len(cfg.Targets), cfg.Interval)

	if once {
		runRound(cfg)
		return nil
	}

	// Schedule rounds against a fixed clock instead of sleeping for the
	// interval after each round. Sleeping would add the probe duration to
	// every cycle, so a 60s interval would slowly drift to 62s, 64s, ...
	next := time.Now()
	for {
		runRound(cfg)

		next = next.Add(cfg.Interval)
		wait := time.Until(next)
		if wait < 0 {
			// The round outran the interval. Skip the missed slots and
			// resync instead of piling rounds on top of each other.
			next = time.Now()
			continue
		}
		time.Sleep(wait)
	}
}

// runRound probes every target once and prints one line per target.
// One timestamp is taken for the whole round, so all its lines line up.
func runRound(cfg config.Config) {
	stamp := time.Now().Format("15:04:05")

	for _, target := range cfg.Targets {
		address := target.Address()

		elapsed, err := probe.TCPConnect(address, cfg.Timeout)
		if err != nil {
			fmt.Printf("%s  %-24s FAIL  %v\n", stamp, address, err)
			continue
		}

		fmt.Printf("%s  %-24s OK    %v\n", stamp, address, elapsed.Round(time.Microsecond))
	}
}
