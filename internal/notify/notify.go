// Package notify delivers detected events to people. It sits after
// detect in the pipeline: detect decides what happened, notify decides
// who hears about it and how often. Delivery policy (cooldown, retry)
// lives here and not in the detector, because the journal must record
// every event while a chat should not be flooded with repeats.
package notify

import (
	"fmt"
	"io"
	"time"

	"github.com/VBorzyk/pathmon/internal/detect"
)

// Notifier receives the events of one probe round. Implementations are
// called once per round with the whole batch, so a channel that groups
// messages (Telegram) can send one digest instead of one message per
// event, and a channel that prints (stdout) can print them all in order.
//
// An empty batch is a valid call: a Notifier may use it to retry
// something it failed to deliver last time.
type Notifier interface {
	Notify(at time.Time, events []detect.Event) error
}

// Stdout prints events as marked lines in the watch output.
type Stdout struct {
	w io.Writer
}

// NewStdout returns a Stdout notifier writing to w.
func NewStdout(w io.Writer) *Stdout {
	return &Stdout{w: w}
}

// Notify prints one "!!" line per event, in the same column layout as
// the probe lines, so the round reads as one block.
func (s *Stdout) Notify(at time.Time, events []detect.Event) error {
	stamp := at.Format("15:04:05")
	for _, e := range events {
		fmt.Fprintf(s.w, "%s  %-24s !! %s: %s\n", stamp, e.Target, e.Type, e.Detail)
	}
	return nil
}
