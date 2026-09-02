package probe

import (
	"net"
	"time"
)

// TCPConnect establishes a TCP connection to address ("host:port") and
// reports how long that took. When address holds a name rather than an IP,
// the measurement also covers the DNS lookup, because net.DialTimeout does
// both under one deadline.
//
// The duration is measured on failure too: an RST that arrives faster
// than a connection is normally established is a sign that something on
// the path answered instead of the host, so the timing of a failure is
// a diagnostic signal, not garbage.
func TCPConnect(address string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()

	conn, err := net.DialTimeout("tcp", address, timeout)
	elapsed := time.Since(start)
	if err != nil {
		return elapsed, err
	}

	// We only care that the handshake completed, not about sending data,
	// so release the socket as soon as we leave the function.
	defer conn.Close()

	return elapsed, nil
}
