package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/mame77/devctl/internal/discover"
)

func EnsureCacheDir() (string, error) {
	dir, err := EnsureDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	return cacheDir, nil
}

func discoveredCachePath() (string, error) {
	dir, err := EnsureCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "discovered.json"), nil
}

func HasDiscoveredProjects() (bool, error) {
	path, err := discoveredCachePath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func LoadDiscoveredProjects() ([]discover.Project, error) {
	path, err := discoveredCachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var projects []discover.Project
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

func SaveDiscoveredProjects(projects []discover.Project) error {
	path, err := discoveredCachePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
