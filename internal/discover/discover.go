package discover

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mame77/devctl/internal/config"
)

type Project struct {
	Name     string
	Path     string
	Command  string
	Ports    []int
	Source   string // "config" | "scan" | "ghq"
	Runnable bool   // command set manually (config or .devctl.toml)
}

func Discover(cfg config.Config) ([]Project, error) {
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

	// one entry per repository root
	ghqPaths, ghqOK := ListGhqRepos()
	if ghqOK {
		for _, root := range ghqPaths {
			addRepoRoot(root, byPath, "ghq")
		}
	} else {
		roots := cfg.ScanRoots
		if len(roots) == 0 {
			if r, err := GhqRoot(); err == nil {
				roots = []string{r}
			}
		}
		for _, root := range roots {
			if root == "" {
				continue
			}
			if _, err := os.Stat(root); err != nil {
				continue
			}
			_ = walkGitRepos(root, cfg.ScanDepth, byPath)
		}
	}

	return orderProjects(cfg, byPath), nil
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

	name := filepath.Base(root)
	cmd := ""
	var ports []int
	runnable := false

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

	byPath[root] = Project{
		Name:     name,
		Path:     root,
		Command:  cmd,
		Ports:    ports,
		Source:   source,
		Runnable: runnable,
	}
}

func skipDir(name string) bool {
	switch name {
	case "node_modules", ".git", "vendor", "dist", "build", ".next", "target",
		"coverage", ".turbo", ".cache", "tmp", "temp", ".idea", ".vscode":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// walkGitRepos finds git repository roots under root (ghq fallback).
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
		if skipDir(d.Name()) && path != root {
			return filepath.SkipDir
		}
		if st, err := os.Stat(filepath.Join(path, ".git")); err == nil && (st.IsDir() || st.Mode().IsRegular()) {
			addRepoRoot(path, byPath, "scan")
			return filepath.SkipDir
		}
		return nil
	})
}
