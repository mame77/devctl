package discover

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mame77/devctl/internal/config"
)

type Project struct {
	Name     string
	Path     string
	Command  string
	Ports    []int
	Source   string // "config" | "scan"
	Runnable bool   // command set manually (config or .devctl.toml)
	Pinned   bool   // pinned via TUI toggle
}

func Discover(cfg config.Config) ([]Project, error) {
	scanned, err := Scan(cfg)
	if err != nil {
		return nil, err
	}
	return Merge(cfg, scanned), nil
}

func Scan(cfg config.Config) ([]Project, error) {
	byPath := map[string]Project{}
	roots := cfg.ScanRoots
	if len(roots) == 0 {
		roots = config.DefaultScanRoots()
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		root = config.ExpandPath(root)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		_ = walkGitRepos(root, cfg.ScanDepth, byPath)
	}
	var out []Project
	for _, p := range byPath {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
	})
	return out, nil
}

func Merge(cfg config.Config, scanned []Project) []Project {
	byPath := map[string]Project{}

	// explicit projects first (command must be set manually)
	for _, p := range cfg.Projects {
		path, err := filepath.Abs(p.Path)
		if err != nil {
			path = p.Path
		}
		name := p.Name
		if name == "" {
			name = filepath.Base(path)
		}
		byPath[path] = Project{
			Name:     name,
			Path:     path,
			Command:  p.Command,
			Ports:    p.AllPorts(),
			Source:   "config",
			Runnable: p.Command != "",
		}
	}
	for _, p := range scanned {
		if existing, ok := byPath[p.Path]; ok && existing.Source == "config" {
			continue
		}
		byPath[p.Path] = p
	}
	// apply temporary ignore (explicit config projects are kept)
	for path, p := range byPath {
		if p.Source != "config" && isIgnored(path, cfg.Ignore) {
			delete(byPath, path)
		}
	}
	return orderProjects(cfg, byPath)
}

func orderProjects(cfg config.Config, byPath map[string]Project) []Project {
	out := make([]Project, 0, len(byPath))
	var scanOnes []Project
	for _, p := range byPath {
		if p.Source != "config" {
			scanOnes = append(scanOnes, p)
		}
	}
	seen := map[string]bool{}
	for _, p := range cfg.Projects {
		path, err := filepath.Abs(p.Path)
		if err != nil {
			path = p.Path
		}
		if proj, ok := byPath[path]; ok && !seen[path] {
			out = append(out, proj)
			seen[path] = true
		}
	}
	sort.Slice(scanOnes, func(i, j int) bool {
		return strings.ToLower(scanOnes[i].Name) < strings.ToLower(scanOnes[j].Name)
	})
	nameCount := map[string]int{}
	for _, p := range scanOnes {
		nameCount[p.Name]++
	}
	for _, p := range scanOnes {
		if seen[p.Path] {
			continue
		}
		if nameCount[p.Name] > 1 {
			p.Name = uniqueName(p.Path)
		}
		out = append(out, p)
		seen[p.Path] = true
	}
	return out
}

func uniqueName(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if n := len(parts); n >= 2 {
		return parts[n-2] + "/" + parts[n-1]
	}
	return filepath.Base(path)
}

// ListGhqRepos returns absolute paths from `ghq list --full-path`.
func ListGhqRepos() ([]string, bool) {
	cmd := exec.Command("ghq", "list", "--full-path")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return nil, false
	}
	var paths []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if st, err := os.Stat(line); err == nil && st.IsDir() {
			paths = append(paths, line)
		}
	}
	return paths, len(paths) > 0
}

// GhqRoot returns the first root from `ghq root`.
func GhqRoot() (string, error) {
	cmd := exec.Command("ghq", "root")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	root := strings.TrimSpace(stdout.String())
	if root == "" {
		return "", os.ErrNotExist
	}
	if i := strings.IndexByte(root, '\n'); i >= 0 {
		root = strings.TrimSpace(root[:i])
	}
	return root, nil
}

// addRepoRoot registers a repository root. Runnable only if .devctl.toml sets command.
func addRepoRoot(root string, byPath map[string]Project, source string) {
	root, err := filepath.Abs(root)
	if err != nil {
		return
	}
	if _, exists := byPath[root]; exists {
		return
	}

	name, cmd, ports, runnable := resolveProjectFile(root)

	byPath[root] = Project{
		Name:     name,
		Path:     root,
		Command:  cmd,
		Ports:    ports,
		Source:   source,
		Runnable: runnable,
	}
}

// resolveProjectFile reads the per-repo config file (<repo>/.devctl.toml or
// ~/.config/devctl/projects/<slug>.toml) for root and returns the settings
// that should apply. If no config file exists, it returns the "unconfigured"
// defaults: no command, no ports, not runnable, name defaulting to the repo
// directory's basename.
func resolveProjectFile(root string) (name, cmd string, ports []int, runnable bool) {
	name = filepath.Base(root)
	if pf, err := config.LoadProjectFile(root); err == nil {
		if pf.Name != "" {
			name = pf.Name
		}
		if pf.Command != "" {
			cmd = pf.Command
			runnable = true
		}
		ports = pf.AllPorts()
	}
	return name, cmd, ports, runnable
}

// Refresh re-resolves name/command/ports/runnable for scan-derived projects
// from their per-repo config files. The discovered-projects cache only
// stores the result of the expensive directory walk (repo paths); settings
// are "config", not "scan" data, so they must be re-read on every load. This
// keeps edits to .devctl.toml (via the `e` key or an external editor) in
// effect on the next reload without forcing a full Rescan.
func Refresh(scanned []Project) []Project {
	out := make([]Project, len(scanned))
	for i, p := range scanned {
		name, cmd, ports, runnable := resolveProjectFile(p.Path)
		p.Name = name
		p.Command = cmd
		p.Ports = ports
		p.Runnable = runnable
		out[i] = p
	}
	return out
}

func skipDir(root, name string) bool {
	switch name {
	case "node_modules", ".git", "vendor", "dist", "build", ".next", "target",
		"coverage", ".turbo", ".cache", "tmp", "temp", ".idea", ".vscode":
		return true
	}
	if isHomeRoot(root) {
		switch runtime.GOOS {
		case "darwin":
			switch name {
			case "Library", "Applications", "Desktop", "Documents", "Downloads",
				"Movies", "Music", "Pictures", "Public":
				return true
			}
		case "linux":
			switch name {
			case ".cache", ".local", ".var", "snap":
				return true
			}
		case "windows":
			switch name {
			case "AppData", "Application Data", "Local Settings", "OneDrive":
				return true
			}
		}
	}
	return strings.HasPrefix(name, ".")
}

// walkGitRepos finds git repository roots under root by looking for .git.
func walkGitRepos(root string, maxDepth int, byPath map[string]Project) error {
	root = filepath.Clean(root)
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(os.PathSeparator)) + 1
		}
		if depth > maxDepth {
			return filepath.SkipDir
		}
		if skipDir(root, d.Name()) && path != root {
			return filepath.SkipDir
		}
		if st, err := os.Stat(filepath.Join(path, ".git")); err == nil && (st.IsDir() || st.Mode().IsRegular()) {
			addRepoRoot(path, byPath, "scan")
			return filepath.SkipDir
		}
		return nil
	})
}

func isHomeRoot(root string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return filepath.Clean(root) == filepath.Clean(home)
}

// isIgnored reports whether absPath matches any ignore entry.
// Entries may be absolute paths or ghq/home-relative paths (e.g.
// "github.com/digeon-inc"). Relative entries are matched against the
// repo's path relative to a known git root or the home directory.
func isIgnored(abs string, ignores []string) bool {
	key := ignoreKey(abs)
	absClean := filepath.ToSlash(filepath.Clean(abs))
	for _, ig := range ignores {
		ig = strings.TrimSpace(ig)
		if ig == "" {
			continue
		}
		igClean := filepath.ToSlash(filepath.Clean(config.ExpandPath(ig)))
		if filepath.IsAbs(igClean) {
			if absClean == igClean || strings.HasPrefix(absClean, igClean+"/") {
				return true
			}
			continue
		}
		if key == igClean || strings.HasPrefix(key, igClean+"/") {
			return true
		}
	}
	return false
}

// ignoreKey (unexported alias for PathKey).
func ignoreKey(abs string) string { return PathKey(abs) }

// PathKey returns a repo path relative to a known git root, then home.
func PathKey(abs string) string {
	abs = filepath.Clean(abs)
	for _, r := range ignoreRoots() {
		r = filepath.Clean(config.ExpandPath(r))
		if r == "" {
			continue
		}
		if rel, err := filepath.Rel(r, abs); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(abs)
}

// MatchPathKey reports whether abs matches any entry in entries.
// Matching uses the same path-key logic as ignore/pin.
func MatchPathKey(abs string, entries []string) bool {
	key := PathKey(abs)
	absClean := filepath.ToSlash(filepath.Clean(abs))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		eClean := filepath.ToSlash(filepath.Clean(config.ExpandPath(e)))
		if filepath.IsAbs(eClean) {
			if absClean == eClean || strings.HasPrefix(absClean, eClean+"/") {
				return true
			}
			continue
		}
		if key == eClean || strings.HasPrefix(key, eClean+"/") {
			return true
		}
	}
	return false
}

// ApplyPins marks projects matching pins as Pinned=true and re-sorts:
// pinned first (in pins order), then the rest maintaining existing order.
func ApplyPins(projects []Project, pins []string) []Project {
	if len(pins) == 0 {
		for i := range projects {
			projects[i].Pinned = false
		}
		return projects
	}
	pinnedOrder := make(map[string]int) // pathKey → position in pins
	for i, p := range pins {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := filepath.ToSlash(filepath.Clean(p))
		if _, ok := pinnedOrder[key]; !ok {
			pinnedOrder[key] = i
		}
	}
	var pinned, rest []Project
	for i := range projects {
		key := PathKey(projects[i].Path)
		if _, ok := pinnedOrder[key]; ok {
			projects[i].Pinned = true
			pinned = append(pinned, projects[i])
		} else {
			projects[i].Pinned = false
			rest = append(rest, projects[i])
		}
	}
	sort.Slice(pinned, func(i, j int) bool {
		ki := PathKey(pinned[i].Path)
		kj := PathKey(pinned[j].Path)
		return pinnedOrder[ki] < pinnedOrder[kj]
	})
	out := make([]Project, 0, len(projects))
	out = append(out, pinned...)
	out = append(out, rest...)
	return out
}

func ignoreRoots() []string {
	var roots []string
	if r := os.Getenv("GHQ_ROOT"); r != "" {
		for _, p := range strings.Split(r, string(os.PathListSeparator)) {
			if p != "" {
				roots = append(roots, p)
			}
		}
	}
	roots = append(roots, "~/ghq", "~/src", "~/Projects")
	return roots
}
