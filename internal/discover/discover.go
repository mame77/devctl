package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mame77/devctl/internal/config"
)

type Project struct {
	Name    string
	Path    string
	Command string
	Port    int
	Source  string // "config" | "scan"
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
			Name:    name,
			Path:    path,
			Command: cmd,
			Port:    p.Port,
			Source:  "config",
		}
	}

	// scan
	for _, root := range cfg.ScanRoots {
		if root == "" {
			continue
		}
		if _, err := os.Stat(root); err != nil {
			continue
		}
		_ = walk(root, cfg.ScanDepth, cfg.ScanMarkers, cfg.DefaultCommand, byPath)
	}

	out := make([]Project, 0, len(byPath))
	var scanOnes []Project
	for _, p := range byPath {
		if p.Source != "config" {
			scanOnes = append(scanOnes, p)
		}
	}
	// preserve config order from cfg.Projects
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
	for _, p := range scanOnes {
		if !seen[p.Path] {
			out = append(out, p)
			seen[p.Path] = true
		}
	}
	return out, nil
}

func walk(root string, maxDepth int, markers []string, defaultCmd string, byPath map[string]Project) error {
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
		// skip common heavy dirs
		base := d.Name()
		if base == "node_modules" || base == ".git" || base == "vendor" || base == "dist" || base == "build" {
			return filepath.SkipDir
		}

		for _, m := range markers {
			markerPath := filepath.Join(path, m)
			if st, err := os.Stat(markerPath); err == nil && !st.IsDir() {
				abs, _ := filepath.Abs(path)
				if _, exists := byPath[abs]; exists {
					// merge project file overrides onto existing if from scan only handled below
					return nil
				}
				pf, err := config.LoadProjectFile(path)
				name := filepath.Base(path)
				cmd := defaultCmd
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
					Name:    name,
					Path:    abs,
					Command: cmd,
					Port:    port,
					Source:  "scan",
				}
				return filepath.SkipDir // don't nest projects
			}
		}
		return nil
	})
}
