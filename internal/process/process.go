package process

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

func FindListeners(port int) ([]int, error) {
	inodes, err := listenInodes(port)
	if err != nil {
		return nil, err
	}
	if len(inodes) == 0 {
		return nil, nil
	}
	return pidsByInodes(inodes)
}

func listenInodes(port int) (map[string]struct{}, error) {
	hexPort := fmt.Sprintf("%04X", port)
	inodes := map[string]struct{}{}
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		file, err := os.Open(f)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Scan() // skip header
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			local := fields[1]
			state := fields[3]
			if state != "0A" {
				continue
			}
			src := strings.Split(local, ":")
			if len(src) != 2 {
				continue
			}
			if src[1] != hexPort {
				continue
			}
			if len(fields) >= 10 {
				inodes[fields[9]] = struct{}{}
			}
		}
		file.Close()
	}
	return inodes, nil
}

func pidsByInodes(inodes map[string]struct{}) ([]int, error) {
	var pids []int
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join("/proc", entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		found := false
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			for ino := range inodes {
				if strings.Contains(link, "socket:["+ino+"]") {
					pids = append(pids, pid)
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	return pids, nil
}

func KillPIDs(pids []int) error {
	mine := os.Getpid()
	var firstErr error
	for _, pid := range pids {
		if pid <= 0 || pid == 1 || pid == mine {
			continue
		}
		p, err := os.FindProcess(pid)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_ = p.Signal(syscall.SIGTERM)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		allDead := true
		for _, pid := range pids {
			if Alive(pid) {
				allDead = false
				break
			}
		}
		if allDead {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, pid := range pids {
		if pid <= 0 || pid == 1 || pid == mine {
			continue
		}
		p, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		_ = p.Signal(syscall.SIGKILL)
	}
	time.Sleep(100 * time.Millisecond)
	for _, pid := range pids {
		if Alive(pid) {
			return fmt.Errorf("failed to kill pid %d", pid)
		}
	}
	return firstErr
}
