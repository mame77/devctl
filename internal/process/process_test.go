package process

import (
	"net"
	"testing"
)

func TestPortInUse_FreePort(t *testing.T) {
	// Grab an ephemeral free port, then release it immediately so the
	// check below observes it as free.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	inUse, err := PortInUse(port)
	if err != nil {
		t.Fatalf("PortInUse returned error: %v", err)
	}
	if inUse {
		t.Fatalf("expected port %d to be free after closing listener", port)
	}
}

func TestPortInUse_OccupiedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	inUse, err := PortInUse(port)
	if err != nil {
		t.Fatalf("PortInUse returned error: %v", err)
	}
	if !inUse {
		t.Fatalf("expected port %d to be reported in use while listener is held", port)
	}
}

func TestPortInUse_InvalidPort(t *testing.T) {
	inUse, err := PortInUse(0)
	if err != nil {
		t.Fatalf("PortInUse returned error: %v", err)
	}
	if inUse {
		t.Fatalf("expected port 0 to be treated as not in use")
	}
}
