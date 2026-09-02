// Package state keeps what pathmon has learned about each target
// across probe rounds: the recent results and the current failure streak.
package state

import (
	"time"

	"github.com/VBorzyk/pathmon/internal/probe"
)

// WindowSize is how many recent probes are kept per target.
// Loss is computed over this window.
const WindowSize = 10

// Sample is the outcome of one probe of one target.
type Sample struct {
	Status  probe.Status
	Elapsed time.Duration // how long the probe took, on success and on failure alike
}

// History is everything remembered about one target. Its zero value is
// a target that has never been probed, so a map of histories needs no setup.
type History struct {
	// Window holds the last WindowSize samples, oldest first.
	Window []Sample
	// Streak counts consecutive failed probes; a success resets it to zero.
	Streak int
	// Total counts every probe ever made to this target.
	Total int
}

// Add records one more sample and returns the updated history.
// The receiver is a value, so the caller must store the result:
//
//	h = h.Add(sample)
func (h History) Add(s Sample) History {
	h.Window = append(h.Window, s)
	if len(h.Window) > WindowSize {
		// Drop the oldest sample so the window never grows past its size.
		h.Window = h.Window[1:]
	}

	if s.Status == probe.StatusOK {
		h.Streak = 0
	} else {
		h.Streak++
	}
	h.Total++

	return h
}

// Loss reports how many probes in the window failed and how many
// samples the window currently holds.
func (h History) Loss() (lost, total int) {
	for _, s := range h.Window {
		if s.Status != probe.StatusOK {
			lost++
		}
	}
	return lost, len(h.Window)
}
