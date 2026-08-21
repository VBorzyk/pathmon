package main

import (
	"fmt"
	"os"
	"time"

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

	switch command {
	case "watch":
		runWatch()
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
  pathmon <command>

Commands:
  watch     run continuous monitoring
  version   print version information
  help      show this help`)
}

// runWatch currently probes a single hardcoded target once.
// The target list moves into a config file next.
func runWatch() {
	const address = "1.1.1.1:443"
	const timeout = 2 * time.Second

	elapsed, err := probe.TCPConnect(address, timeout)
	if err != nil {
		fmt.Printf("%-22s FAIL  %v\n", address, err)
		return
	}

	fmt.Printf("%-22s OK    connect %v\n", address, elapsed.Round(time.Millisecond))
}
