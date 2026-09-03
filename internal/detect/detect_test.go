package detect

import (
	"testing"
	"time"

	"github.com/VBorzyk/pathmon/internal/probe"
	"github.com/VBorzyk/pathmon/internal/state"
)

// ok returns a successful sample with the given connect time.
func ok(elapsed time.Duration) state.Sample {
	return state.Sample{Status: probe.StatusOK, Elapsed: elapsed}
}

// feed runs a sequence of samples through one detector and returns every
// event it produced, in order. This mirrors how runRound drives it.
func feed(t *testing.T, samples []state.Sample) []Event {
	t.Helper()
	d := New()
	var h state.History
	var events []Event
	for _, s := range samples {
		h = h.Add(s)
		if e := d.Check("test-target", h); e != nil {
			events = append(events, *e)
		}
	}
	return events
}

// stable is a baseline of successful probes around 20ms, long enough
// to satisfy MinBaseline.
func stable() []state.Sample {
	return []state.Sample{
		ok(19 * time.Millisecond), ok(20 * time.Millisecond),
		ok(21 * time.Millisecond), ok(20 * time.Millisecond),
		ok(19 * time.Millisecond), ok(20 * time.Millisecond),
	}
}

func TestDetect(t *testing.T) {
	timeout := state.Sample{Status: probe.StatusTimeout}

	tests := []struct {
		name    string
		samples []state.Sample
		want    []Type
	}{
		{
			name:    "healthy target stays silent",
			samples: stable(),
			want:    nil,
		},
		{
			name: "single timeout is not a blackhole",
			samples: append(stable(),
				timeout,
				ok(20*time.Millisecond)),
			want: nil,
		},
		{
			name: "timeout series confirms blackhole once",
			samples: append(stable(),
				timeout, timeout, timeout, timeout, timeout, // 5th confirms
				timeout, timeout), // still the same incident: no new events
			want: []Type{TypeBlackhole},
		},
		{
			name: "recovery after blackhole is reported",
			samples: append(stable(),
				timeout, timeout, timeout, timeout, timeout,
				ok(20*time.Millisecond)),
			want: []Type{TypeBlackhole, TypeRecovered},
		},
		{
			name: "refusals are not a blackhole",
			samples: append(stable(),
				// A dead service refuses in about the usual round-trip
				// time, so this must not look like an injected RST either.
				state.Sample{Status: probe.StatusRefused, Elapsed: 18 * time.Millisecond},
				state.Sample{Status: probe.StatusRefused, Elapsed: 19 * time.Millisecond},
				state.Sample{Status: probe.StatusRefused, Elapsed: 20 * time.Millisecond},
				state.Sample{Status: probe.StatusRefused, Elapsed: 18 * time.Millisecond},
				state.Sample{Status: probe.StatusRefused, Elapsed: 19 * time.Millisecond}),
			want: nil,
		},
		{
			name: "early RST reads as injected",
			samples: append(stable(),
				// 2ms against a 20ms median: the answer came from the
				// path, not from the host 20ms away.
				state.Sample{Status: probe.StatusRefused, Elapsed: 2 * time.Millisecond}),
			want: []Type{TypeRSTInjected},
		},
		{
			name: "slow connect against a stable baseline",
			samples: append(stable(),
				ok(200*time.Millisecond)),
			want: []Type{TypeSlowConnect},
		},
		{
			name: "no timing rules without a baseline",
			samples: []state.Sample{
				// Only two successes: not enough points for a median,
				// so an early RST right after must stay silent.
				ok(20 * time.Millisecond), ok(20 * time.Millisecond),
				{Status: probe.StatusRefused, Elapsed: 1 * time.Millisecond},
			},
			want: nil,
		},
		{
			name: "lasting slow connect is one incident, not a flap",
			samples: append(stable(),
				ok(80*time.Millisecond),  // slow: event
				ok(85*time.Millisecond),  // still slow: same incident
				ok(90*time.Millisecond),  // still slow: same incident
				ok(20*time.Millisecond)), // back to normal: recovered
			want: []Type{TypeSlowConnect, TypeRecovered},
		},
		{
			name: "small jitter on a stable anchor stays silent",
			// Taken from a real overnight run: median 10.5ms, probe
			// 15.2ms. With the old median/10 floor this fired.
			samples: append(stableAnchor(), ok(15200*time.Microsecond)),
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := feed(t, tt.samples)

			var got []Type
			for _, e := range events {
				got = append(got, e.Type)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got events %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("event %d: got %v, want %v", i, got, tt.want)
				}
			}
		})
	}
}

func TestMedianOf(t *testing.T) {
	odd := []time.Duration{30, 10, 20}
	if got := medianOf(odd); got != 20 {
		t.Errorf("odd length: got %v, want 20", got)
	}
	even := []time.Duration{40, 10, 20, 30}
	if got := medianOf(even); got != 25 {
		t.Errorf("even length: got %v, want 25", got)
	}
	// The input must come back untouched: medianOf sorts a copy.
	if odd[0] != 30 || odd[1] != 10 || odd[2] != 20 {
		t.Errorf("input slice was reordered: %v", odd)
	}
}

func TestBaselineFloor(t *testing.T) {
	// Identical samples give MAD = 0; the floor must keep the scale
	// from collapsing, or any jitter would be an infinite deviation.
	window := []state.Sample{
		ok(20 * time.Millisecond), ok(20 * time.Millisecond),
		ok(20 * time.Millisecond), ok(20 * time.Millisecond),
		ok(20 * time.Millisecond),
	}
	median, scale, points := baseline(window)
	if points != 5 {
		t.Fatalf("points = %d, want 5", points)
	}
	if median != 20*time.Millisecond {
		t.Errorf("median = %v, want 20ms", median)
	}
	if want := 5 * time.Millisecond; scale != want { // median/4
		t.Errorf("scale = %v, want %v", scale, want)
	}
}

// stableAnchor mimics a low-latency anchor like a public DNS resolver:
// a tight baseline around 10.5ms where MAD is close to zero.
func stableAnchor() []state.Sample {
	return []state.Sample{
		ok(10400 * time.Microsecond),
		ok(10500 * time.Microsecond),
		ok(10600 * time.Microsecond),
		ok(10500 * time.Microsecond),
		ok(10450 * time.Microsecond),
	}
}
