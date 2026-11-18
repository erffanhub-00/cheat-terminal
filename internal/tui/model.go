// Package tui is the interactive cheat-sheet browser: a search box on top,
// a category sidebar on the left, live results in the middle, and a detail
// pane on the right showing the selected command in full.
//
// The point of the browser is to find a command and use it: Enter copies
// the selected command to the clipboard, wherever you are. Keys:
//
//	Search mode (default): type to search, Up/Down move, Tab switch
//	  category, Enter copy, Esc clear/switch to list, ? help, Ctrl+C quit.
//	List mode (Esc from search): Up/Down or j/k move, Enter/c copy,
//	  s star or unstar a favorite, f filter by usefulness level,
//	  / back to search, q quit.
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/erffanhub-00/cheat-terminal/internal/clipboard"
	"github.com/erffanhub-00/cheat-terminal/internal/db"
)

type focus int

const (
	focusSearch focus = iota
	focusList
)

type sideKind int

const (
	sideFavorites sideKind = iota
	sideAll
	sideCategory
)

// sideItem is one row of the category sidebar.
type sideItem struct {
	kind  sideKind
	name  string // category name ("" for favorites/all)
	title string
	count int
}

// clearStatusMsg clears the status line after the copy toast expires.
type clearStatusMsg struct{ seq int }

// Model is the Bubbletea model for the browser.
type Model struct {
	store      *db.Store
	version    string // shown in the header, e.g. "1.1.0"
	input      textinput.Model
	sides      []sideItem
	activeSide int
	level      int // 0 = all levels, else db.LevelEssential/Common/Advanced
	total      int // total entries in the database
	results    []db.Entry
	cursor     int
	offset     int
	focus      focus
	showHelp   bool
	status     string
	statusErr  bool
	statusSeq  int
	width      int
	height     int
	err        error
}

// New builds the model and loads the first page of results.
func New(store *db.Store, version string) Model {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "Type to search..."
	ti.CharLimit = 120
	ti.Width = 60
	ti.Focus()
	m := Model{store: store, version: version, input: ti, focus: focusSearch, width: 80, height: 24}
	m.reload()
	m.activeSide = 1 // "All": Favorites is a filter, not the start view.
	m.search()
	return m
}

// Init starts the text-input cursor blink.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// reload rebuilds the sidebar (categories + counts) and the total.
func (m *Model) reload() {
	cats, err := m.store.ListCategories()
	if err != nil {
		m.err = err
		return
	}
	total, err := m.store.CountEntries()
	if err != nil {
		m.err = err
		return
	}
	favs, err := m.store.CountFavorites()
	if err != nil {
		m.err = err
		return
	}
	m.total = total
	m.sides = []sideItem{
		{kind: sideFavorites, name: "", title: "★ Favorites", count: favs},
		{kind: sideAll, name: "", title: "All", count: total},
	}
	for _, c := range cats {
		m.sides = append(m.sides, sideItem{kind: sideCategory, name: c.Name, title: c.Title, count: c.Count})
	}
	if m.activeSide >= len(m.sides) {
		m.activeSide = 0
	}
}

// activeCategoryName returns the category filter of the active sidebar row.
func (m *Model) activeCategoryName() string {
	if m.activeSide < 0 || m.activeSide >= len(m.sides) {
		return ""
	}
	if m.sides[m.activeSide].kind == sideCategory {
		return m.sides[m.activeSide].name
	}
	return ""
}

// activeIsFavorites reports whether the Favorites row is active.
func (m *Model) activeIsFavorites() bool {
	return m.activeSide >= 0 && m.activeSide < len(m.sides) &&
		m.sides[m.activeSide].kind == sideFavorites
}

// search refreshes the result list from the current query, sidebar row
// and usefulness-level filter.
func (m *Model) search() {
	var results []db.Entry
	var err error
	if m.activeIsFavorites() {
		results, err = m.searchFavorites()
	} else {
		results, err = m.store.Search(m.input.Value(), m.activeCategoryName(), m.level, 200)
	}
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

// searchFavorites filters the starred entries in memory: there are few
// of them, and it keeps one code path for the favorite pseudo-category.
func (m *Model) searchFavorites() ([]db.Entry, error) {
	all, err := m.store.ListFavorites(0)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	var out []db.Entry
	for _, e := range all {
		if m.level != 0 && e.Level != m.level {
			continue
		}
		if q == "" {
			out = append(out, e)
			continue
		}
		hay := strings.ToLower(e.Command + " " + e.Title + " " + e.Description + " " + e.Tags + " " + e.Example)
		if strings.Contains(hay, q) {
			out = append(out, e)
		}
	}
	return out, nil
}

// visibleRows is how many result rows fit in the list column.
func (m *Model) visibleRows() int {
	h := m.bodyHeight()
	if h < 3 {
		h = 3
	}
	return h
}

// bodyHeight is the height of the sidebar/list/detail row.
func (m *Model) bodyHeight() int {
	h := m.height - 6 // header 1, search box 3, footer 2
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

func (m *Model) cycleSide(d int) {
	n := len(m.sides)
	if n == 0 {
		return
	}
	m.activeSide = (m.activeSide + d + n) % n
	m.cursor, m.offset = 0, 0
	m.search()
}

func (m *Model) cycleLevel(d int) {
	// 0 All -> 1 Essential -> 2 Common -> 3 Advanced -> 0 ...
	m.level = (m.level + d + 4) % 4
	m.cursor, m.offset = 0, 0
	m.search()
}

// selected returns the highlighted entry, if any.
func (m *Model) selected() (db.Entry, bool) {
	if len(m.results) == 0 || m.cursor < 0 || m.cursor >= len(m.results) {
		return db.Entry{}, false
	}
	return m.results[m.cursor], true
}

// copySelected copies the highlighted command to the clipboard and shows
// a toast. Copying is the point of the browser, so it works in both modes.
func (m *Model) copySelected() tea.Cmd {
	e, ok := m.selected()
	if !ok {
		return nil
	}
	if err := clipboard.Copy(e.Command); err != nil {
		m.status = "Copy failed: " + err.Error()
		m.statusErr = true
		return nil
	}
	m.statusSeq++
	seq := m.statusSeq
	m.status = "✓ Copied: " + truncate(e.Command, 60)
	m.statusErr = false
	return tea.Tick(2500*time.Millisecond, func(time.Time) tea.Msg {
		return clearStatusMsg{seq: seq}
	})
}

// toggleFavorite stars or unstars the highlighted entry.
func (m *Model) toggleFavorite() {
	e, ok := m.selected()
	if !ok {
		return
	}
	_, err := m.store.SetFavorite(e.ID, !e.Favorite)
	if err != nil {
		m.status = "Favorite failed: " + err.Error()
		m.statusErr = true
		return
	}
	m.statusSeq++
	if e.Favorite {
		m.status = "Removed from favorites"
	} else {
		m.status = "★ Added to favorites"
	}
	m.statusErr = false
	m.reload()
	m.search()
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, msg.Width-8)
		m.ensureVisible()
		return m, nil

	case clearStatusMsg:
		if msg.seq == m.statusSeq {
			m.status = ""
			m.statusErr = false
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		}
		if m.showHelp {
			switch msg.String() {
			case "esc", "q":
				m.showHelp = false
			}
			return m, nil
		}
		switch m.focus {
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
		m.cycleSide(1)
		return m, nil
	case "shift+tab":
		m.cycleSide(-1)
		return m, nil
	case "enter":
		return m, m.copySelected()
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
		m.cycleSide(1)
		return m, nil
	case "shift+tab":
		m.cycleSide(-1)
		return m, nil
	case "enter", "c":
		return m, m.copySelected()
	case "s":
		m.toggleFavorite()
		return m, nil
	case "f":
		m.cycleLevel(1)
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

func truncate(s string, n int) string {
	if n < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func pad(s string, n int) string {
	if runeLen(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-runeLen(s))
}

func wrapText(s string, width int) []string {
	if width < 10 {
		width = 10
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	// Split words longer than the width, so the renderer never has to
	// re-wrap a line and grow the box taller than the screen.
	var tokens []string
	for _, w := range words {
		for runeLen(w) > width {
			r := []rune(w)
			tokens = append(tokens, string(r[:width]))
			w = string(r[width:])
		}
		tokens = append(tokens, w)
	}
	var lines []string
	cur := tokens[0]
	for _, w := range tokens[1:] {
		if runeLen(cur)+1+runeLen(w) > width {
			lines = append(lines, cur)
			cur = w
		} else {
			cur += " " + w
		}
	}
	return append(lines, cur)
}
