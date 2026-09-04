package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/VBorzyk/pathmon/internal/config"
	"github.com/VBorzyk/pathmon/internal/detect"
	"github.com/VBorzyk/pathmon/internal/notify"
	"github.com/VBorzyk/pathmon/internal/probe"
	"github.com/VBorzyk/pathmon/internal/state"
)

// tokenEnv is the only place the Telegram bot token is read from. A flag
// or a config key would expose it in /proc/*/cmdline, ps and docker
// inspect to every user on the host.
const tokenEnv = "PATHMON_TELEGRAM_TOKEN"

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
	case "history":
		if err := runHistory(args); err != nil {
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
  history   print events from the journal
  version   print version information
  help      show this help

Flags for watch:
  -c, -config path    configuration file (default pathmon.yaml)
  -interval duration  override the interval from the config
  -once               run a single round and exit
  -no-notify          print events but do not send them to Telegram

Flags for history:
  -c, -config path    configuration file (default pathmon.yaml)
  -target host:port   only events for this target
  -since duration     only events newer than this (e.g. 24h)
  -n count            only the last N matching events

Environment:
  PATHMON_TELEGRAM_TOKEN   bot token; required when telegram.chat_id is set
  HTTPS_PROXY              proxy for Telegram delivery (standard Go semantics)`)
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
		noNotify bool
	)
	// Registering the same variable twice is how stdlib flag does aliases.
	fs.StringVar(&path, "c", config.DefaultPath, "configuration file")
	fs.StringVar(&path, "config", config.DefaultPath, "configuration file")
	fs.DurationVar(&interval, "interval", 0, "override the configured interval")
	fs.BoolVar(&once, "once", false, "run a single round and exit")
	fs.BoolVar(&noNotify, "no-notify", false, "print events but do not send them to Telegram")

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

	// Delivery chain, in the order events flow through it: the screen,
	// then the record on disk, then the chat. The journal comes before
	// Telegram so that an event is written down before anyone tries to
	// send it anywhere.
	notifiers := []notify.Notifier{notify.NewStdout(os.Stdout)}

	journal := "off"
	if cfg.Journal.Enabled() {
		j, err := notify.OpenJournal(cfg.Journal.Path, cfg.HostID)
		if err != nil {
			return err
		}
		// defer runs when runWatch returns: after -once, or never for
		// the endless loop, where the OS closes the file on exit.
		defer j.Close()
		notifiers = append(notifiers, j)
		journal = cfg.Journal.Path
	}

	telegramNotifier, telegram, err := buildTelegram(cfg, noNotify)
	if err != nil {
		return err
	}
	if telegramNotifier != nil {
		notifiers = append(notifiers, telegramNotifier)
	}

	fmt.Printf("pathmon %s | host_id=%s | targets=%d | interval=%v | journal=%s | telegram=%s\n\n",
		version, cfg.HostID, len(cfg.Targets), cfg.Interval, journal, telegram)

	// One history per target address, kept for the whole run. The map
	// starts empty: a missing key reads as a zero History, which is valid.
	histories := make(map[string]state.History)
	// The detector lives as long as the loop: it has to remember which
	// incidents are already reported to avoid repeating them every round.
	detector := detect.New()

	if once {
		runRound(cfg, histories, detector, notifiers)
		return nil
	}

	// Schedule rounds against a fixed clock instead of sleeping for the
	// interval after each round. Sleeping would add the probe duration to
	// every cycle, so a 60s interval would slowly drift to 62s, 64s, ...
	next := time.Now()
	for {
		runRound(cfg, histories, detector, notifiers)

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

// buildTelegram returns the Telegram notifier when a chat_id is
// configured and -no-notify is not set, and nil otherwise. The second
// return value is a short status word for the banner.
//
// The return type is the interface, not *notify.Telegram: a nil pointer
// stored in an interface is not a nil interface, and the caller's
// "!= nil" check would pass and then call Notify on nothing.
func buildTelegram(cfg config.Config, noNotify bool) (notify.Notifier, string, error) {
	switch {
	case !cfg.Telegram.Enabled():
		return nil, "off", nil
	case noNotify:
		return nil, "muted", nil
	}

	token := os.Getenv(tokenEnv)
	if token == "" {
		// Failing here is deliberate: a configured chat with no token
		// would run for hours silently sending nothing.
		return nil, "", fmt.Errorf("telegram.chat_id is set but %s is empty", tokenEnv)
	}
	return notify.NewTelegram(cfg.Telegram, token, cfg.HostID), "on", nil
}

// runRound probes every target once, updates its history and prints one
// line per target, then hands the events of the round to every notifier.
// One timestamp is taken for the whole round, so all its lines line up.
func runRound(cfg config.Config, histories map[string]state.History, detector *detect.Detector, notifiers []notify.Notifier) {
	now := time.Now()
	stamp := now.Format("15:04:05")
	var events []detect.Event

	for _, target := range cfg.Targets {
		address := target.Address()

		elapsed, err := probe.TCPConnect(address, cfg.Timeout)
		status := probe.Classify(err)

		// Maps hand out copies of their values, so the updated history
		// has to be written back under the same key.
		h := histories[address]
		h = h.Add(state.Sample{Status: status, Elapsed: elapsed})
		histories[address] = h

		lost, total := h.Loss()

		// On success the interesting detail is the connect time,
		// on failure it is the error text.
		detail := elapsed.Round(time.Microsecond).String()
		if err != nil {
			detail = err.Error()
		}

		fmt.Printf("%s  %-24s %-8s streak=%-3d loss=%d/%-3d %s\n",
			stamp, address, status, h.Streak, lost, total, detail)

		if event := detector.Check(address, h); event != nil {
			events = append(events, *event)
		}
	}

	// Every notifier sees the whole round, even an empty one: Telegram
	// uses empty rounds to retry a digest it failed to send. A delivery
	// failure is reported and the loop goes on; the events happened
	// whether or not anyone was told.
	for _, n := range notifiers {
		if err := n.Notify(now, events); err != nil {
			fmt.Fprintf(os.Stderr, "%s  notify: %v\n", stamp, err)
		}
	}
}

// runHistory prints events recorded in the journal, newest last, with
// optional filters. It reads the journal path from the same config
// watch uses, so the two commands never disagree about the file.
func runHistory(args []string) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		path   string
		target string
		since  time.Duration
		last   int
	)
	fs.StringVar(&path, "c", config.DefaultPath, "configuration file")
	fs.StringVar(&path, "config", config.DefaultPath, "configuration file")
	fs.StringVar(&target, "target", "", "only events for this target (host:port)")
	fs.DurationVar(&since, "since", 0, "only events newer than this duration")
	fs.IntVar(&last, "n", 0, "only the last N matching events")

	if err := fs.Parse(args); err != nil {
		printUsage()
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("invalid flags: %w", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if !cfg.Journal.Enabled() {
		return fmt.Errorf("journal.path is not set in %s: nothing to read", path)
	}

	f, err := os.Open(cfg.Journal.Path)
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	defer f.Close()

	records, skipped, err := notify.ReadRecords(f)
	if err != nil {
		return err
	}
	if skipped > 0 {
		// Loud but not fatal: the rest of the file is still useful.
		fmt.Fprintf(os.Stderr, "pathmon: %d unreadable line(s) skipped in %s\n", skipped, cfg.Journal.Path)
	}

	// Filters are applied in memory: the journal is small (one line per
	// event, not per probe) and this keeps the read path a single pass.
	cutoff := time.Time{}
	if since > 0 {
		cutoff = time.Now().Add(-since)
	}
	var matched []notify.Record
	for _, r := range records {
		if target != "" && r.Target != target {
			continue
		}
		if r.Time.Before(cutoff) {
			continue
		}
		matched = append(matched, r)
	}
	if last > 0 && len(matched) > last {
		matched = matched[len(matched)-last:]
	}

	// Times are stored in UTC and shown in local time, the same clock
	// the watch output uses, so an event is easy to find in both.
	for _, r := range matched {
		fmt.Printf("%s  %-14s %-24s %-12s %s\n",
			r.Time.Local().Format("2006-01-02 15:04:05"),
			r.HostID, r.Target, r.Event, strings.TrimSpace(r.Detail))
	}
	return nil
}
