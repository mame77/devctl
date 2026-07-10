package jump

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mame77/devctl/internal/discover"
)

// Entry is a selectable destination for fzf jump.
type Entry struct {
	Label string // shown in fzf (relative / short)
	Path  string // absolute path
}

// ListCandidates returns ghq repos + git-managed ~/.config/* dirs.
func ListCandidates() ([]Entry, error) {
	var entries []Entry
	seen := map[string]bool{}

	root, err := discover.GhqRoot()
	if err != nil {
		root = ""
	}

	if paths, ok := discover.ListGhqRepos(); ok {
		for _, p := range paths {
			label := p
			if root != "" {
				if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
					label = rel
				}
			}
			if !seen[p] {
				entries = append(entries, Entry{Label: label, Path: p})
				seen[p] = true
			}
		}
	}

	// ~/.config/* that are git repos (same as bashrc projects-fzf)
	home, err := os.UserHomeDir()
	if err == nil {
		configDir := filepath.Join(home, ".config")
		dirs, _ := os.ReadDir(configDir)
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			full := filepath.Join(configDir, d.Name())
			if st, err := os.Stat(filepath.Join(full, ".git")); err == nil && st.IsDir() {
				label := filepath.Join(".config", d.Name())
				if !seen[full] {
					entries = append(entries, Entry{Label: label, Path: full})
					seen[full] = true
				}
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Label) < strings.ToLower(entries[j].Label)
	})
	return entries, nil
}

// Pick runs fzf and returns the selected absolute path.
// Returns ("", nil) if the user cancelled.
func Pick() (string, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return "", fmt.Errorf("fzf not found in PATH")
	}

	entries, err := ListCandidates()
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("no repositories found (install ghq or clone repos)")
	}

	byLabel := map[string]string{}
	var lines []string
	for _, e := range entries {
		byLabel[e.Label] = e.Path
		lines = append(lines, e.Label)
	}

	cmd := exec.Command("fzf", "--height", "40%", "--reverse", "--prompt", "devctl jump> ")
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	cmd.Stderr = os.Stderr
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		// fzf exits 130 / 1 on cancel
		if exitErr, ok := err.(*exec.ExitError); ok {
			if code := exitErr.ExitCode(); code == 1 || code == 130 {
				return "", nil
			}
		}
		return "", err
	}

	selected := strings.TrimSpace(stdout.String())
	if selected == "" {
		return "", nil
	}
	if path, ok := byLabel[selected]; ok {
		return path, nil
	}
	// allow absolute path paste
	if st, err := os.Stat(selected); err == nil && st.IsDir() {
		return selected, nil
	}
	return "", fmt.Errorf("unknown selection: %s", selected)
}

// Interactive picks a repo with fzf and jumps via tmux.
func Interactive() error {
	path, err := Pick()
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	return To(path)
}
