package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mame77/devctl/internal/config"
	"github.com/mame77/devctl/internal/jump"
	"github.com/mame77/devctl/internal/session"
)

const (
	maxPanelWidth  = 80
	panelHeightPct = 70 // percent of terminal height
	minPanelHeight = 10
	minListRows    = 3
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	cursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	// repository names stay white
	normalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	searchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	panelStyle   = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("15")).
			Padding(0, 1)
)

type Model struct {
	mgr      *session.Manager
	allItems []session.Item // full list
	cursor   int            // index into filtered list
	offset   int            // list scroll offset
	query    string         // name filter (case-insensitive substring)
	searching bool          // typing into /
	status   string
	errMsg   string
	width    int
	height   int
	quitting bool
	jumpPath string // set when quitting to jump via tmux
	wantFzf  bool   // quit then open fzf picker
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
	// keep selection by name across reload when possible
	var selected string
	if filtered := m.filtered(); len(filtered) > 0 && m.cursor < len(filtered) {
		selected = filtered[m.cursor].Name
	}
	m.allItems = items
	m.errMsg = ""
	m.cursor = 0
	if selected != "" {
		for i, it := range m.filtered() {
			if it.Name == selected {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
	m.ensureVisible(m.listRows())
}

func (m Model) filtered() []session.Item {
	q := strings.ToLower(strings.TrimSpace(m.query))
	if q == "" {
		return m.allItems
	}
	out := make([]session.Item, 0, len(m.allItems))
	for _, it := range m.allItems {
		if strings.Contains(strings.ToLower(it.Name), q) {
			out = append(out, it)
		}
	}
	return out
}

func (m *Model) clampCursor() {
	n := len(m.filtered())
	if n == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m Model) Init() tea.Cmd {
	return tickCmd()
}

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

type doneMsg struct {
	err    error
	status string
}

type editorDoneMsg struct {
	err  error
	path string
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ensureVisible(m.listRows())
		return m, nil

	case tickMsg:
		m.reload()
		return m, tickCmd()

	case doneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.errMsg = ""
			m.status = msg.status
		}
		m.reload()
		return m, nil

	case editorDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.errMsg = ""
			m.status = "edited " + msg.path
		}
		_ = m.mgr.ReloadConfig()
		m.reload()
		return m, nil

	case tea.KeyMsg:
		if m.searching {
			return m.updateSearch(msg)
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.searching = false
		if msg.String() == "esc" && m.query != "" {
			// first esc exits typing; keep filter. second clear is handled in normal mode with esc
		}
		return m, nil
	case "enter":
		m.searching = false
		return m, nil
	case "backspace", "ctrl+h":
		if m.query != "" {
			// remove last rune
			r := []rune(m.query)
			m.query = string(r[:len(r)-1])
			m.cursor = 0
			m.offset = 0
			m.clampCursor()
			m.ensureVisible(m.listRows())
		} else {
			m.searching = false
		}
		return m, nil
	case "ctrl+u":
		m.query = ""
		m.cursor = 0
		m.offset = 0
		m.ensureVisible(m.listRows())
		return m, nil
	case "down", "ctrl+n":
		items := m.filtered()
		if len(items) > 0 && m.cursor < len(items)-1 {
			m.cursor++
			m.ensureVisible(m.listRows())
		}
		return m, nil
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
			m.ensureVisible(m.listRows())
		}
		return m, nil
	}

	// printable runes (skip multi-key combos)
	if msg.Type == tea.KeyRunes && !msg.Alt {
		for _, r := range msg.Runes {
			if unicode.IsPrint(r) {
				m.query += string(r)
			}
		}
		m.cursor = 0
		m.offset = 0
		m.clampCursor()
		m.ensureVisible(m.listRows())
	}
	return m, nil
}

func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.filtered()
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "/":
		m.searching = true
		return m, nil
	case "esc":
		if m.query != "" {
			m.query = ""
			m.cursor = 0
			m.offset = 0
			m.ensureVisible(m.listRows())
		}
		return m, nil
	case "j", "down":
		if len(items) > 0 && m.cursor < len(items)-1 {
			m.cursor++
			m.ensureVisible(m.listRows())
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m.ensureVisible(m.listRows())
		}
	case "enter", "g":
		if len(items) == 0 {
			return m, nil
		}
		m.jumpPath = items[m.cursor].Path
		m.quitting = true
		return m, tea.Quit
	case "ctrl+g":
		m.wantFzf = true
		m.quitting = true
		return m, tea.Quit
	case "r":
		_ = m.mgr.ReloadConfig()
		m.reload()
		m.status = "reloaded"
	case "e":
		if len(items) == 0 {
			return m, nil
		}
		it := items[m.cursor]
		return m, openProjectEditor(it.Path, it.Name)
	case " ":
		if len(items) == 0 {
			return m, nil
		}
		name := items[m.cursor].Name
		return m, func() tea.Msg {
			err := m.mgr.StartSwitch(name)
			if err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{status: fmt.Sprintf("started %s", name)}
		}
	case "x":
		if len(items) == 0 {
			return m, nil
		}
		name := items[m.cursor].Name
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
	n := len(m.filtered())
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
	nameStyle := normalStyle
	if i == m.cursor {
		cursor = cursorStyle.Render("❯ ")
		nameStyle = cursorStyle
	}

	mark := " "
	name := nameStyle.Render(it.Name)
	extra := ""
	if it.Running {
		mark = runningStyle.Render("●")
		label := "  running"
		if it.Port > 0 {
			label += fmt.Sprintf("  :%d", it.Port)
		}
		if i == m.cursor {
			extra = cursorStyle.Render(label)
		} else {
			extra = runningStyle.Render(label)
		}
	} else if it.Done {
		mark = statusStyle.Render("✓")
		if i == m.cursor {
			extra = cursorStyle.Render("  Done")
		} else {
			extra = statusStyle.Render("  Done")
		}
	} else if it.Port > 0 {
		if i == m.cursor {
			extra = cursorStyle.Render(fmt.Sprintf("  :%d", it.Port))
		} else {
			extra = dimStyle.Render(fmt.Sprintf("  :%d", it.Port))
		}
	}

	return fmt.Sprintf("%s%s %s%s", cursor, mark, name, extra)
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
	items := m.filtered()

	activeName := "none"
	activeExtra := ""
	for _, it := range m.allItems {
		if it.Running {
			activeName = it.Name
			activeExtra = fmt.Sprintf(" (pid %d)", it.PID)
			if it.Port > 0 {
				activeExtra += fmt.Sprintf("  port %d", it.Port)
			}
			break
		}
		if it.Done && activeName == "none" {
			activeName = it.Name
			activeExtra = " Done"
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
	if len(m.allItems) == 0 {
		empty := []string{
			dimStyle.Render("(no projects — ghq repos, .devctl.toml,"),
			dimStyle.Render(" or [[projects]] in config are listed)"),
		}
		for i := 0; i < listRows; i++ {
			if i < len(empty) {
				body.WriteString(empty[i])
			}
			body.WriteString("\n")
		}
	} else if len(items) == 0 {
		for i := 0; i < listRows; i++ {
			if i == 0 {
				body.WriteString(dimStyle.Render(fmt.Sprintf("(no match for %q)", m.query)))
			}
			body.WriteString("\n")
		}
	} else {
		end := m.offset + listRows
		if end > len(items) {
			end = len(items)
		}
		shown := 0
		for i := m.offset; i < end; i++ {
			body.WriteString(m.renderItem(i, items[i]))
			body.WriteString("\n")
			shown++
		}
		for shown < listRows {
			body.WriteString("\n")
			shown++
		}
	}

	body.WriteString(dimStyle.Render(strings.Repeat("─", contentW)))
	body.WriteString("\n")

	// status / search line
	if m.searching {
		body.WriteString(searchStyle.Render("/" + m.query + "█"))
		body.WriteString(dimStyle.Render(fmt.Sprintf("  %d/%d", len(items), len(m.allItems))))
	} else if m.query != "" {
		body.WriteString(searchStyle.Render("/" + m.query))
		body.WriteString(dimStyle.Render(fmt.Sprintf("  %d/%d  esc clear", len(items), len(m.allItems))))
	} else if m.errMsg != "" {
		body.WriteString(errStyle.Render("error: " + m.errMsg))
	} else if m.status != "" {
		body.WriteString(statusStyle.Render(m.status))
	}
	body.WriteString("\n")

	var help string
	if m.searching {
		help = "type to filter  enter done  esc cancel input  ↑↓ move  ctrl+u clear"
	} else {
		help = "j/k move  / search  e edit  enter/g jump  ^g fzf  space start  x kill  a kill-all  r reload  q quit"
		if len(items) > listRows || m.query != "" {
			help = fmt.Sprintf("%s  (%d/%d)", help, m.cursor+1, len(items))
		}
	}
	body.WriteString(helpStyle.Render(help))

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

func openProjectEditor(dir, name string) tea.Cmd {
	path, err := config.EnsureProjectFile(dir, name)
	if err != nil {
		return func() tea.Msg {
			return editorDoneMsg{err: err}
		}
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "nvim"
	}
	// EDITOR may be "code --wait" etc.
	parts := strings.Fields(editor)
	bin := parts[0]
	args := append(parts[1:], path)
	c := exec.Command(bin, args...)
	c.Dir = dir
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{err: err, path: path}
	})
}

func Run(mgr *session.Manager) error {
	p := tea.NewProgram(New(mgr), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return err
	}
	m, ok := final.(Model)
	if !ok {
		return nil
	}
	if m.wantFzf {
		return jump.Interactive()
	}
	if m.jumpPath != "" {
		return jump.To(m.jumpPath)
	}
	return nil
}
