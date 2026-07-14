package session

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mame77/devctl/internal/config"
	"github.com/mame77/devctl/internal/discover"
	"github.com/mame77/devctl/internal/process"
	"github.com/mame77/devctl/internal/state"
)

type Item struct {
	Name         string
	Path         string
	Command      string
	Ports        []int
	Running      bool
	Done         bool // one-shot finished OK / ports ready (transient)
	Failed       bool // process exited unexpectedly / non-zero
	PID          int
	PGID         int
	Source       string
	Runnable     bool
	Pinned       bool
	PortsReadyAt *time.Time
}

func (it Item) PrimaryPort() int {
	if len(it.Ports) > 0 {
		return it.Ports[0]
	}
	return 0
}

type PortConflict struct {
	Port int
	Name string
	PID  int // non-zero when the holder is outside devctl management
}

type PortConflictError struct {
	Conflicts []PortConflict
}

func (e *PortConflictError) Error() string {
	parts := make([]string, len(e.Conflicts))
	for i, c := range e.Conflicts {
		parts[i] = fmt.Sprintf(":%d (%s)", c.Port, c.Name)
	}
	return "port conflict: " + strings.Join(parts, ", ")
}

type Manager struct {
	mu       sync.RWMutex
	cfg      config.Config
	projects []discover.Project
}

func New() (*Manager, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	mgr := &Manager{cfg: cfg}
	if ok, err := state.HasDiscoveredProjects(); err == nil && ok {
		scanned, err := state.LoadDiscoveredProjects()
		if err != nil {
			return nil, err
		}
		mgr.projects = mergeWithPins(cfg, discover.Refresh(scanned))
		return mgr, nil
	}
	if err := mgr.Rescan(); err != nil {
		return nil, err
	}
	return mgr, nil
}

func (m *Manager) ReloadConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.cfg = cfg
	scanned, loadErr := state.LoadDiscoveredProjects()
	if loadErr == nil {
		m.projects = mergeWithPins(cfg, discover.Refresh(scanned))
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) List() ([]Item, error) {
	m.mu.RLock()
	projects := append([]discover.Project(nil), m.projects...)
	m.mu.RUnlock()
	entries, err := state.List()
	if err != nil {
		return nil, err
	}

	statusByName := map[string]state.Entry{}
	for _, e := range entries {
		if process.Alive(e.PID) {
			if e.FinishedAt != nil {
				e.FinishedAt = nil
				e.ExitCode = nil
				_ = state.Save(e)
			}
			statusByName[e.Name] = e
			continue
		}
		// process ended
		now := time.Now()
		if e.FinishedAt == nil {
			e.FinishedAt = &now
			// unknown exit — treat port servers as fail, one-shots as done
			if e.ExitCode == nil {
				code := 0
				if len(config.NormalizePorts(e.Ports, e.Port)) > 0 {
					code = 1
				}
				e.ExitCode = &code
			}
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
			Pinned:   p.Pinned,
		}
		if e, ok := statusByName[p.Name]; ok {
			it.PID = e.PID
			it.PGID = e.PGID
			if len(it.Ports) == 0 {
				it.Ports = config.NormalizePorts(e.Ports, e.Port)
			}
			if process.Alive(e.PID) {
				it.Running = true
				it.PortsReadyAt = e.PortsReadyAt
				if len(it.Ports) > 0 && portsListening(it.Ports) {
					if e.PortsReadyAt == nil {
						now := time.Now()
						e.PortsReadyAt = &now
						_ = state.Save(e)
						it.PortsReadyAt = &now
					}
					if it.PortsReadyAt != nil && time.Since(*it.PortsReadyAt) < state.DoneTTL {
						it.Done = true
					}
				} else {
					if e.PortsReadyAt != nil {
						e.PortsReadyAt = nil
						_ = state.Save(e)
						it.PortsReadyAt = nil
					}
				}
			} else if e.ExitCode != nil && *e.ExitCode == 0 {
				it.Done = true
			} else {
				// port-based server died, or non-zero exit
				it.Failed = true
			}
		}
		items = append(items, it)
	}
	sortItems(items)
	return items, nil
}

func portsListening(ports []int) bool {
	for _, p := range ports {
		inUse, _ := process.PortInUse(p)
		if !inUse {
			return false
		}
	}
	return len(ports) > 0
}

func sortItems(items []Item) {
	running := make([]Item, 0)
	pinned := make([]Item, 0)
	rest := make([]Item, 0)
	for _, it := range items {
		if it.Running {
			running = append(running, it)
		} else if it.Pinned {
			pinned = append(pinned, it)
		} else {
			rest = append(rest, it)
		}
	}
	idx := 0
	for i := range running {
		items[idx] = running[i]
		idx++
	}
	for i := range pinned {
		items[idx] = pinned[i]
		idx++
	}
	for i := range rest {
		items[idx] = rest[i]
		idx++
	}
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
		conflicts, err := m.findConflicts(target.Ports, name)
		if err != nil {
			return err
		}
		for _, port := range target.Ports {
			inUse, _ := process.PortInUse(port)
			if !inUse {
				continue
			}
			alreadyListed := false
			for _, c := range conflicts {
				if c.Port == port {
					alreadyListed = true
					break
				}
			}
			if alreadyListed {
				continue
			}
			pids, _ := process.FindListeners(port)
			if len(pids) == 0 {
				names, _ := process.FindDockerHolders(port)
				if len(names) > 0 {
					for _, n := range names {
						conflicts = append(conflicts, PortConflict{Port: port, Name: fmt.Sprintf("docker:%s", n)})
					}
					continue
				}
				conflicts = append(conflicts, PortConflict{Port: port, Name: "unknown process"})
				continue
			}
			for _, pid := range pids {
				conflicts = append(conflicts, PortConflict{Port: port, Name: fmt.Sprintf("pid:%d", pid), PID: pid})
			}
		}
		if len(conflicts) > 0 {
			return &PortConflictError{Conflicts: conflicts}
		}
	} else {
		_ = state.Remove(target.Name)
	}

	return m.startProcess(target)
}

func (m *Manager) StartSwitchForce(name string) error {
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
		return nil
	}

	if len(target.Ports) > 0 {
		conflicts, err := m.findConflicts(target.Ports, name)
		if err != nil {
			return err
		}
		for _, c := range conflicts {
			if e, loadErr := state.Load(c.Name); loadErr == nil {
				_ = process.Kill(e.PID, e.PGID)
			}
			_ = state.Remove(c.Name)
		}
		for _, port := range target.Ports {
			inUse, _ := process.PortInUse(port)
			if !inUse {
				continue
			}
			pids, _ := process.FindListeners(port)
			if len(pids) > 0 {
				_ = process.KillPIDs(pids)
			} else {
				names, _ := process.FindDockerHolders(port)
				if len(names) > 0 {
					_ = process.StopContainers(names)
				}
			}
		}
		for _, port := range target.Ports {
			for i := 0; i < 10; i++ {
				if inUse, _ := process.PortInUse(port); !inUse {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			if inUse, _ := process.PortInUse(port); inUse {
				return fmt.Errorf("port %d still in use (could not kill holder)", port)
			}
		}
	}

	return m.startProcess(target)
}

func (m *Manager) findConflicts(targetPorts []int, excludeName string) ([]PortConflict, error) {
	entries, err := state.List()
	if err != nil {
		return nil, err
	}
	var conflicts []PortConflict
	for _, e := range entries {
		if e.Name == excludeName || !process.Alive(e.PID) {
			continue
		}
		runningPorts := config.NormalizePorts(e.Ports, e.Port)
		for _, tp := range targetPorts {
			for _, rp := range runningPorts {
				if tp == rp {
					conflicts = append(conflicts, PortConflict{Port: tp, Name: e.Name})
				}
			}
		}
	}
	return conflicts, nil
}

func (m *Manager) startProcess(target *Item) error {
	logPath, err := state.LogPath(target.Name)
	if err != nil {
		return err
	}
	nameForCB := target.Name
	var startedPID int
	pid, pgid, err := process.Start(target.Command, target.Path, logPath, func(exitCode int) {
		e, err := state.Load(nameForCB)
		if err != nil {
			return
		}
		if e.PID != startedPID {
			return
		}
		now := time.Now()
		e.FinishedAt = &now
		e.ExitCode = &exitCode
		_ = state.Save(e)
	})
	if err != nil {
		return err
	}
	startedPID = pid
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

func (m *Manager) Rescan() error {
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()
	scanned, err := discover.Scan(cfg)
	if err != nil {
		return err
	}
	if err := state.SaveDiscoveredProjects(scanned); err != nil {
		return err
	}
	projects := mergeWithPins(cfg, scanned)
	m.mu.Lock()
	m.projects = projects
	m.mu.Unlock()
	return nil
}

func mergeWithPins(cfg config.Config, scanned []discover.Project) []discover.Project {
	merged := discover.Merge(cfg, scanned)
	pins, _ := state.LoadPins()
	return discover.ApplyPins(merged, pins)
}
