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
	Port     int
	Running  bool
	PID      int
	PGID     int
	Source   string
	Runnable bool
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
	runningByName := map[string]state.Entry{}
	for _, e := range entries {
		if process.Alive(e.PID) {
			runningByName[e.Name] = e
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
			Port:     p.Port,
			Source:   p.Source,
			Runnable: p.Runnable,
		}
		if e, ok := runningByName[p.Name]; ok {
			it.Running = true
			it.PID = e.PID
			it.PGID = e.PGID
			if it.Port == 0 {
				it.Port = e.Port
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
	// also surface orphan running states not in list
	entries, _ := state.List()
	for _, e := range entries {
		if process.Alive(e.PID) {
			return &Item{
				Name:    e.Name,
				Path:    e.Cwd,
				Command: e.Command,
				Port:    e.Port,
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
		return fmt.Errorf("%s has no start command (jump-only; press enter to open in tmux)", name)
	}
	if target.Running {
		return nil // already active
	}

	// kill all others (and any orphans)
	if err := m.KillAll(); err != nil {
		return fmt.Errorf("kill existing: %w", err)
	}

	if target.Port > 0 {
		inUse, _ := process.PortInUse(target.Port)
		if inUse {
			return fmt.Errorf("port %d already in use (not managed by devctl)", target.Port)
		}
	}

	logPath, err := state.LogPath(target.Name)
	if err != nil {
		return err
	}
	pid, pgid, err := process.Start(target.Command, target.Path, logPath)
	if err != nil {
		return err
	}
	return state.Save(state.Entry{
		Name:      target.Name,
		PID:       pid,
		PGID:      pgid,
		Port:      target.Port,
		Cwd:       target.Path,
		Command:   target.Command,
		StartedAt: time.Now(),
		LogPath:   logPath,
	})
}
