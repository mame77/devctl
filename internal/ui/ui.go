package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mame77/devctl/internal/session"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	normalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type Model struct {
	mgr     *session.Manager
	items   []session.Item
	cursor  int
	status  string
	errMsg  string
	width   int
	height  int
	quitting bool
}

func New(mgr *session.Manager) Model {
	m := Model{mgr: mgr}
	m.reload()
	return m
}

func (m *Model) reload() {
	items, err := m.mgr.List()
	if err != nil {
		m.errMsg = err.Error()
		return
	}
	m.items = items
	m.errMsg = ""
	if m.cursor >= len(m.items) && len(m.items) > 0 {
		m.cursor = len(m.items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

type doneMsg struct {
	err    error
	status string
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case doneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.errMsg = ""
			m.status = msg.status
		}
		m.reload()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "j", "down":
			if len(m.items) > 0 && m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "r":
			_ = m.mgr.ReloadConfig()
			m.reload()
			m.status = "reloaded"
		case " ":
			if len(m.items) == 0 {
				return m, nil
			}
			name := m.items[m.cursor].Name
			return m, func() tea.Msg {
				err := m.mgr.StartSwitch(name)
				if err != nil {
					return doneMsg{err: err}
				}
				return doneMsg{status: fmt.Sprintf("started %s", name)}
			}
		case "x":
			if len(m.items) == 0 {
				return m, nil
			}
			name := m.items[m.cursor].Name
			return m, func() tea.Msg {
				err := m.mgr.Kill(name)
				if err != nil {
					return doneMsg{err: err}
				}
				return doneMsg{status: fmt.Sprintf("killed %s", name)}
			}
		case "a":
			return m, func() tea.Msg {
				err := m.mgr.KillAll()
				if err != nil {
					return doneMsg{err: err}
				}
				return doneMsg{status: "killed all"}
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	activeName := "none"
	activeExtra := ""
	for _, it := range m.items {
		if it.Running {
			activeName = it.Name
			activeExtra = fmt.Sprintf(" (pid %d)", it.PID)
			if it.Port > 0 {
				activeExtra += fmt.Sprintf("  port %d", it.Port)
			}
			break
		}
	}

	header := titleStyle.Render("devctl") + "  │  active: " +
		statusStyle.Render(activeName) + dimStyle.Render(activeExtra)
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", max(40, m.width))))
	b.WriteString("\n")

	if len(m.items) == 0 {
		b.WriteString(dimStyle.Render("  (no projects — add [[projects]] in ~/.config/devctl/config.toml"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("   or place .devctl.toml under scan_roots)"))
		b.WriteString("\n")
	} else {
		for i, it := range m.items {
			cursor := "  "
			lineStyle := normalStyle
			if i == m.cursor {
				cursor = cursorStyle.Render("❯ ")
				lineStyle = cursorStyle
			}

			mark := " "
			name := it.Name
			extra := ""
			if it.Running {
				mark = runningStyle.Render("●")
				name = runningStyle.Render(it.Name)
				extra = runningStyle.Render("  running")
				if it.Port > 0 {
					extra += runningStyle.Render(fmt.Sprintf("  :%d", it.Port))
				}
			} else {
				name = lineStyle.Render(it.Name)
				if it.Port > 0 {
					extra = dimStyle.Render(fmt.Sprintf("  :%d", it.Port))
				}
			}

			src := ""
			if it.Source == "scan" {
				src = dimStyle.Render("  [scan]")
			}

			b.WriteString(fmt.Sprintf("%s%s %s%s%s\n", cursor, mark, name, extra, src))
		}
	}

	b.WriteString(dimStyle.Render(strings.Repeat("─", max(40, m.width))))
	b.WriteString("\n")

	if m.errMsg != "" {
		b.WriteString(errStyle.Render("error: " + m.errMsg))
		b.WriteString("\n")
	} else if m.status != "" {
		b.WriteString(statusStyle.Render(m.status))
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("j/k move  space start/switch  x kill  a kill-all  r reload  q quit"))
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func Run(mgr *session.Manager) error {
	p := tea.NewProgram(New(mgr), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
