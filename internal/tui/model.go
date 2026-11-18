// Package tui is the interactive cheat-sheet browser: a search box on top,
// a category sidebar as tabs, live results, and a detail view.
//
// Keys (list): type to search, Up/Down move, Tab switch category,
// Enter details, c copy command, q quit, Esc back to search, Ctrl+C quit.
// Keys (detail): c copy command, Esc/Enter/q back, Ctrl+C quit.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/erffanhub-00/cheat-terminal/internal/clipboard"
	"github.com/erffanhub-00/cheat-terminal/internal/db"
)

type focus int

const (
	focusSearch focus = iota
	focusList
	focusDetail
)

// Model is the Bubbletea model for the browser.
type Model struct {
	store      *db.Store
	input      textinput.Model
	categories []db.Category // without "All"
	activeCat  int           // 0 = All, otherwise index+1 into categories
	results    []db.Entry
	cursor     int
	offset     int
	focus      focus
	detail     db.Entry
	detailOff  int
	status     string
	statusErr  bool
	width      int
	height     int
	err        error
}

var (
	tabActiveStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("63"))
	tabInactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	selectedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("63")).Bold(true)
	dimStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	titleStyle       = lipgloss.NewStyle().Bold(true)
	okStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	errStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// New builds the model and loads the first page of results.
func New(store *db.Store) Model {
	ti := textinput.New()
	ti.Placeholder = "Type to search..."
	ti.CharLimit = 120
	ti.Width = 60
	ti.Focus()
	m := Model{store: store, input: ti, focus: focusSearch, width: 80, height: 24}
	m.reloadCategories()
	m.search()
	return m
}

// Init starts the text-input cursor blink.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *Model) reloadCategories() {
	cats, err := m.store.ListCategories()
	if err != nil {
		m.err = err
		return
	}
	m.categories = cats
}

func (m *Model) activeCategoryName() string {
	if m.activeCat == 0 || m.activeCat-1 >= len(m.categories) {
		return ""
	}
	return m.categories[m.activeCat-1].Name
}

func (m *Model) search() {
	results, err := m.store.Search(m.input.Value(), m.activeCategoryName(), 200)
	if err != nil {
		m.err = err
		m.results = nil
		return
	}
	m.err = nil
	m.results = results
	if m.cursor >= len(m.results) {
		m.cursor = 0
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.ensureVisible()
}

func (m *Model) visibleRows() int {
	h := m.height - 5 // search row, tabs row, divider, footer (2 lines)
	if h < 3 {
		h = 3
	}
	return h
}

func (m *Model) ensureVisible() {
	visible := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *Model) moveCursor(d int) {
	if len(m.results) == 0 {
		return
	}
	m.cursor += d
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.results) {
		m.cursor = len(m.results) - 1
	}
	m.ensureVisible()
}

func (m *Model) cycleCategory(d int) {
	n := len(m.categories) + 1 // +1 for "All"
	m.activeCat = (m.activeCat + d + n) % n
	m.cursor, m.offset = 0, 0
	m.search()
}

func (m *Model) openDetail() {
	if len(m.results) == 0 {
		return
	}
	m.detail = m.results[m.cursor]
	m.detailOff = 0
	m.focus = focusDetail
}

func (m *Model) copyCommand(cmd string) {
	if err := clipboard.Copy(cmd); err != nil {
		m.status = "Copy failed: " + err.Error()
		m.statusErr = true
		return
	}
	m.status = "Copied: " + truncate(cmd, 60)
	m.statusErr = false
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, msg.Width-12)
		m.ensureVisible()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
		switch m.focus {
		case focusDetail:
			return m.updateDetail(msg)
		case focusList:
			return m.updateList(msg)
		default:
			return m.updateSearch(msg)
		}
	}
	return m, nil
}

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		m.moveCursor(-1)
		return m, nil
	case "down":
		m.moveCursor(1)
		return m, nil
	case "pgup":
		m.moveCursor(-m.visibleRows())
		return m, nil
	case "pgdown":
		m.moveCursor(m.visibleRows())
		return m, nil
	case "tab":
		m.cycleCategory(1)
		return m, nil
	case "shift+tab":
		m.cycleCategory(-1)
		return m, nil
	case "enter":
		m.openDetail()
		return m, nil
	case "esc":
		if m.input.Value() != "" {
			m.input.SetValue("")
			m.cursor, m.offset = 0, 0
			m.search()
			return m, nil
		}
		m.focus = focusList
		m.input.Blur()
		return m, nil
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.cursor, m.offset = 0, 0
		m.status = ""
		m.search()
	}
	return m, cmd
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(1)
		return m, nil
	case "pgup":
		m.moveCursor(-m.visibleRows())
		return m, nil
	case "pgdown":
		m.moveCursor(m.visibleRows())
		return m, nil
	case "tab":
		m.cycleCategory(1)
		return m, nil
	case "shift+tab":
		m.cycleCategory(-1)
		return m, nil
	case "enter":
		m.openDetail()
		return m, nil
	case "c":
		if len(m.results) > 0 {
			m.copyCommand(m.results[m.cursor].Command)
		}
		return m, nil
	case "q":
		return m, tea.Quit
	case "/":
		m.focus = focusSearch
		m.input.Focus()
		return m, textinput.Blink
	case "esc":
		m.focus = focusSearch
		m.input.Focus()
		return m, textinput.Blink
	}
	// Any other printable key jumps back to search and types it.
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		m.focus = focusSearch
		m.input.Focus()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.cursor, m.offset = 0, 0
		m.search()
		return m, cmd
	}
	return m, nil
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "c":
		m.copyCommand(m.detail.Command)
		return m, nil
	case "q":
		return m, tea.Quit
	case "esc", "enter":
		m.focus = focusList
		return m, nil
	case "up", "k":
		if m.detailOff > 0 {
			m.detailOff--
		}
		return m, nil
	case "down", "j":
		m.detailOff++
		return m, nil
	}
	return m, nil
}

// View renders the whole screen.
func (m Model) View() string {
	if m.err != nil {
		return errStyle.Render("Error: "+m.err.Error()) + "\nPress Ctrl+C to quit."
	}
	if m.focus == focusDetail {
		return m.viewDetail()
	}
	var b strings.Builder
	// Search row.
	b.WriteString("Search: " + m.input.View() + "\n")
	// Category tabs.
	b.WriteString(m.viewTabs() + "\n")
	b.WriteString(strings.Repeat("─", max(1, m.width-1)) + "\n")
	// Results.
	if len(m.results) == 0 {
		b.WriteString(dimStyle.Render("No matches.") + "\n")
	} else {
		visible := m.visibleRows()
		end := m.offset + visible
		if end > len(m.results) {
			end = len(m.results)
		}
		for i := m.offset; i < end; i++ {
			b.WriteString(m.viewRow(m.results[i], i == m.cursor) + "\n")
		}
	}
	// Footer.
	b.WriteString(m.viewFooter())
	return b.String()
}

func (m Model) viewTabs() string {
	names := []string{"All"}
	for _, c := range m.categories {
		names = append(names, c.Title)
	}
	var parts []string
	for i, name := range names {
		label := " " + name + " "
		if i == m.activeCat {
			parts = append(parts, tabActiveStyle.Render(label))
		} else {
			parts = append(parts, tabInactiveStyle.Render(label))
		}
	}
	return strings.Join(parts, " ")
}

func (m Model) viewRow(e db.Entry, selected bool) string {
	text := e.Command
	if strings.TrimSpace(e.Description) != "" {
		text += "  —  " + e.Description
	}
	text = truncate(text, max(10, m.width-2))
	if selected {
		return selectedStyle.Render(pad(text, m.width-1))
	}
	return " " + text
}

func (m Model) viewFooter() string {
	var help string
	if m.focus == focusList {
		help = "↑↓ move · Tab category · Enter details · c copy · q quit · / search"
	} else {
		help = "Type to search · ↑↓ move · Tab category · Enter details · Esc list · Ctrl+C quit"
	}
	count := fmt.Sprintf("%d entries", len(m.results))
	if m.activeCategoryName() != "" {
		count += " in " + m.activeCategoryName()
	}
	footer := dimStyle.Render(help) + "\n" + dimStyle.Render(count)
	if m.status != "" {
		if m.statusErr {
			footer += "\n" + errStyle.Render(m.status)
		} else {
			footer += "\n" + okStyle.Render(m.status)
		}
	}
	return footer
}

func (m Model) viewDetail() string {
	w := max(20, m.width-4)
	var lines []string
	lines = append(lines, titleStyle.Render("Command:"))
	lines = append(lines, wrapText(m.detail.Command, w)...)
	lines = append(lines, "")
	if strings.TrimSpace(m.detail.Description) != "" {
		lines = append(lines, titleStyle.Render("Description:"))
		lines = append(lines, wrapText(m.detail.Description, w)...)
		lines = append(lines, "")
	}
	if strings.TrimSpace(m.detail.Example) != "" {
		lines = append(lines, titleStyle.Render("Example:"))
		lines = append(lines, wrapText(m.detail.Example, w)...)
		lines = append(lines, "")
	}
	if strings.TrimSpace(m.detail.Tags) != "" {
		lines = append(lines, dimStyle.Render("Tags: "+m.detail.Tags))
	}
	lines = append(lines, dimStyle.Render(fmt.Sprintf("Category: %s · #%d · %s",
		m.detail.CategoryTitle, m.detail.ID, m.detail.Source)))
	// Scroll if taller than the screen.
	avail := m.height - 2
	if avail < 3 {
		avail = 3
	}
	maxOff := len(lines) - avail
	if maxOff < 0 {
		maxOff = 0
	}
	if m.detailOff > maxOff {
		m.detailOff = maxOff
	}
	if m.detailOff < 0 {
		m.detailOff = 0
	}
	end := m.detailOff + avail
	if end > len(lines) {
		end = len(lines)
	}
	var b strings.Builder
	for _, ln := range lines[m.detailOff:end] {
		b.WriteString(ln + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("c copy · Esc back · q quit"))
	return b.String()
}

func truncate(s string, n int) string {
	if n < 1 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func wrapText(s string, width int) []string {
	if width < 10 {
		width = 10
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := words[0]
	for _, w := range words[1:] {
		if len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
		} else {
			cur += " " + w
		}
	}
	return append(lines, cur)
}
