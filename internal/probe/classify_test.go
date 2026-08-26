package probe

import (
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

func TestClassifyOK(t *testing.T) {
	if got := Classify(nil); got != StatusOK {
		t.Errorf("nil error: got %q, want %q", got, StatusOK)
	}
}

// os.ErrDeadlineExceeded is the standard "deadline hit" error and reports
// Timeout() == true, so it stands in for a real dial timeout here.
func TestClassifyTimeout(t *testing.T) {
	err := fmt.Errorf("dial tcp: %w", os.ErrDeadlineExceeded)
	if got := Classify(err); got != StatusTimeout {
		t.Errorf("deadline error: got %q, want %q", got, StatusTimeout)
	}
}

// A closed port on localhost answers with RST at once, which is exactly
// the "refused" case, and it needs no network.
func TestClassifyRefused(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	address := listener.Addr().String()
	listener.Close()

	_, err = TCPConnect(address, time.Second)
	if got := Classify(err); got != StatusRefused {
		t.Errorf("closed port: got %q (%v), want %q", got, err, StatusRefused)
	}
}

func TestClassifyOther(t *testing.T) {
	err := fmt.Errorf("something unrelated")
	if got := Classify(err); got != StatusError {
		t.Errorf("plain error: got %q, want %q", got, StatusError)
	}
}
