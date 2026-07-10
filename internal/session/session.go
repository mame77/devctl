package session

import (
	"fmt"
	"os"
	"time"

	"github.com/mame77/devctl/internal/config"
	"github.com/mame77/devctl/internal/discover"
	"github.com/mame77/devctl/internal/process"
	"github.com/mame77/devctl/internal/state"
)

type Item struct {
	Name     string
	Path     string
	Command  string
	Ports    []int
	Running  bool
	Done     bool // finished recently; shows "Done" briefly
	PID      int
	PGID     int
	Source   string
	Runnable bool
}

func (it Item) PrimaryPort() int {
	if len(it.Ports) > 0 {
		return it.Ports[0]
	}
	return 0
}

type Manager struct {
	cfg config.Config
}

func New() (*Manager, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return &Manager{cfg: cfg}, nil
}

func (m *Manager) ReloadConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	m.cfg = cfg
	return nil
}

func (m *Manager) List() ([]Item, error) {
	projects, err := discover.Discover(m.cfg)
	if err != nil {
		return nil, err
	}
	entries, err := state.List()
	if err != nil {
		return nil, err
	}

	statusByName := map[string]state.Entry{}
	for _, e := range entries {
		if process.Alive(e.PID) {
			// still running — clear any stale finished marker
			if e.FinishedAt != nil {
				e.FinishedAt = nil
				_ = state.Save(e)
			}
			statusByName[e.Name] = e
			continue
		}
		// process ended
		now := time.Now()
		if e.FinishedAt == nil {
			e.FinishedAt = &now
			_ = state.Save(e)
		}
		if now.Sub(*e.FinishedAt) < state.DoneTTL {
			statusByName[e.Name] = e
		} else {
			_ = state.Remove(e.Name)
		}
	}

	items := make([]Item, 0, len(projects))
	for _, p := range projects {
		it := Item{
			Name:     p.Name,
			Path:     p.Path,
			Command:  p.Command,
			Ports:    append([]int(nil), p.Ports...),
			Source:   p.Source,
			Runnable: p.Runnable,
		}
		if e, ok := statusByName[p.Name]; ok {
			it.PID = e.PID
			it.PGID = e.PGID
			if len(it.Ports) == 0 {
				it.Ports = config.NormalizePorts(e.Ports, e.Port)
			}
			if process.Alive(e.PID) {
				it.Running = true
			} else {
				it.Done = true
			}
		}
		items = append(items, it)
	}
	return items, nil
}

func (m *Manager) Active() (*Item, error) {
	items, err := m.List()
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Running {
			return &items[i], nil
		}
	}
	entries, _ := state.List()
	for _, e := range entries {
		if process.Alive(e.PID) {
			return &Item{
				Name:    e.Name,
				Path:    e.Cwd,
				Command: e.Command,
				Ports:   config.NormalizePorts(e.Ports, e.Port),
				Running: true,
				PID:     e.PID,
				PGID:    e.PGID,
			}, nil
		}
	}
	return nil, nil
}

func (m *Manager) KillAll() error {
	entries, err := state.List()
	if err != nil {
		return err
	}
	var first error
	for _, e := range entries {
		if !process.Alive(e.PID) {
			_ = state.Remove(e.Name)
			continue
		}
		if err := process.Kill(e.PID, e.PGID); err != nil && first == nil {
			first = err
		}
		_ = state.Remove(e.Name)
	}
	return first
}

func (m *Manager) Kill(name string) error {
	e, err := state.Load(name)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s is not running", name)
		}
		return err
	}
	if process.Alive(e.PID) {
		if err := process.Kill(e.PID, e.PGID); err != nil {
			return err
		}
	}
	return state.Remove(name)
}

func (m *Manager) StartSwitch(name string) error {
	items, err := m.List()
	if err != nil {
		return err
	}
	var target *Item
	for i := range items {
		if items[i].Name == name {
			target = &items[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("project not found: %s", name)
	}
	if !target.Runnable || target.Command == "" {
		return fmt.Errorf("%s has no command (set command in .devctl.toml or config)", name)
	}
	if target.Running {
		return nil // already active
	}

	// port-based projects own the exclusive "dev server" slot
	if len(target.Ports) > 0 {
		if err := m.KillAll(); err != nil {
			return fmt.Errorf("kill existing: %w", err)
		}
		for _, port := range target.Ports {
			inUse, _ := process.PortInUse(port)
			if inUse {
				return fmt.Errorf("port %d already in use (not managed by devctl)", port)
			}
		}
	} else {
		// one-shot / no-port (e.g. go install): do not kill running servers
		_ = state.Remove(target.Name)
	}

	logPath, err := state.LogPath(target.Name)
	if err != nil {
		return err
	}
	pid, pgid, err := process.Start(target.Command, target.Path, logPath)
	if err != nil {
		return err
	}
	primary := 0
	if len(target.Ports) > 0 {
		primary = target.Ports[0]
	}
	return state.Save(state.Entry{
		Name:      target.Name,
		PID:       pid,
		PGID:      pgid,
		Port:      primary,
		Ports:     append([]int(nil), target.Ports...),
		Cwd:       target.Path,
		Command:   target.Command,
		StartedAt: time.Now(),
		LogPath:   logPath,
	})
}
