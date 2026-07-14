package process

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// OnExit is called after the process exits (from a background goroutine).
type OnExit func(exitCode int)

func Start(command, cwd, logPath string, onExit OnExit) (pid, pgid int, err error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, 0, fmt.Errorf("open log: %w", err)
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = cwd
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, 0, fmt.Errorf("start: %w", err)
	}

	pid = cmd.Process.Pid
	pgid, err = syscall.Getpgid(pid)
	if err != nil {
		pgid = pid
	}

	go func() {
		waitErr := cmd.Wait()
		logFile.Close()
		code := 0
		if waitErr != nil {
			if ee, ok := waitErr.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		if onExit != nil {
			onExit(code)
		}
	}()

	return pid, pgid, nil
}

func Kill(pid, pgid int) error {
	target := pgid
	if target <= 0 {
		target = pid
	}
	if target <= 0 {
		return fmt.Errorf("invalid pid/pgid")
	}

	_ = syscall.Kill(-target, syscall.SIGTERM)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !Alive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	_ = syscall.Kill(-target, syscall.SIGKILL)
	time.Sleep(100 * time.Millisecond)
	if Alive(pid) {
		return fmt.Errorf("failed to kill pid %d", pid)
	}
	return nil
}

// PortInUse reports whether port is already bound on this host. It is a
// pure-Go probe (no "ss"/"lsof" dependency, which is not available on every
// platform — notably macOS lacks "ss") that attempts a real TCP listen and
// immediately releases it on success. A server may bind only to the
// wildcard address or only to loopback, so both are probed to catch either
// case; if either bind fails, the port is considered in use.
func PortInUse(port int) (bool, error) {
	if port <= 0 {
		return false, nil
	}
	for _, host := range []string{"", "127.0.0.1"} {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return true, nil
		}
		_ = ln.Close()
	}
	return false, nil
}
