package process

import (
	"net"
	"os"
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

func TestFindListeners_OwnProcess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	pids, err := FindListeners(port)
	if err != nil {
		t.Fatalf("FindListeners returned error: %v", err)
	}
	if len(pids) == 0 {
		t.Fatalf("expected own pid to be found listening on port %d", port)
	}
	found := false
	for _, p := range pids {
		if p == os.Getpid() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected own pid %d among listeners: %v", os.Getpid(), pids)
	}
}

func TestFindListeners_FreePort(t *testing.T) {
	pids, err := FindListeners(22)
	if err != nil {
		t.Fatalf("FindListeners returned error: %v", err)
	}
	// SSH (port 22) may or may not be running. We just verify the function
	// doesn't crash. If SSH is running, we should see at least one PID.
	_ = pids
}
