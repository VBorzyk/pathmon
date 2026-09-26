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
	StatusTimeout Status = "timeout" // deadline exceeded; excludes known DNS errors
	StatusRefused Status = "refused" // connection refused; does not identify who rejected it
	StatusError   Status = "error"   // anything else: DNS failure, no route, ...
)

// Classify turns the error from TCPConnect into a Status.
// A nil error means the connection succeeded.
func Classify(err error) Status {
	if err == nil {
		return StatusOK
	}

	// DNS errors can also report Timeout() == true. Check them first:
	// a failed lookup must not become evidence of a TCP blackhole.
	// Dial wraps the cause in *net.OpError; errors.As looks through it.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return StatusError
	}

	// Classify the remaining timeouts without depending on error text.
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
