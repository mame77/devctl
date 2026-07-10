package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mame77/devctl/internal/config"
	"github.com/mame77/devctl/internal/jump"
	"github.com/mame77/devctl/internal/session"
)

const (
	panelHeightPct = 70 // percent of terminal height
	minPanelHeight = 10
	minListRows    = 3
	minRightWidth  = 20
	maxRightWidth  = 28
	maxTotalWidth  = 78 // keep UI compact and centered
	minLeftWidth   = 36
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
	mgr       *session.Manager
	allItems  []session.Item // full list
	cursor    int            // index into filtered list
	offset    int            // list scroll offset
	query     string         // name filter (case-insensitive substring)
	searching bool           // typing into /
	focus     string // "list" | "ports"
	portCur   int    // cursor in running ports panel
	showHelp  bool   // ctrl+p toggles key help overlay
	status    string
	errMsg    string
	width     int
	height    int
	quitting  bool
	jumpPath  string // set when quitting to jump via tmux
	wantFzf   bool   // quit then open fzf picker
}

func New(mgr *session.Manager) Model {
	m := Model{mgr: mgr, focus: "list"}
	m.reload()
	// initial open: cursor on repo matching cwd
	if idx := indexForCwd(m.filtered()); idx >= 0 {
		m.cursor = idx
		m.ensureVisible(m.listRows())
	}
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
	// do not clear errMsg/status here — tick reloads must not hide feedback
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
	m.clampPortCursor()
	m.ensureVisible(m.listRows())
}

func (m Model) runningItems() []session.Item {
	var out []session.Item
	for _, it := range m.allItems {
		if it.Running {
			out = append(out, it)
		}
	}
	return out
}

func (m *Model) clampPortCursor() {
	n := len(m.runningItems())
	if n == 0 {
		m.portCur = 0
		return
	}
	if m.portCur >= n {
		m.portCur = n - 1
	}
	if m.portCur < 0 {
		m.portCur = 0
	}
}

// indexForCwd returns the filtered-list index for the project that best matches
// the current working directory (exact path, or cwd under project root).
func indexForCwd(items []session.Item) int {
	cwd, err := os.Getwd()
	if err != nil {
		return -1
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return -1
	}
	cwd = filepath.Clean(cwd)

	best := -1
	bestLen := -1
	for i, it := range items {
		p, err := filepath.Abs(it.Path)
		if err != nil {
			p = filepath.Clean(it.Path)
		} else {
			p = filepath.Clean(p)
		}
		if cwd == p || strings.HasPrefix(cwd, p+string(os.PathSeparator)) {
			if len(p) > bestLen {
				best = i
				bestLen = len(p)
			}
		}
	}
	return best
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
	case "up":
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
	running := m.runningItems()

	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "ctrl+p":
		m.showHelp = !m.showHelp
		return m, nil
	case "tab":
		m.showHelp = false
		if m.focus == "ports" {
			m.focus = "list"
		} else {
			m.focus = "ports"
			m.clampPortCursor()
		}
		return m, nil
	case "l", "right":
		if m.focus == "list" {
			m.showHelp = false
			m.focus = "ports"
			m.clampPortCursor()
		}
		return m, nil
	case "h", "left":
		if m.focus == "ports" {
			m.focus = "list"
		}
		return m, nil
	case "/":
		m.focus = "list"
		m.showHelp = false
		m.searching = true
		return m, nil
	case "esc":
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		if m.focus == "ports" {
			m.focus = "list"
			return m, nil
		}
		if m.query != "" {
			m.query = ""
			m.cursor = 0
			m.offset = 0
			m.ensureVisible(m.listRows())
		}
		return m, nil
	case "j", "down":
		if m.focus == "ports" {
			if len(running) > 0 && m.portCur < len(running)-1 {
				m.portCur++
			}
			return m, nil
		}
		if len(items) > 0 && m.cursor < len(items)-1 {
			m.cursor++
			m.ensureVisible(m.listRows())
		}
	case "k", "up":
		if m.focus == "ports" {
			if m.portCur > 0 {
				m.portCur--
			}
			return m, nil
		}
		if m.cursor > 0 {
			m.cursor--
			m.ensureVisible(m.listRows())
		}
	case "enter", "g":
		if m.focus == "ports" {
			return m, m.openRunningPort()
		}
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
		if m.focus == "ports" {
			return m, nil
		}
		if len(items) == 0 {
			return m, nil
		}
		it := items[m.cursor]
		return m, openProjectEditor(it.Path, it.Name)
	case " ":
		if m.focus == "ports" {
			return m, nil
		}
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
		if m.focus == "ports" {
			if len(running) == 0 {
				return m, nil
			}
			name := running[m.portCur].Name
			return m, func() tea.Msg {
				err := m.mgr.Kill(name)
				if err != nil {
					return doneMsg{err: err}
				}
				return doneMsg{status: fmt.Sprintf("killed %s", name)}
			}
		}
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
	case "o":
		if m.focus == "ports" {
			return m, m.openRunningPort()
		}
		if len(items) == 0 {
			return m, nil
		}
		it := items[m.cursor]
		port := it.PrimaryPort()
		if port <= 0 {
			return m, func() tea.Msg {
				return doneMsg{err: fmt.Errorf("%s has no port (set ports = [ui, ...])", it.Name)}
			}
		}
		url := fmt.Sprintf("http://localhost:%d", port)
		return m, func() tea.Msg {
			if err := openURL(url); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{status: fmt.Sprintf("opened %s", url)}
		}
	}
	return m, nil
}

func (m Model) openRunningPort() tea.Cmd {
	running := m.runningItems()
	if len(running) == 0 {
		return nil
	}
	it := running[m.portCur]
	port := it.PrimaryPort()
	if port <= 0 {
		return func() tea.Msg {
			return doneMsg{err: fmt.Errorf("%s has no port", it.Name)}
		}
	}
	url := fmt.Sprintf("http://localhost:%d", port)
	return func() tea.Msg {
		if err := openURL(url); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{status: fmt.Sprintf("opened %s", url)}
	}
}

// layoutWidths returns outer panel content width and left/right column widths.
// outer total includes padding inside border; left+1(divider)+right = content.
func (m Model) layoutWidths() (outer, left, right int) {
	w := m.width
	if w <= 0 {
		w = maxTotalWidth
	}
	outer = w - 4
	if outer > maxTotalWidth {
		outer = maxTotalWidth
	}
	if outer < 48 {
		outer = w
		if outer < 48 {
			outer = 48
		}
	}
	// content width inside border+padding (Width includes padding area in lipgloss)
	content := outer - 4 // approx for internal calc; actual uses left/right
	if content < 40 {
		content = outer
	}
	right = content * 30 / 100
	if right < minRightWidth {
		right = minRightWidth
	}
	if right > maxRightWidth {
		right = maxRightWidth
	}
	left = content - right - 1 // 1 for vertical divider
	if left < minLeftWidth {
		left = minLeftWidth
		content = left + right + 1
		outer = content + 4
		if outer > maxTotalWidth {
			outer = maxTotalWidth
			content = outer - 4
			right = content * 30 / 100
			if right < minRightWidth {
				right = minRightWidth
			}
			if right > maxRightWidth {
				right = maxRightWidth
			}
			left = content - right - 1
		}
	}
	return outer, left, right
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

// chromeLines: title + col headers + top rule + bottom rule + status
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
	// white if command exists, gray if not; cursor still yellow when selected
	nameStyle := normalStyle
	if !it.Runnable {
		nameStyle = dimStyle
	}
	if i == m.cursor {
		cursor = cursorStyle.Render("❯ ")
		nameStyle = cursorStyle
	}

	mark := " "
	name := nameStyle.Render(it.Name)
	extra := ""
	if it.Running {
		mark = runningStyle.Render("●")
		extra = runningStyle.Render("  running")
	} else if it.Failed {
		mark = errStyle.Render("✗")
		extra = errStyle.Render("  failed")
	} else if it.Done {
		mark = statusStyle.Render("✓")
		extra = statusStyle.Render("  Done")
	}

	return fmt.Sprintf("%s%s %s%s", cursor, mark, name, extra)
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	outerW, leftW, rightW := m.layoutWidths()
	ph := m.panelHeight()
	listRows := m.listRows()
	items := m.filtered()
	running := m.runningItems()
	div := dimStyle.Render("│")

	activeName := "none"
	activeExtra := ""
	for _, it := range m.allItems {
		if it.Running {
			activeName = it.Name
			if len(it.Ports) > 0 {
				parts := make([]string, len(it.Ports))
				for i, p := range it.Ports {
					parts[i] = fmt.Sprintf("%d", p)
				}
				activeExtra = "  :" + strings.Join(parts, " :")
			}
			break
		}
		if it.Done && activeName == "none" {
			activeName = it.Name
			activeExtra = " Done"
		}
	}

	// column headers
	repoHead := "repositories"
	portsHead := "ports"
	if m.focus == "list" {
		repoHead = cursorStyle.Render(repoHead)
		portsHead = dimStyle.Render(portsHead)
	} else {
		repoHead = dimStyle.Render(repoHead)
		portsHead = cursorStyle.Render(portsHead)
	}

	// build left column lines (listRows body)
	leftLines := make([]string, 0, listRows)
	if len(m.allItems) == 0 {
		leftLines = append(leftLines,
			dimStyle.Render("(no projects)"),
			dimStyle.Render("(add config via e)"),
		)
	} else if len(items) == 0 {
		leftLines = append(leftLines, dimStyle.Render(fmt.Sprintf("(no match for %q)", m.query)))
	} else {
		end := m.offset + listRows
		if end > len(items) {
			end = len(items)
		}
		for i := m.offset; i < end; i++ {
			leftLines = append(leftLines, m.renderItem(i, items[i]))
		}
	}
	for len(leftLines) < listRows {
		leftLines = append(leftLines, "")
	}

	// build right column lines
	rightLines := m.portLines(rightW, listRows)

	var body strings.Builder
	// title row
	body.WriteString(titleStyle.Render("devctl"))
	body.WriteString("  │  active: ")
	body.WriteString(statusStyle.Render(activeName))
	body.WriteString(dimStyle.Render(activeExtra))
	body.WriteString("\n")

	// section headers with vertical divider
	body.WriteString(padCell(repoHead, leftW))
	body.WriteString(div)
	body.WriteString(padCell(" "+portsHead+dimStyle.Render(fmt.Sprintf(" %d", len(running))), rightW))
	body.WriteString("\n")

	// separator under headers (with ┼)
	body.WriteString(dimStyle.Render(strings.Repeat("─", leftW) + "┼" + strings.Repeat("─", rightW)))
	body.WriteString("\n")

	// body rows
	for i := 0; i < listRows; i++ {
		body.WriteString(padCell(leftLines[i], leftW))
		body.WriteString(div)
		body.WriteString(padCell(rightLines[i], rightW))
		body.WriteString("\n")
	}

	// bottom rule
	body.WriteString(dimStyle.Render(strings.Repeat("─", leftW) + "┴" + strings.Repeat("─", rightW)))
	body.WriteString("\n")

	// status / search only
	if m.searching {
		body.WriteString(searchStyle.Render("/" + m.query + "█"))
		body.WriteString(dimStyle.Render(fmt.Sprintf("  %d/%d", len(items), len(m.allItems))))
	} else if m.query != "" {
		body.WriteString(searchStyle.Render("/" + m.query))
		body.WriteString(dimStyle.Render(fmt.Sprintf("  %d/%d", len(items), len(m.allItems))))
	} else if m.errMsg != "" {
		body.WriteString(errStyle.Render("error: " + m.errMsg))
	} else if m.status != "" {
		body.WriteString(statusStyle.Render(m.status))
	}

	panel := panelStyle.Width(outerW).Render(strings.TrimRight(body.String(), "\n"))
	if m.showHelp {
		help := panelStyle.Width(outerW).Render(helpText())
		panel = lipgloss.JoinVertical(lipgloss.Left, panel, "", help)
	}

	tw, th := m.width, m.height
	if tw <= 0 {
		tw = outerW
	}
	if th <= 0 {
		th = ph
	}
	return lipgloss.Place(tw, th, lipgloss.Center, lipgloss.Center, panel)
}

func helpText() string {
	return strings.Join([]string{
		titleStyle.Render("keys") + dimStyle.Render("  ctrl+p close"),
		dimStyle.Render("j/k") + " move   " + dimStyle.Render("tab/h/l") + " list/ports",
		dimStyle.Render("/") + " search   " + dimStyle.Render("e") + " edit   " + dimStyle.Render("enter/g") + " jump",
		dimStyle.Render("space") + " start   " + dimStyle.Render("o") + " open   " + dimStyle.Render("x") + " kill",
		dimStyle.Render("a") + " kill-all   " + dimStyle.Render("r") + " reload   " + dimStyle.Render("q") + " quit",
	}, "\n")
}

// padCell left-aligns s into width cells (ANSI-aware via lipgloss.Width).
func padCell(s string, width int) string {
	if width <= 0 {
		return s
	}
	w := lipgloss.Width(s)
	if w > width {
		// crude truncate by runes then re-check
		r := []rune(s)
		for len(r) > 0 && lipgloss.Width(string(r)) > width-1 {
			r = r[:len(r)-1]
		}
		s = string(r) + "…"
		w = lipgloss.Width(s)
	}
	if w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}

func (m Model) portLines(width, rows int) []string {
	running := m.runningItems()
	lines := make([]string, 0, rows)
	if len(running) == 0 {
		lines = append(lines, dimStyle.Render(" (none)"))
	} else {
		start := 0
		if m.portCur >= rows {
			start = m.portCur - rows + 1
		}
		end := start + rows
		if end > len(running) {
			end = len(running)
			start = end - rows
			if start < 0 {
				start = 0
			}
		}
		for i := start; i < end; i++ {
			it := running[i]
			cur := " "
			nameStyle := normalStyle
			if m.focus == "ports" && i == m.portCur {
				cur = cursorStyle.Render("❯")
				nameStyle = cursorStyle
			}
			ports := ""
			if len(it.Ports) > 0 {
				parts := make([]string, len(it.Ports))
				for j, p := range it.Ports {
					parts[j] = fmt.Sprintf(":%d", p)
				}
				ports = strings.Join(parts, " ")
			}
			line := " " + cur + runningStyle.Render("● ") + nameStyle.Render(it.Name)
			if ports != "" {
				line += " " + dimStyle.Render(ports)
			}
			lines = append(lines, line)
		}
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	_ = width
	return lines
}

func openURL(url string) error {
	type candidate struct {
		bin  string
		args []string
	}
	var cands []candidate

	// $BROWSER: "firefox", "chromium %s", etc.
	if b := strings.TrimSpace(os.Getenv("BROWSER")); b != "" {
		if strings.Contains(b, "%s") {
			parts := strings.Fields(strings.ReplaceAll(b, "%s", url))
			if len(parts) > 0 {
				cands = append(cands, candidate{bin: parts[0], args: parts[1:]})
			}
		} else {
			parts := strings.Fields(b)
			cands = append(cands, candidate{bin: parts[0], args: append(parts[1:], url)})
		}
	}

	for _, bin := range []string{
		"xdg-open", "gio", "sensible-browser", "open",
		"chromium", "chromium-browser", "google-chrome-stable", "google-chrome",
		"firefox", "brave", "brave-browser", "microsoft-edge",
	} {
		args := []string{url}
		if bin == "gio" {
			args = []string{"open", url}
		}
		cands = append(cands, candidate{bin: bin, args: args})
	}

	env := browserEnv()
	if !hasDisplay(env) {
		return fmt.Errorf("no display (DISPLAY/WAYLAND_DISPLAY unset); open %s manually", url)
	}

	var lastErr error
	var tried []string
	for _, c := range cands {
		path, err := exec.LookPath(c.bin)
		if err != nil {
			continue
		}
		tried = append(tried, c.bin)

		stderr, err := os.CreateTemp("", "devctl-browser-*.log")
		if err != nil {
			lastErr = err
			continue
		}
		cmd := exec.Command(path, c.args...)
		cmd.Env = env
		cmd.Stdout = nil
		cmd.Stderr = stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			stderr.Close()
			os.Remove(stderr.Name())
			lastErr = err
			continue
		}

		// detect immediate failure (e.g. chromium missing X server)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			msg := readTrim(stderr.Name())
			stderr.Close()
			os.Remove(stderr.Name())
			if err != nil {
				lastErr = err
				if msg != "" {
					lastErr = fmt.Errorf("%s: %s", c.bin, firstLine(msg))
				}
				continue
			}
			// exited 0 quickly — still treat as attempt; try next only if error-like msg
			if strings.Contains(strings.ToLower(msg), "missing x server") ||
				strings.Contains(strings.ToLower(msg), "cannot open display") {
				lastErr = fmt.Errorf("%s: %s", c.bin, firstLine(msg))
				continue
			}
			return nil
		case <-time.After(400 * time.Millisecond):
			// still running — assume browser is up
			stderr.Close()
			os.Remove(stderr.Name())
			return nil
		}
	}
	if len(tried) == 0 {
		return fmt.Errorf("no browser found (set $BROWSER or install chromium/firefox/xdg-open)")
	}
	if lastErr != nil {
		return fmt.Errorf("open browser failed: %w", lastErr)
	}
	return fmt.Errorf("failed to open browser (tried %s)", strings.Join(tried, ", "))
}

func hasDisplay(env []string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, "DISPLAY=") && len(e) > len("DISPLAY=") {
			return true
		}
		if strings.HasPrefix(e, "WAYLAND_DISPLAY=") && len(e) > len("WAYLAND_DISPLAY=") {
			return true
		}
	}
	return false
}

// browserEnv returns os.Environ(), filling DISPLAY/WAYLAND_DISPLAY from
// other user processes when the current shell has none (e.g. plain TTY).
func browserEnv() []string {
	env := os.Environ()
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return env
	}
	disp, wayland, runtime := discoverDisplayEnv()
	if disp == "" && wayland == "" {
		return env
	}
	out := make([]string, 0, len(env)+3)
	for _, e := range env {
		if strings.HasPrefix(e, "DISPLAY=") ||
			strings.HasPrefix(e, "WAYLAND_DISPLAY=") ||
			strings.HasPrefix(e, "XDG_RUNTIME_DIR=") {
			continue
		}
		out = append(out, e)
	}
	if disp != "" {
		out = append(out, "DISPLAY="+disp)
	}
	if wayland != "" {
		out = append(out, "WAYLAND_DISPLAY="+wayland)
	}
	if runtime != "" {
		out = append(out, "XDG_RUNTIME_DIR="+runtime)
	} else if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		out = append(out, "XDG_RUNTIME_DIR="+v)
	}
	return out
}

func discoverDisplayEnv() (display, wayland, runtime string) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return "", "", ""
	}
	uid := fmt.Sprintf("%d", os.Getuid())
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		name := ent.Name()
		if name[0] < '1' || name[0] > '9' {
			continue
		}
		// only same user
		st, err := os.Stat("/proc/" + name)
		if err != nil {
			continue
		}
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			if fmt.Sprintf("%d", sys.Uid) != uid {
				continue
			}
		}
		data, err := os.ReadFile("/proc/" + name + "/environ")
		if err != nil {
			continue
		}
		for _, part := range strings.Split(string(data), "\x00") {
			if display == "" && strings.HasPrefix(part, "DISPLAY=") {
				display = strings.TrimPrefix(part, "DISPLAY=")
			}
			if wayland == "" && strings.HasPrefix(part, "WAYLAND_DISPLAY=") {
				wayland = strings.TrimPrefix(part, "WAYLAND_DISPLAY=")
			}
			if runtime == "" && strings.HasPrefix(part, "XDG_RUNTIME_DIR=") {
				runtime = strings.TrimPrefix(part, "XDG_RUNTIME_DIR=")
			}
		}
		if display != "" || wayland != "" {
			return display, wayland, runtime
		}
	}
	return display, wayland, runtime
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
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
