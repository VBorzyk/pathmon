package main

import (
	"fmt"
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
  pathmon <command> [config-path]

Commands:
  watch     run continuous monitoring
  version   print version information
  help      show this help

The config path defaults to ./pathmon.yaml.`)
}

// runWatch probes every configured target once. The repeating loop
// comes next.
func runWatch(args []string) error {
	path := config.DefaultPath
	if len(args) > 0 {
		path = args[0]
	}

	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	fmt.Printf("host_id=%s interval=%v timeout=%v targets=%d\n\n",
		cfg.HostID, cfg.Interval, cfg.Timeout, len(cfg.Targets))

	for _, target := range cfg.Targets {
		address := target.Address()

		elapsed, err := probe.TCPConnect(address, cfg.Timeout)
		if err != nil {
			fmt.Printf("%-24s FAIL  %v\n", address, err)
			continue
		}

		fmt.Printf("%-24s OK    connect %v\n", address, elapsed.Round(time.Millisecond))
	}

	return nil
}
