package probe

import (
	"net"
	"testing"
	"time"
)

// The test spins up its own listener on localhost so it does not depend
// on the internet and will not fail because of an unrelated network issue.
func TestTCPConnectSuccess(t *testing.T) {
	// Port 0 tells the OS to pick any free port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer listener.Close()

	elapsed, err := TCPConnect(listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("expected a successful connection, got error: %v", err)
	}
	if elapsed <= 0 {
		t.Errorf("expected a positive duration, got %v", elapsed)
	}
}

// Here the listener is closed immediately, so the port is guaranteed
// to be free and the connection must fail.
func TestTCPConnectFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	address := listener.Addr().String()
	listener.Close()

	if _, err := TCPConnect(address, time.Second); err == nil {
		t.Error("expected an error connecting to a closed port, got none")
	}
}

// A failed probe must still report how long it took: the detector uses
// the timing of an RST to tell an injected reset from a real refusal.
func TestTCPConnectFailureReportsElapsed(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	address := listener.Addr().String()
	listener.Close()

	elapsed, err := TCPConnect(address, time.Second)
	if err == nil {
		t.Fatal("expected an error connecting to a closed port, got none")
	}
	if elapsed <= 0 {
		t.Errorf("expected a positive duration on failure, got %v", elapsed)
	}
}
