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
		// command is manual only; do not invent defaults
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

func LoadProjectFile(dir string) (ProjectFile, error) {
	var pf ProjectFile
	path := filepath.Join(dir, ".devctl.toml")
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
	path := filepath.Join(dir, ".devctl.toml")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(pf)
}

// ProjectFilePath returns the path to a repo's .devctl.toml.
func ProjectFilePath(dir string) string {
	return filepath.Join(dir, ".devctl.toml")
}

// EnsureProjectFile creates .devctl.toml with a stub if missing.
// Returns the file path.
func EnsureProjectFile(dir, name string) (string, error) {
	path := ProjectFilePath(dir)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if name == "" {
		name = filepath.Base(dir)
	}
	content := fmt.Sprintf("name = %q\n# command = \"npm run dev\"\n# port = 3000\n", name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
