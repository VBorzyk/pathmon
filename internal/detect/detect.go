// Package detect turns per-target histories into events: it decides when
// a run of failures is a blackhole, when an RST came suspiciously fast,
// and when a target has recovered. It works on state.History only and
// knows nothing about printing or Telegram, so those layers can change
// without touching the rules here.
package detect

import (
	"fmt"
	"sort"
	"time"

	"github.com/VBorzyk/pathmon/internal/probe"
	"github.com/VBorzyk/pathmon/internal/state"
)

// Tunables of the detector. Constants for now; they move into the config
// file once a soak run shows which of them actually need tuning.
const (
	// BlackholeStreak is how many timeouts in a row confirm a blackhole.
	// A single timeout happens on any congested network; a series while
	// SYNs keep leaving is the signature of traffic being dropped.
	BlackholeStreak = 5

	// MinBaseline is how many successful samples the window must hold
	// before timing-based rules fire. With fewer points the median is
	// noise and every conclusion from it is a coin flip.
	MinBaseline = 5

	// SlowK scales the deviation that makes a connect time anomalous:
	// anything above median + SlowK*scale is reported.
	SlowK = 4.0

	// EarlyRSTFactor: a refusal faster than this fraction of the median
	// connect time is treated as injected. A real RST travels the same
	// path as a SYN-ACK, so it cannot be radically faster than one.
	EarlyRSTFactor = 0.5
)

// Type names a detected condition. These strings end up in alerts and
// in the event journal, so they are part of the public behaviour.
type Type string

const (
	TypeBlackhole   Type = "blackhole"
	TypeRSTInjected Type = "rst_injected"
	TypeSlowConnect Type = "slow_connect"
	TypeRecovered   Type = "recovered"
)

// Event is one detected condition on one target.
type Event struct {
	Type   Type
	Target string
	Detail string // human-readable explanation, already formatted
}

// Detector remembers which condition is currently active on each target,
// so a blackhole that lasts an hour produces one event, not sixty.
type Detector struct {
	active map[string]Type
}

// New returns a Detector with no active conditions.
func New() *Detector {
	return &Detector{active: make(map[string]Type)}
}

// Check inspects the newest sample of a history and reports at most one
// event. Call it right after History.Add, once per probe.
func (d *Detector) Check(target string, h state.History) *Event {
	if len(h.Window) == 0 {
		return nil
	}
	newest := h.Window[len(h.Window)-1]
	// The baseline deliberately excludes the newest sample: it is the
	// point being judged, so it must not influence its own verdict.
	median, scale, points := baseline(h.Window[:len(h.Window)-1])

	switch newest.Status {
	case probe.StatusOK:
		slow := points >= MinBaseline &&
			float64(newest.Elapsed) > float64(median)+SlowK*float64(scale)

		// A slow connect that stays slow is one lasting incident.
		// Announcing recovery just because the probe succeeded would
		// flap: slow_connect, recovered, slow_connect, ... every round.
		if d.active[target] == TypeSlowConnect && slow {
			return nil
		}
		if t, ok := d.active[target]; ok && !slow {
			delete(d.active, target)
			return &Event{
				Type:   TypeRecovered,
				Target: target,
				Detail: fmt.Sprintf("recovered after %s, connect %v", t, round(newest.Elapsed)),
			}
		}
		if slow {
			d.active[target] = TypeSlowConnect
			return &Event{
				Type:   TypeSlowConnect,
				Target: target,
				Detail: fmt.Sprintf("connect %v vs median %v", round(newest.Elapsed), round(median)),
			}
		}
		return nil

	case probe.StatusTimeout:
		if d.active[target] == TypeBlackhole {
			return nil
		}
		if run := trailingTimeouts(h.Window); run >= BlackholeStreak {
			d.active[target] = TypeBlackhole
			return &Event{
				Type:   TypeBlackhole,
				Target: target,
				Detail: fmt.Sprintf("%d timeouts in a row: SYNs leave, nothing answers", run),
			}
		}
		return nil

	case probe.StatusRefused:
		if d.active[target] == TypeRSTInjected {
			return nil
		}
		if points >= MinBaseline && float64(newest.Elapsed) < EarlyRSTFactor*float64(median) {
			d.active[target] = TypeRSTInjected
			return &Event{
				Type:   TypeRSTInjected,
				Target: target,
				Detail: fmt.Sprintf("RST after %v, median connect is %v: answered from the path, not the host", round(newest.Elapsed), round(median)),
			}
		}
		return nil
	}

	return nil
}

// baseline computes the median connect time of the successful samples,
// a robust scale around it, and how many points went into both.
//
// The scale is 1.4826*MAD with a floor of max(median/4, 1ms): on a
// stable path MAD collapses to zero, and without the floor any jitter
// would read as an infinite deviation.
func baseline(window []state.Sample) (median, scale time.Duration, points int) {
	var times []time.Duration
	for _, s := range window {
		if s.Status == probe.StatusOK {
			times = append(times, s.Elapsed)
		}
	}
	if len(times) == 0 {
		return 0, 0, 0
	}

	median = medianOf(times)

	// MAD: the median of absolute deviations from the median.
	deviations := make([]time.Duration, len(times))
	for i, t := range times {
		d := t - median
		if d < 0 {
			d = -d
		}
		deviations[i] = d
	}
	scale = time.Duration(1.4826 * float64(medianOf(deviations)))

	// A quarter of the median keeps the alert bar at roughly "connect
	// time doubled": with SlowK=4 the minimum threshold is 2*median.
	// A tenth was tried first and produced noise on stable anchors,
	// where a few extra milliseconds of jitter crossed the bar.
	if floor := median / 4; scale < floor {
		scale = floor
	}
	if scale < time.Millisecond {
		scale = time.Millisecond
	}
	return median, scale, len(times)
}

// medianOf sorts a copy of the slice and returns its middle element
// (the mean of the two middle elements for an even length).
func medianOf(values []time.Duration) time.Duration {
	sorted := make([]time.Duration, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// trailingTimeouts counts how many samples at the end of the window are
// timeouts. Unlike History.Streak it ignores other failure kinds: five
// refusals are a dead service, not a blackhole.
func trailingTimeouts(window []state.Sample) int {
	run := 0
	for i := len(window) - 1; i >= 0; i-- {
		if window[i].Status != probe.StatusTimeout {
			break
		}
		run++
	}
	return run
}

func round(d time.Duration) time.Duration {
	return d.Round(time.Microsecond)
}
