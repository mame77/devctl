package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Project struct {
	Name    string `toml:"name"`
	Path    string `toml:"path"`
	Command string `toml:"command"`
	Port    int    `toml:"port"`
	Workdir string `toml:"workdir"`
}

type Config struct {
	DefaultCommand string    `toml:"default_command"`
	ScanRoots      []string  `toml:"scan_roots"`
	ScanDepth      int       `toml:"scan_depth"`
	ScanMarkers    []string  `toml:"scan_markers"`
	Projects       []Project `toml:"projects"`
}

type ProjectFile struct {
	Name    string `toml:"name"`
	Command string `toml:"command"`
	Port    int    `toml:"port"`
	Workdir string `toml:"workdir"`
}

func Default() Config {
	return Config{
		DefaultCommand: "",
		ScanRoots:      []string{"~/ghq"},
		ScanDepth:      6,
		ScanMarkers:    []string{".devctl.toml"},
		Projects:       nil,
	}
}

func ConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "devctl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "devctl"), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// ProjectsDir is ~/.config/devctl/projects/ (per-repo configs).
func ProjectsDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "projects"), nil
}

func StateDir() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "devctl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "devctl"), nil
}

func ExpandPath(p string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func Load() (Config, error) {
	cfg := Default()
	path, err := ConfigPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.ScanDepth <= 0 {
		cfg.ScanDepth = 6
	}
	if len(cfg.ScanMarkers) == 0 {
		cfg.ScanMarkers = []string{".devctl.toml"}
	}
	for i := range cfg.Projects {
		cfg.Projects[i].Path = ExpandPath(cfg.Projects[i].Path)
		if cfg.Projects[i].Command == "" && cfg.DefaultCommand != "" {
			cfg.Projects[i].Command = cfg.DefaultCommand
		}
		if cfg.Projects[i].Name == "" {
			cfg.Projects[i].Name = filepath.Base(cfg.Projects[i].Path)
		}
	}
	for i := range cfg.ScanRoots {
		cfg.ScanRoots[i] = ExpandPath(cfg.ScanRoots[i])
	}
	return cfg, nil
}

// RepoLocalPath is <repo>/.devctl.toml
func RepoLocalPath(dir string) string {
	return filepath.Join(dir, ".devctl.toml")
}

// GlobalProjectPath is ~/.config/devctl/projects/<slug>.toml
// slug mirrors ghq layout when possible: github.com/owner/repo.toml
func GlobalProjectPath(dir string) (string, error) {
	base, err := ProjectsDir()
	if err != nil {
		return "", err
	}
	slug, err := projectSlug(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, slug+".toml"), nil
}

func projectSlug(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	abs = filepath.Clean(abs)

	// prefer path relative to ghq root(s)
	for _, root := range ghqRootsForSlug() {
		root = filepath.Clean(ExpandPath(root))
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(root, abs)
		if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel), nil
		}
	}

	// fallback: strip home prefix
	if home, err := os.UserHomeDir(); err == nil {
		rel, err := filepath.Rel(home, abs)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel), nil
		}
	}
	// last resort: basename only (may collide)
	return filepath.Base(abs), nil
}

func ghqRootsForSlug() []string {
	var roots []string
	// env / common
	if r := os.Getenv("GHQ_ROOT"); r != "" {
		for _, p := range strings.Split(r, string(os.PathListSeparator)) {
			if p != "" {
				roots = append(roots, p)
			}
		}
	}
	roots = append(roots, "~/ghq", "~/src", "~/Projects")
	// also scan_roots from default
	return roots
}

// ResolveProjectFilePath returns which config file applies (repo wins if both exist).
// ok is false if neither exists.
func ResolveProjectFilePath(dir string) (path string, ok bool) {
	local := RepoLocalPath(dir)
	if st, err := os.Stat(local); err == nil && !st.IsDir() {
		return local, true
	}
	global, err := GlobalProjectPath(dir)
	if err != nil {
		return "", false
	}
	if st, err := os.Stat(global); err == nil && !st.IsDir() {
		return global, true
	}
	return "", false
}

// LoadProjectFile loads project settings.
// Priority: <repo>/.devctl.toml > ~/.config/devctl/projects/<slug>.toml
func LoadProjectFile(dir string) (ProjectFile, error) {
	var pf ProjectFile
	path, ok := ResolveProjectFilePath(dir)
	if !ok {
		return pf, os.ErrNotExist
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return pf, err
	}
	if _, err := toml.Decode(string(data), &pf); err != nil {
		return pf, err
	}
	return pf, nil
}

func WriteProjectFile(dir string, pf ProjectFile) error {
	path := RepoLocalPath(dir)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(pf)
}

// ProjectFilePath is the preferred path for editing (always XDG global).
func ProjectFilePath(dir string) string {
	global, err := GlobalProjectPath(dir)
	if err != nil {
		return RepoLocalPath(dir)
	}
	return global
}

// EnsureProjectFile opens/creates the project config for editing under
// ~/.config/devctl/projects/ (never writes into the repo on `e`).
// Runtime load still prefers <repo>/.devctl.toml if present.
func EnsureProjectFile(dir, name string) (string, error) {
	path, err := GlobalProjectPath(dir)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		return path, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if name == "" {
		name = filepath.Base(dir)
	}
	content := fmt.Sprintf(
		"# repo: %s\nname = %q\n# command = \"npm run dev\"\n# port = 3000\n",
		filepath.Clean(dir),
		name,
	)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
