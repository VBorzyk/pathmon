package probe

import (
	"errors"
	"net"
	"syscall"
)

// Status says how a probe ended. The detector works with these values,
// not with raw error text, so the text can change without breaking it.
type Status string

const (
	StatusOK      Status = "ok"      // handshake completed
	StatusTimeout Status = "timeout" // no reply before the deadline: silence on the path
	StatusRefused Status = "refused" // the peer replied with RST: the host is up, nothing listens
	StatusError   Status = "error"   // anything else: DNS failure, no route, ...
)

// Classify turns the error from TCPConnect into a Status.
// A nil error means the connection succeeded.
func Classify(err error) Status {
	if err == nil {
		return StatusOK
	}

	// Every error from the net package implements net.Error, which can
	// tell a timeout apart from other failures without looking at text.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return StatusTimeout
	}

	// A refused connection surfaces as the ECONNREFUSED errno, wrapped
	// several layers deep; errors.Is walks the chain for us.
	if errors.Is(err, syscall.ECONNREFUSED) {
		return StatusRefused
	}

	return StatusError
}
