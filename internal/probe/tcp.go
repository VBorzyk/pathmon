// Package probe provides network probes: ways to check whether a target
// is reachable and how long that takes.
package probe

import (
	"net"
	"time"
)

// TCPConnect establishes a TCP connection to address ("host:port")
// and reports how long the handshake took.
//
// On failure it returns a zero duration and the underlying error.
func TCPConnect(address string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()

	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return 0, err
	}

	// Connection established: measure before doing anything else.
	elapsed := time.Since(start)

	// We only care that the handshake completed, not about sending data,
	// so release the socket as soon as we leave the function.
	defer conn.Close()

	return elapsed, nil
}
