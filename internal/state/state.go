package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/mame77/devctl/internal/config"
)

type Entry struct {
	Name       string     `json:"name"`
	PID        int        `json:"pid"`
	PGID       int        `json:"pgid"`
	Port       int        `json:"port"` // legacy first port
	Ports      []int      `json:"ports,omitempty"`
	Cwd        string     `json:"cwd"`
	Command    string     `json:"command"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	LogPath    string     `json:"log_path"`
}

// DoneTTL is how long a finished process stays visible as "Done".
const DoneTTL = 3 * time.Second

func Dir() (string, error) {
	return config.StateDir()
}

func EnsureDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func pathFor(name string) (string, error) {
	dir, err := EnsureDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
}

func LogPath(name string) (string, error) {
	dir, err := EnsureDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".log"), nil
}

func Save(e Entry) error {
	p, err := pathFor(e.Name)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func Load(name string) (Entry, error) {
	var e Entry
	p, err := pathFor(name)
	if err != nil {
		return e, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return e, err
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return e, err
	}
	return e, nil
}

func Remove(name string) error {
	p, err := pathFor(name)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func List() ([]Entry, error) {
	dir, err := EnsureDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, ent := range entries {
		if ent.IsDir() || filepath.Ext(ent.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
