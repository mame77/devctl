package jump

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SessionName returns the tmux session name for a path (directory basename).
func SessionName(path string) string {
	return filepath.Base(filepath.Clean(path))
}

// To opens or switches to a tmux session rooted at path.
// Mirrors the Ctrl+G projects-fzf behavior in bashrc.
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

	if inTmux {
		current, err := tmuxOutput("display-message", "-p", "#S")
		if err == nil && current == name {
			// already in this session — nothing to do
			return nil
		}
	}

	exists := tmuxHasSession(name)
	if exists {
		if inTmux {
			return runTmux("switch-client", "-t", "="+name)
		}
		return runTmux("attach-session", "-t", "="+name)
	}

	if inTmux {
		if err := runTmux("new-session", "-ds", name, "-c", path); err != nil {
			return err
		}
		return runTmux("switch-client", "-t", "="+name)
	}
	return runTmux("new-session", "-s", name, "-c", path)
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
