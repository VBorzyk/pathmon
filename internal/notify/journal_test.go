package notify

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VBorzyk/pathmon/internal/detect"
)

func TestJournalWritesOneLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	j := NewJournal(&buf, "ru-obs")

	// A non-UTC time with sub-second precision: the journal must
	// normalise both.
	msk := time.FixedZone("MSK", 3*60*60)
	at := time.Date(2026, 9, 5, 17, 22, 1, 987654321, msk)

	events := []detect.Event{
		{Type: detect.TypeBlackhole, Target: "vpn.example.com:443", Detail: "5 timeouts in a row"},
		{Type: detect.TypeRecovered, Target: "1.1.1.1:443", Detail: "connect ok again"},
	}
	if err := j.Notify(at, events); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}

	// The on-disk format is part of the contract, so check the exact
	// text of one line, not just that it decodes.
	want := `{"ts":"2026-09-05T14:22:01Z","host_id":"ru-obs","target":"vpn.example.com:443","event":"blackhole","detail":"5 timeouts in a row"}`
	if lines[0] != want {
		t.Errorf("line 0:\n got %s\nwant %s", lines[0], want)
	}

	records, skipped, err := ReadRecords(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("ReadRecords: %v", err)
	}
	if skipped != 0 {
		t.Errorf("skipped: got %d, want 0", skipped)
	}
	if len(records) != 2 {
		t.Fatalf("records: got %d, want 2", len(records))
	}
	if records[1].Event != detect.TypeRecovered || records[1].Target != "1.1.1.1:443" {
		t.Errorf("record 1: got %+v", records[1])
	}
	if loc := records[0].Time.Location(); loc != time.UTC {
		t.Errorf("time zone after round-trip: got %v, want UTC", loc)
	}
}

func TestJournalHasNoCooldown(t *testing.T) {
	// Telegram mutes repeats; the journal must not. If someone ever
	// "helpfully" adds a cooldown here, this test goes red.
	var buf bytes.Buffer
	j := NewJournal(&buf, "h")
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	e := []detect.Event{{Type: detect.TypeSlowConnect, Target: "x:443", Detail: "d"}}

	for i := 0; i < 3; i++ {
		if err := j.Notify(at.Add(time.Duration(i)*time.Minute), e); err != nil {
			t.Fatalf("Notify %d: %v", i, err)
		}
	}
	if got := strings.Count(buf.String(), "\n"); got != 3 {
		t.Errorf("got %d lines, want 3 (repeats must not be suppressed)", got)
	}
}

func TestJournalEmptyRoundWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	j := NewJournal(&buf, "h")
	if err := j.Notify(time.Now(), nil); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("empty round wrote %q", buf.String())
	}
}

func TestOpenJournalAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	e := []detect.Event{{Type: detect.TypeBlackhole, Target: "x:443", Detail: "d"}}

	// Two separate open/write/close cycles stand in for two runs of
	// watch. The second must add to the file, not replace it.
	for i := 0; i < 2; i++ {
		j, err := OpenJournal(path, "h")
		if err != nil {
			t.Fatalf("OpenJournal: %v", err)
		}
		if err := j.Notify(at, e); err != nil {
			t.Fatalf("Notify: %v", err)
		}
		if err := j.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := strings.Count(string(data), "\n"); got != 2 {
		t.Errorf("got %d lines after two runs, want 2:\n%s", got, data)
	}
}

func TestReadRecordsSkipsBrokenLines(t *testing.T) {
	// A blank line, a torn last line (power loss mid-write) and one
	// good record. The good one must come through, the torn one must
	// be counted, and nothing must abort the read.
	input := "\n" +
		`{"ts":"2026-09-05T12:00:00Z","host_id":"h","target":"x:443","event":"blackhole","detail":"d"}` + "\n" +
		`{"ts":"2026-09-05T12:01:00Z","host_id":"h","tar`

	records, skipped, err := ReadRecords(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ReadRecords: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("records: got %d, want 1", len(records))
	}
	if skipped != 1 {
		t.Errorf("skipped: got %d, want 1", skipped)
	}
}
