package process

import (
	"fmt"
	"os"
	"os/exec"
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

func PortInUse(port int) (bool, error) {
	if port <= 0 {
		return false, nil
	}
	out, err := exec.Command("ss", "-ltn").Output()
	if err != nil {
		return false, nil
	}
	return containsPort(string(out), port), nil
}

func containsPort(ssOut string, port int) bool {
	target := fmt.Sprintf(":%d", port)
	for i := 0; i+len(target) <= len(ssOut); i++ {
		if ssOut[i:i+len(target)] != target {
			continue
		}
		end := i + len(target)
		if end == len(ssOut) {
			return true
		}
		c := ssOut[end]
		if c < '0' || c > '9' {
			return true
		}
	}
	return false
}
