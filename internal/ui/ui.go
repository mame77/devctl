package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mame77/devctl/internal/session"
)

const (
	maxPanelWidth   = 80
	panelHeightPct  = 70 // percent of terminal height
	minPanelHeight  = 10
	minListRows     = 3
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	cursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	normalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	panelStyle   = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)
)

type Model struct {
	mgr      *session.Manager
	items    []session.Item
	cursor   int
	offset   int // list scroll offset
	status   string
	errMsg   string
	width    int
	height   int
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
	m.ensureVisible(m.listRows())
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
		m.ensureVisible(m.listRows())
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
				m.ensureVisible(m.listRows())
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
				m.ensureVisible(m.listRows())
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

func (m Model) panelWidth() int {
	w := m.width
	if w <= 0 {
		return 60
	}
	inner := w - 4
	if inner < 20 {
		inner = w
	}
	if inner > maxPanelWidth {
		return maxPanelWidth
	}
	return inner
}

// panelHeight is a fixed fraction of the terminal height.
func (m Model) panelHeight() int {
	h := m.height
	if h <= 0 {
		return minPanelHeight
	}
	ph := h * panelHeightPct / 100
	if ph < minPanelHeight {
		ph = minPanelHeight
	}
	if ph > h {
		ph = h
	}
	return ph
}

// chromeLines are non-list rows inside the panel content (always fixed).
// header + top rule + bottom rule + status + help
func (m Model) chromeLines() int {
	return 5
}

// listRows is the fixed number of rows reserved for the project list.
func (m Model) listRows() int {
	// panel height includes top/bottom border (2)
	inner := m.panelHeight() - 2
	rows := inner - m.chromeLines()
	if rows < minListRows {
		rows = minListRows
	}
	return rows
}

// ensureVisible scrolls offset so cursor stays in the visible list window.
func (m *Model) ensureVisible(listRows int) {
	if listRows < 1 {
		listRows = 1
	}
	n := len(m.items)
	if n == 0 {
		m.offset = 0
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+listRows {
		m.offset = m.cursor - listRows + 1
	}
	maxOff := n - listRows
	if maxOff < 0 {
		maxOff = 0
	}
	if m.offset > maxOff {
		m.offset = maxOff
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m Model) renderItem(i int, it session.Item) string {
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

	return fmt.Sprintf("%s%s %s%s%s", cursor, mark, name, extra, src)
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	pw := m.panelWidth()
	ph := m.panelHeight()
	contentW := pw - 4
	if contentW < 10 {
		contentW = 10
	}
	listRows := m.listRows()

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

	var body strings.Builder

	header := titleStyle.Render("devctl") + "  │  active: " +
		statusStyle.Render(activeName) + dimStyle.Render(activeExtra)
	body.WriteString(header)
	body.WriteString("\n")
	body.WriteString(dimStyle.Render(strings.Repeat("─", contentW)))
	body.WriteString("\n")

	// fixed-height list viewport
	if len(m.items) == 0 {
		empty := []string{
			dimStyle.Render("(no projects — add [[projects]] in ~/.config/devctl/config.toml"),
			dimStyle.Render(" or place .devctl.toml under scan_roots)"),
		}
		for i := 0; i < listRows; i++ {
			if i < len(empty) {
				body.WriteString(empty[i])
			}
			body.WriteString("\n")
		}
	} else {
		end := m.offset + listRows
		if end > len(m.items) {
			end = len(m.items)
		}
		shown := 0
		for i := m.offset; i < end; i++ {
			body.WriteString(m.renderItem(i, m.items[i]))
			body.WriteString("\n")
			shown++
		}
		// pad remaining rows so panel height stays fixed
		for shown < listRows {
			body.WriteString("\n")
			shown++
		}
	}

	body.WriteString(dimStyle.Render(strings.Repeat("─", contentW)))
	body.WriteString("\n")

	// always reserve status line so panel height stays fixed
	if m.errMsg != "" {
		body.WriteString(errStyle.Render("error: " + m.errMsg))
	} else if m.status != "" {
		body.WriteString(statusStyle.Render(m.status))
	}
	body.WriteString("\n")

	help := "j/k move  space start/switch  x kill  a kill-all  r reload  q quit"
	if len(m.items) > listRows {
		help = fmt.Sprintf("%s  (%d/%d)", help, m.cursor+1, len(m.items))
	}
	body.WriteString(helpStyle.Render(help))

	// fixed size from padded list rows; Width only (Height would double-count borders)
	panel := panelStyle.Width(pw).Render(strings.TrimRight(body.String(), "\n"))

	tw, th := m.width, m.height
	if tw <= 0 {
		tw = pw
	}
	if th <= 0 {
		th = ph
	}
	return lipgloss.Place(tw, th, lipgloss.Center, lipgloss.Center, panel)
}

func Run(mgr *session.Manager) error {
	p := tea.NewProgram(New(mgr), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
