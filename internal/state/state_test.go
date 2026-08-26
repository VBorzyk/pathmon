package state

import (
	"testing"

	"github.com/VBorzyk/pathmon/internal/probe"
)

func TestAddTracksStreak(t *testing.T) {
	var h History

	h = h.Add(Sample{Status: probe.StatusTimeout})
	h = h.Add(Sample{Status: probe.StatusTimeout})
	if h.Streak != 2 {
		t.Errorf("after two failures: streak %d, want 2", h.Streak)
	}

	h = h.Add(Sample{Status: probe.StatusOK})
	if h.Streak != 0 {
		t.Errorf("after a success: streak %d, want 0", h.Streak)
	}
	if h.Total != 3 {
		t.Errorf("total %d, want 3", h.Total)
	}
}

func TestWindowIsBounded(t *testing.T) {
	var h History
	for i := 0; i < WindowSize+5; i++ {
		h = h.Add(Sample{Status: probe.StatusOK})
	}
	if got := len(h.Window); got != WindowSize {
		t.Errorf("window holds %d samples, want %d", got, WindowSize)
	}
	if h.Total != WindowSize+5 {
		t.Errorf("total %d, want %d", h.Total, WindowSize+5)
	}
}

func TestWindowDropsOldest(t *testing.T) {
	var h History
	// Fill the window with failures, then push one success: the window
	// must now start with a failure that is one step younger.
	for i := 0; i < WindowSize; i++ {
		h = h.Add(Sample{Status: probe.StatusRefused})
	}
	h = h.Add(Sample{Status: probe.StatusOK})

	if last := h.Window[len(h.Window)-1]; last.Status != probe.StatusOK {
		t.Errorf("newest sample is %q, want %q", last.Status, probe.StatusOK)
	}
	lost, total := h.Loss()
	if lost != WindowSize-1 || total != WindowSize {
		t.Errorf("loss %d/%d, want %d/%d", lost, total, WindowSize-1, WindowSize)
	}
}

func TestLossOnEmptyHistory(t *testing.T) {
	var h History
	if lost, total := h.Loss(); lost != 0 || total != 0 {
		t.Errorf("empty history: loss %d/%d, want 0/0", lost, total)
	}
}
