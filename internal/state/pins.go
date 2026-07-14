package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mame77/devctl/internal/discover"
)

func pinsPath() (string, error) {
	dir, err := EnsureDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pins.json"), nil
}

func LoadPins() ([]string, error) {
	path, err := pinsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return nil, err
	}
	return paths, nil
}

func SavePins(paths []string) error {
	p, err := pinsPath()
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		_ = os.Remove(p)
		return nil
	}
	data, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func IsPinned(abs string) (bool, error) {
	pins, err := LoadPins()
	if err != nil {
		return false, err
	}
	return discover.MatchPathKey(abs, pins), nil
}

func TogglePin(abs string) (bool, error) {
	abs = filepath.Clean(abs)
	pins, err := LoadPins()
	if err != nil {
		return false, err
	}
	key := discover.PathKey(abs)
	found := -1
	for i, p := range pins {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == key {
			found = i
			break
		}
	}
	if found >= 0 {
		pins = append(pins[:found], pins[found+1:]...)
		if err := SavePins(pins); err != nil {
			return false, err
		}
		return false, nil
	}
	pins = append(pins, key)
	if err := SavePins(pins); err != nil {
		return false, err
	}
	return true, nil
}
