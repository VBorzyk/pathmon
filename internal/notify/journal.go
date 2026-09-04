package notify

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/VBorzyk/pathmon/internal/detect"
)

// Record is one journal line. The field names are the on-disk format:
// other tools (jq, a future collector) read them, so renaming a JSON
// key is a breaking change even if the Go name stays the same.
type Record struct {
	// Time is in UTC. Journals from observers in different time zones
	// have to merge with a plain cat, without converting anything.
	Time time.Time `json:"ts"`
	// HostID names the observer, for the same reason: once files from
	// several hosts are concatenated, the line has to say where it came from.
	HostID string      `json:"host_id"`
	Target string      `json:"target"`
	Event  detect.Type `json:"event"`
	Detail string      `json:"detail"`
}

// Journal appends every event to an NDJSON file: one JSON object per
// line, nothing else. It has no cooldown and no batching on purpose.
// Delivery policy belongs to channels people read; the journal is the
// complete record those channels are allowed to thin out.
//
// Writes are unbuffered and each line goes out in a single Write call,
// so an interrupted process leaves no half-written line behind: the
// kernel finishes the write it was handed before the process dies.
type Journal struct {
	w      io.Writer
	hostID string
	closer io.Closer // nil when the writer is not ours to close
}

// NewJournal writes records to w. It exists for tests and for callers
// that manage the file themselves; watch uses OpenJournal.
func NewJournal(w io.Writer, hostID string) *Journal {
	return &Journal{w: w, hostID: hostID}
}

// OpenJournal opens (or creates) the file at path for appending and
// returns a Journal writing to it. Call Close when done.
func OpenJournal(path, hostID string) (*Journal, error) {
	// O_APPEND makes every write land at the current end of file, even
	// if another process (a second watch, an operator with >>) appends
	// to the same file. The 0o644 mode is the usual "owner writes,
	// everyone reads" for a log; the umask trims it further.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open journal: %w", err)
	}
	return &Journal{w: f, hostID: hostID, closer: f}, nil
}

// Notify appends one line per event. An empty batch writes nothing.
// It stops at the first write error: once the disk refuses a line,
// the following ones will fail the same way, and the caller should
// hear about it once, not once per event.
func (j *Journal) Notify(at time.Time, events []detect.Event) error {
	// Truncate drops sub-second precision, so ts reads as
	// "2026-09-05T14:22:01Z" rather than a nanosecond tail nobody uses.
	ts := at.UTC().Truncate(time.Second)

	for _, e := range events {
		rec := Record{
			Time:   ts,
			HostID: j.hostID,
			Target: e.Target,
			Event:  e.Type,
			Detail: e.Detail,
		}
		line, err := json.Marshal(rec)
		if err != nil {
			// Marshal fails only on values JSON cannot represent, and
			// Record has none; still, never swallow an error silently.
			return fmt.Errorf("journal: encode: %w", err)
		}
		// One Write for the whole line, newline included: appending the
		// newline separately would let another writer slip in between.
		if _, err := j.w.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("journal: write: %w", err)
		}
	}
	return nil
}

// Close releases the underlying file. It is safe to call on a Journal
// built with NewJournal, where there is nothing to close.
func (j *Journal) Close() error {
	if j.closer == nil {
		return nil
	}
	return j.closer.Close()
}

// ReadRecords parses a journal from r. Blank lines are ignored. A line
// that does not parse is skipped and counted rather than aborting the
// whole read: the history command must still show yesterday's events
// when the last line was cut short by a power loss.
func ReadRecords(r io.Reader) (records []Record, skipped int, err error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			skipped++
			continue
		}
		records = append(records, rec)
	}
	// Scanner reports read errors (and lines longer than its buffer)
	// through Err, not through Scan returning false.
	if err := sc.Err(); err != nil {
		return records, skipped, fmt.Errorf("read journal: %w", err)
	}
	return records, skipped, nil
}
