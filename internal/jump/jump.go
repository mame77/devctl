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

// generic leaf dirs that collide across monorepos
var genericLeaf = map[string]bool{
	"app": true, "web": true, "api": true, "frontend": true, "backend": true,
	"server": true, "client": true, "packages": true, "src": true, "apps": true,
}

// SessionName returns a unique, tmux-safe session name for path.
// Monorepo leaves like .../jal-eap/app become "jal-eap-app", not "app".
func SessionName(path string) string {
	path = filepath.Clean(path)
	base := filepath.Base(path)
	parent := filepath.Base(filepath.Dir(path))
	name := base
	if genericLeaf[base] && parent != "" && parent != "." && parent != string(filepath.Separator) {
		name = parent + "-" + base
	}
	return sanitizeSessionName(name)
}

func sanitizeSessionName(name string) string {
	// tmux rejects '.', ':', and '/' in session names
	replacer := strings.NewReplacer(
		"/", "-",
		":", "-",
		".", "-",
		" ", "-",
	)
	name = replacer.Replace(name)
	name = strings.Trim(name, "-")
	if name == "" {
		return "devctl"
	}
	return name
}

// uniqueSessionName ensures the name is free or already points at path.
func uniqueSessionName(path string) string {
	path = filepath.Clean(path)
	name := SessionName(path)
	if canUseSession(name, path) {
		return name
	}
	// collision: same basename, different repo — use owner-repo
	parts := strings.Split(filepath.ToSlash(path), "/")
	for len(parts) > 0 && parts[0] == "" {
		parts = parts[1:]
	}
	if n := len(parts); n >= 2 {
		cand := sanitizeSessionName(parts[n-2] + "-" + parts[n-1])
		if cand != name && canUseSession(cand, path) {
			return cand
		}
	}
	for i := 2; i < 100; i++ {
		cand := fmt.Sprintf("%s-%d", name, i)
		if canUseSession(cand, path) {
			return cand
		}
	}
	return name
}

// canUseSession is true if name is free, or already targets path.
func canUseSession(name, path string) bool {
	if !tmuxHasSession(name) {
		return true
	}
	sp := sessionPath(name)
	// unknown path: reuse name (matches bashrc basename behavior)
	if sp == "" {
		return true
	}
	return samePath(sp, path)
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return aa == bb
}

func sessionPath(name string) string {
	// prefer session_path; fall back to first pane cwd
	out, err := tmuxOutput("display-message", "-t", "="+name, "-p", "#{session_path}")
	if err == nil {
		out = strings.TrimSpace(out)
		if out != "" {
			return filepath.Clean(out)
		}
	}
	out, err = tmuxOutput("list-panes", "-t", "="+name, "-F", "#{pane_current_path}")
	if err != nil {
		return ""
	}
	// first line only
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	return filepath.Clean(out)
}

func pendingPath() (string, error) {
	dir, err := config.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "jump-target"), nil
}

// SetPending records session name + path for post-popup switch.
func SetPending(session, dir string) error {
	path, err := pendingPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// line1: session name, line2: path (optional, for recreate)
	content := session + "\n" + filepath.Clean(dir) + "\n"
	return os.WriteFile(path, []byte(content), 0o600)
}

// ConsumePending reads and clears the pending jump target.
// Returns session name, path, error. Empty session means none.
func ConsumePending() (session, dir string, err error) {
	path, err := pendingPath()
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", err
	}
	_ = os.Remove(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return "", "", nil
	}
	session = strings.TrimSpace(lines[0])
	if len(lines) > 1 {
		dir = strings.TrimSpace(lines[1])
	}
	return session, dir, nil
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

// PrintPath writes a validated directory path for shell wrappers such as:
//
//	cd "$(devctl jump)"
func PrintPath(path string) error {
	return WritePath(path, "")
}

// WritePath writes a validated directory path to cwdFile, or stdout if cwdFile is empty.
func WritePath(path, cwdFile string) error {
	path = filepath.Clean(path)
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}
	if cwdFile != "" {
		if err := os.MkdirAll(filepath.Dir(cwdFile), 0o755); err != nil {
			return err
		}
		return os.WriteFile(cwdFile, []byte(path+"\n"), 0o600)
	}
	_, err = fmt.Fprintln(os.Stdout, path)
	return err
}

// ToTmux opens or switches to a tmux session rooted at path.
// Inside a tmux popup, records a pending switch so the popup wrapper can
// apply it after the popup closes (switch-client alone is undone on close).
func ToTmux(path string) error {
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

	name := uniqueSessionName(path)
	inTmux := os.Getenv("TMUX") != ""
	inPopup := InPopup()

	if inPopup {
		if err := SetPending(name, path); err != nil {
			return err
		}
	} else {
		ClearPending()
	}

	if inTmux && !inPopup {
		current, err := tmuxOutput("display-message", "-p", "#S")
		if err == nil && current == name && sessionPath(name) == path {
			return nil
		}
	}

	if !tmuxHasSession(name) {
		if err := runTmux("new-session", "-ds", name, "-c", path); err != nil {
			return fmt.Errorf("tmux new-session %q: %w", name, err)
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
// Missing pending is a no-op (exit 0) so quitting the TUI without jump is fine.
func ApplyPending() error {
	name, dir, err := ConsumePending()
	if err != nil {
		return err
	}
	if name == "" {
		return nil
	}
	if !tmuxHasSession(name) {
		// recreate if we still know the path
		if dir != "" {
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				if err := runTmux("new-session", "-ds", name, "-c", dir); err != nil {
					return nil // don't fail popup close
				}
			} else {
				return nil
			}
		} else {
			return nil
		}
	}
	// switch-client errors should not break run-shell loudly when already on target
	_ = runTmux("switch-client", "-t", "="+name)
	return nil
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
