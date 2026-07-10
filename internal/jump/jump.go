package jump

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mame77/devctl/internal/config"
)

// SessionName returns the tmux session name for a path (directory basename).
func SessionName(path string) string {
	return filepath.Base(filepath.Clean(path))
}

func pendingPath() (string, error) {
	dir, err := config.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "jump-target"), nil
}

// SetPending records a session name to switch to after a tmux popup closes.
func SetPending(session string) error {
	path, err := pendingPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(session+"\n"), 0o600)
}

// ConsumePending reads and clears the pending jump target.
// Returns "" if none.
func ConsumePending() (string, error) {
	path, err := pendingPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	_ = os.Remove(path)
	return strings.TrimSpace(string(data)), nil
}

// ClearPending removes any pending jump without applying it.
func ClearPending() {
	path, err := pendingPath()
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

// InPopup reports whether we are running inside a tmux display-popup
// (set DEVCTL_POPUP=1 from the popup bind).
func InPopup() bool {
	return os.Getenv("DEVCTL_POPUP") == "1"
}

// To opens or switches to a tmux session rooted at path.
// Inside a tmux popup, records a pending switch so the popup wrapper can
// apply it after the popup closes (switch-client alone is undone on close).
func To(path string) error {
	path = filepath.Clean(path)
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}

	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not found in PATH")
	}

	name := SessionName(path)
	inTmux := os.Getenv("TMUX") != ""
	inPopup := InPopup()

	if inPopup {
		if err := SetPending(name); err != nil {
			return err
		}
	} else {
		ClearPending()
	}

	if inTmux {
		current, err := tmuxOutput("display-message", "-p", "#S")
		if err == nil && current == name && !inPopup {
			return nil
		}
	}

	if !tmuxHasSession(name) {
		if err := runTmux("new-session", "-ds", name, "-c", path); err != nil {
			return err
		}
	}

	if inPopup {
		// Parent client will switch via --apply-pending after popup exits.
		return nil
	}

	if inTmux {
		return runTmux("switch-client", "-t", "="+name)
	}
	return runTmux("attach-session", "-t", "="+name)
}

// ApplyPending switches to a previously recorded session (for after popup exit).
func ApplyPending() error {
	name, err := ConsumePending()
	if err != nil {
		return err
	}
	if name == "" {
		return nil
	}
	if !tmuxHasSession(name) {
		return fmt.Errorf("session %q not found", name)
	}
	return runTmux("switch-client", "-t", "="+name)
}

func tmuxHasSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", "="+name)
	return cmd.Run() == nil
}

func runTmux(args ...string) error {
	cmd := exec.Command("tmux", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func tmuxOutput(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
