package discover

import (
	"bytes"
	"encoding/json"
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
	Port     int
	Source   string // "config" | "scan" | "ghq"
	Runnable bool   // has a startable dev command
}

type packageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

func Discover(cfg config.Config) ([]Project, error) {
	byPath := map[string]Project{}

	// explicit projects first
	for _, p := range cfg.Projects {
		path, err := filepath.Abs(p.Path)
		if err != nil {
			path = p.Path
		}
		name := p.Name
		if name == "" {
			name = filepath.Base(path)
		}
		cmd := p.Command
		if cmd == "" {
			cmd = cfg.DefaultCommand
		}
		byPath[path] = Project{
			Name:     name,
			Path:     path,
			Command:  cmd,
			Port:     p.Port,
			Source:   "config",
			Runnable: cmd != "",
		}
	}

	// prefer ghq list when available
	ghqPaths, ghqOK := ListGhqRepos()
	if ghqOK {
		for _, root := range ghqPaths {
			scanRepo(root, cfg, byPath, "ghq")
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
			_ = walk(root, cfg.ScanDepth, cfg, byPath)
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
	// disambiguate duplicate names among scan results
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
	// owner/repo or repo/subdir
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
	// ghq root can return multiple roots (newline-separated); use first
	if i := strings.IndexByte(root, '\n'); i >= 0 {
		root = strings.TrimSpace(root[:i])
	}
	return root, nil
}

// scanRepo looks for projects inside a single git repository.
// Always registers the repo root (for jump). Runnable if .devctl.toml or package.json "dev".
func scanRepo(root string, cfg config.Config, byPath map[string]Project, source string) {
	root, err := filepath.Abs(root)
	if err != nil {
		return
	}
	base := filepath.Base(root)

	// project-local config always wins for this tree
	if tryAddDevctl(root, cfg, byPath, source) {
		return
	}
	if tryAddPackageJSON(root, cfg, byPath, source, base) {
		return
	}
	// jump target even without a dev command (e.g. nix-config)
	addJumpOnly(root, source, base, byPath)

	// monorepo: scan immediate subdirs (and one more level for apps/* style)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if skipDir(name) {
			continue
		}
		sub := filepath.Join(root, name)
		if tryAddDevctl(sub, cfg, byPath, source) {
			continue
		}
		label := base + "/" + name
		if tryAddPackageJSON(sub, cfg, byPath, source, label) {
			continue
		}
		// one more level (e.g. packages/web)
		subs, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, e2 := range subs {
			if !e2.IsDir() || skipDir(e2.Name()) {
				continue
			}
			sub2 := filepath.Join(sub, e2.Name())
			if tryAddDevctl(sub2, cfg, byPath, source) {
				continue
			}
			label2 := base + "/" + name + "/" + e2.Name()
			_ = tryAddPackageJSON(sub2, cfg, byPath, source, label2)
		}
	}
}

func addJumpOnly(dir, source, name string, byPath map[string]Project) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if _, exists := byPath[abs]; exists {
		return
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	byPath[abs] = Project{
		Name:     name,
		Path:     abs,
		Command:  "",
		Port:     0,
		Source:   source,
		Runnable: false,
	}
}

func tryAddDevctl(dir string, cfg config.Config, byPath map[string]Project, source string) bool {
	marker := filepath.Join(dir, ".devctl.toml")
	st, err := os.Stat(marker)
	if err != nil || st.IsDir() {
		return false
	}
	abs, _ := filepath.Abs(dir)
	if _, exists := byPath[abs]; exists {
		return true
	}
	pf, err := config.LoadProjectFile(dir)
	name := filepath.Base(dir)
	cmd := cfg.DefaultCommand
	port := 0
	if err == nil {
		if pf.Name != "" {
			name = pf.Name
		}
		if pf.Command != "" {
			cmd = pf.Command
		}
		port = pf.Port
		if pf.Workdir != "" && pf.Workdir != "." {
			abs = filepath.Join(abs, pf.Workdir)
		}
	}
	byPath[abs] = Project{
		Name:     name,
		Path:     abs,
		Command:  cmd,
		Port:     port,
		Source:   source,
		Runnable: true,
	}
	return true
}

func tryAddPackageJSON(dir string, cfg config.Config, byPath map[string]Project, source, name string) bool {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	if pkg.Scripts == nil {
		return false
	}
	if _, ok := pkg.Scripts["dev"]; !ok {
		return false
	}
	abs, _ := filepath.Abs(dir)
	if existing, exists := byPath[abs]; exists && existing.Runnable {
		return true
	}
	if name == "" {
		name = filepath.Base(dir)
	}
	cmd := inferDevCommand(dir, cfg.DefaultCommand)
	byPath[abs] = Project{
		Name:     name,
		Path:     abs,
		Command:  cmd,
		Port:     0,
		Source:   source,
		Runnable: true,
	}
	return true
}

func inferDevCommand(dir, defaultCmd string) string {
	checks := []struct {
		file string
		cmd  string
	}{
		{"bun.lockb", "bun run dev"},
		{"bun.lock", "bun run dev"},
		{"pnpm-lock.yaml", "pnpm run dev"},
		{"yarn.lock", "yarn dev"},
		{"package-lock.json", "npm run dev"},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(dir, c.file)); err == nil {
			return c.cmd
		}
	}
	if defaultCmd != "" {
		return defaultCmd
	}
	return "npm run dev"
}

func skipDir(name string) bool {
	switch name {
	case "node_modules", ".git", "vendor", "dist", "build", ".next", "target",
		"coverage", ".turbo", ".cache", "tmp", "temp", ".idea", ".vscode":
		return true
	}
	return strings.HasPrefix(name, ".")
}

func walk(root string, maxDepth int, cfg config.Config, byPath map[string]Project) error {
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

		// .devctl.toml always counts
		if tryAddDevctl(path, cfg, byPath, "scan") {
			return filepath.SkipDir
		}
		// package.json with dev script
		if tryAddPackageJSON(path, cfg, byPath, "scan", projectNameFromPath(path, root)) {
			return filepath.SkipDir
		}
		return nil
	})
}

func projectNameFromPath(path, root string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return filepath.Base(path)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	// ghq layout: host/owner/repo[/sub...]
	if len(parts) >= 3 {
		// repo or repo/sub
		return strings.Join(parts[2:], "/")
	}
	if len(parts) >= 1 {
		return parts[len(parts)-1]
	}
	return filepath.Base(path)
}
