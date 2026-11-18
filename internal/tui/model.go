// Package tui is the interactive cheat-sheet browser: a search box on top,
// a category sidebar on the left, live results in the middle, and a detail
// pane on the right showing the selected command in full.
//
// The point of the browser is to find a command and use it: Enter copies
// the selected command to the clipboard, wherever you are. Keys:
//
//	Search mode (default): type to search, Up/Down move, Tab switch
//	  category, Enter copy, Esc clear/switch to list, ? help, Ctrl+C quit.
//	  Keys only work after Esc switches to list mode: in search mode
//	  typing goes into the search box.
//	List mode (Esc from search): Up/Down or j/k move, Enter/c copy,
//	  s star or unstar a favorite, f filter by usefulness level,
//	  a add an entry, C add a category, d delete an entry,
//	  E export entries to a file, I import entries from a file,
//	  / back to search, q quit.
//	Prompts (add entry, add category, delete, export, import): Enter
//	  confirms each step, Esc cancels, y/n answers the delete question.
package tui

import (
	"fmt"
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

// promptKind is the overlay prompt currently open, if any. Only one
// prompt can be open at a time; while one is open it owns the keyboard.
type promptKind int

const (
	promptNone     promptKind = iota
	promptAdd                 // sequential add-entry form, one field per step
	promptCategory            // single-field add-category prompt
	promptDelete              // y/n delete confirmation
	promptExport              // single-field export filename prompt
	promptImport              // two-step import: file path, then category
)

// addSteps is the number of steps in the add-entry form.
const addSteps = 5

// addStepLabels names the add-entry form steps, in order.
var addStepLabels = []string{"Command", "Description", "Category", "Tags", "Level"}

// addStepHints is the short hint shown under each add-entry step.
var addStepHints = []string{
	"The command itself, e.g. docker ps -a",
	"What it does, in a few words (optional)",
	"An existing category, or a new name to create it",
	"Comma separated, e.g. containers, list (optional)",
	"1 Essential, 2 Common or 3 Advanced (empty = Common)",
}

// clearStatusMsg clears the status line after the copy toast expires.
type clearStatusMsg struct{ seq int }

// Model is the Bubbletea model for the browser.
type Model struct {
	store      *db.Store
	version    string // shown in the header, e.g. "1.2.0"
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
	// Prompt state: the overlay form currently open, if any.
	prompt promptKind
	// formInput is the text field of the add-entry and add-category
	// prompts. It is separate from the search input, which keeps its
	// value while a prompt is open.
	formInput textinput.Model
	// addStep is the current step of the add-entry form (0..addSteps-1).
	addStep int
	// addCmd, addDesc, addCat and addTags hold the values collected so
	// far by the add-entry form.
	addCmd  string
	addDesc string
	addCat  string
	addTags string
	// formErr is an inline error shown inside a prompt (e.g. a bad
	// level, an empty file path), without closing the prompt.
	formErr string
	// importStep is the current step of the import prompt (0 = file
	// path, 1 = target category), and importFile holds the path typed
	// in step 0.
	importStep int
	importFile string
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
		// The limit is well above the largest category (Linux, 513),
		// so every entry stays reachable: with a smaller cap, entries
		// sorting late in a big category (Docker has 214) would be
		// cut off and could never be selected.
		results, err = m.store.Search(m.input.Value(), m.activeCategoryName(), m.level, 1000)
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

// rowHeight is how many terminal lines one result takes: one content
// line (the command) plus a blank line between rows, so the list
// breathes instead of packing every command together. The description
// is not shown here; it lives in the detail pane.
const rowHeight = 2

// visibleRows is how many result rows fit in the list column.
func (m *Model) visibleRows() int {
	v := m.bodyHeight() / rowHeight
	if v < 1 {
		v = 1
	}
	return v
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

// toast shows a status line that clears itself after 2.5 seconds, the
// same way the copy toast does. It returns the tick command, if any.
func (m *Model) toast(msg string, isErr bool) tea.Cmd {
	m.statusSeq++
	seq := m.statusSeq
	m.status = msg
	m.statusErr = isErr
	return tea.Tick(2500*time.Millisecond, func(time.Time) tea.Msg {
		return clearStatusMsg{seq: seq}
	})
}

// toggleFavorite stars or unstars the highlighted entry.
func (m *Model) toggleFavorite() tea.Cmd {
	e, ok := m.selected()
	if !ok {
		return nil
	}
	_, err := m.store.SetFavorite(e.ID, !e.Favorite)
	if err != nil {
		return m.toast("Favorite failed: "+err.Error(), true)
	}
	m.reload()
	m.search()
	if e.Favorite {
		return m.toast("Removed from favorites", false)
	}
	return m.toast("★ Added to favorites", false)
}

// newFormInput builds the text field used by the prompts.
func (m Model) newFormInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = placeholder
	ti.CharLimit = 200
	ti.Width = m.formWidth()
	ti.Focus()
	return ti
}

// formWidth is the text width of the prompt input, sized to the terminal.
func (m Model) formWidth() int {
	w := m.width - 12
	if w > 52 {
		w = 52
	}
	if w < 20 {
		w = 20
	}
	return w
}

// openAddForm starts the sequential add-entry form (list mode only).
func (m *Model) openAddForm() tea.Cmd {
	m.prompt = promptAdd
	m.addStep = 0
	m.addCmd, m.addDesc, m.addCat, m.addTags = "", "", "", ""
	m.formErr = ""
	m.formInput = m.newFormInput("Command, e.g. docker ps -a")
	m.status = ""
	return textinput.Blink
}

// openCategoryForm starts the single-field add-category prompt.
func (m *Model) openCategoryForm() tea.Cmd {
	m.prompt = promptCategory
	m.formErr = ""
	m.formInput = m.newFormInput("Category name, e.g. kubernetes")
	m.status = ""
	return textinput.Blink
}

// openDeleteConfirm asks whether to delete the highlighted entry.
func (m *Model) openDeleteConfirm() {
	if _, ok := m.selected(); !ok {
		return
	}
	m.prompt = promptDelete
	m.status = ""
}

// openExportForm starts the export filename prompt (list mode only).
func (m *Model) openExportForm() tea.Cmd {
	m.prompt = promptExport
	m.formErr = ""
	m.formInput = m.newFormInput("cheats-export.json")
	m.status = ""
	return textinput.Blink
}

// openImportForm starts the two-step import prompt (list mode only).
func (m *Model) openImportForm() tea.Cmd {
	m.prompt = promptImport
	m.importStep = 0
	m.importFile = ""
	m.formErr = ""
	m.formInput = m.newFormInput("File path, e.g. cheats-export.json")
	m.status = ""
	return textinput.Blink
}

// runExport writes all entries to the typed file (default name when the
// field is empty) and reports how many were written.
func (m *Model) runExport() tea.Cmd {
	path := strings.TrimSpace(m.formInput.Value())
	if path == "" {
		path = "cheats-export.json"
	}
	n, err := m.store.ExportToFile(path, "")
	m.cancelPrompt()
	if err != nil {
		return m.toast("Export failed: "+err.Error(), true)
	}
	return m.toast(fmt.Sprintf("Exported %d entries to %s", n, path), false)
}

// advanceImport moves the import prompt one step forward, running the
// import on the last step.
func (m *Model) advanceImport() tea.Cmd {
	v := strings.TrimSpace(m.formInput.Value())
	if m.importStep == 0 {
		if v == "" {
			m.formErr = "File path is empty"
			return nil
		}
		m.importFile = v
		m.importStep = 1
		m.formErr = ""
		m.formInput = m.newFormInput("Category (empty = use the file's categories)")
		return textinput.Blink
	}
	// Step 1: the target category. Empty means the file's categories.
	cat := normalizeCategoryName(v)
	stats, err := m.store.ImportFile(m.importFile, cat)
	m.cancelPrompt()
	if err != nil {
		return m.toast("Import failed: "+err.Error(), true)
	}
	// The import may have added categories: clear the search, rebuild
	// the sidebar and show the new entries.
	m.input.SetValue("")
	m.reload()
	m.search()
	return m.toast(fmt.Sprintf("Imported %d new, %d updated, %d unchanged",
		stats.New, stats.Updated, stats.Unchanged), false)
}

// cancelPrompt closes any open prompt and discards what it collected.
func (m *Model) cancelPrompt() {
	m.prompt = promptNone
	m.formErr = ""
	m.importStep = 0
	m.importFile = ""
	m.formInput.Blur()
}

// parseLevelInput accepts the level spellings the add form allows:
// 1/2/3, e/c/a, or the full names, case-insensitive. "" means Common.
func parseLevelInput(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "2", "c", "common":
		return db.LevelCommon, nil
	case "1", "e", "essential":
		return db.LevelEssential, nil
	case "3", "a", "advanced":
		return db.LevelAdvanced, nil
	}
	return 0, errBadLevel
}

var errBadLevel = errString("use 1, 2, 3 or essential / common / advanced")

type errString string

func (e errString) Error() string { return string(e) }

// titleCaseName turns a category name into a display title: first letter
// upper-case, the rest as typed ("kubernetes" -> "Kubernetes").
func titleCaseName(name string) string {
	if name == "" {
		return ""
	}
	r := []rune(name)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

// normalizeCategoryName lower-cases a typed category name so "Linux"
// matches the existing "linux" category instead of creating a duplicate.
func normalizeCategoryName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// selectCategoryByName moves the sidebar to the named category and
// refreshes the results. It reports whether the category was found.
func (m *Model) selectCategoryByName(name string) bool {
	for i, s := range m.sides {
		if s.kind == sideCategory && s.name == name {
			m.activeSide = i
			m.cursor, m.offset = 0, 0
			m.search()
			return true
		}
	}
	return false
}

// selectEntryByID moves the cursor to the entry with the given id, if it
// is in the current result list.
func (m *Model) selectEntryByID(id int64) {
	for i, e := range m.results {
		if e.ID == id {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
}

// advanceAdd moves the add-entry form one step forward, saving on the
// last step. Enter on an empty command cancels without saving.
func (m *Model) advanceAdd() tea.Cmd {
	v := strings.TrimSpace(m.formInput.Value())
	switch m.addStep {
	case 0: // Command (required).
		if v == "" {
			m.cancelPrompt()
			return m.toast("Command is empty", true)
		}
		m.addCmd = v
		m.addStep = 1
		m.formErr = ""
		m.formInput = m.newFormInput("Description (optional)")
	case 1: // Description (optional).
		m.addDesc = v
		m.addStep = 2
		m.formErr = ""
		m.formInput = m.newFormInput("Linux, Docker, Git, PowerShell or a new name")
	case 2: // Category (existing name, or a new one to create).
		m.addCat = normalizeCategoryName(v)
		m.addStep = 3
		m.formErr = ""
		m.formInput = m.newFormInput("Tags, comma separated (optional)")
	case 3: // Tags (optional).
		m.addTags = v
		m.addStep = 4
		m.formErr = ""
		m.formInput = m.newFormInput("Level: 1 Essential, 2 Common, 3 Advanced")
	default: // Level, then save.
		level, err := parseLevelInput(v)
		if err != nil {
			m.formErr = err.Error()
			return nil
		}
		return m.saveAdd(level)
	}
	return textinput.Blink
}

// saveAdd inserts the collected add-form values as a user entry, then
// returns to the list with the new entry selected.
func (m *Model) saveAdd(level int) tea.Cmd {
	name := m.addCat
	if name == "" {
		name = "general"
	}
	id, err := m.store.AddEntry(db.Entry{
		Command:       m.addCmd,
		Title:         m.addCmd,
		Description:   m.addDesc,
		Tags:          m.addTags,
		Category:      name,
		CategoryTitle: titleCaseName(name),
		Level:         level,
	})
	if err != nil {
		m.cancelPrompt()
		return m.toast("Add failed: "+err.Error(), true)
	}
	title := titleCaseName(name)
	m.cancelPrompt()
	// The new entry must be visible: clear the search, switch to its
	// category, and put the cursor on it.
	m.input.SetValue("")
	m.reload()
	m.selectCategoryByName(name)
	m.selectEntryByID(id)
	return m.toast("✓ Added to "+title, false)
}

// saveCategory creates the typed category, or reports that it exists.
func (m *Model) saveCategory() tea.Cmd {
	name := normalizeCategoryName(m.formInput.Value())
	if name == "" {
		m.cancelPrompt()
		return m.toast("Category name is empty", true)
	}
	_, existed, err := m.store.AddCategory(name, titleCaseName(name))
	if err != nil {
		m.cancelPrompt()
		return m.toast("Add category failed: "+err.Error(), true)
	}
	title := titleCaseName(name)
	m.cancelPrompt()
	m.input.SetValue("")
	m.reload()
	if existed {
		m.selectCategoryByName(name)
		return m.toast("Category already exists", true)
	}
	m.selectCategoryByName(name)
	return m.toast("✓ Added category "+title, false)
}

// deleteSelected deletes the highlighted entry after confirmation.
func (m *Model) deleteSelected() tea.Cmd {
	e, ok := m.selected()
	if !ok {
		m.cancelPrompt()
		return nil
	}
	if _, err := m.store.DeleteEntry(e.ID); err != nil {
		m.cancelPrompt()
		return m.toast("Delete failed: "+err.Error(), true)
	}
	m.cancelPrompt()
	m.reload()
	m.search()
	return m.toast("✓ Deleted", false)
}

// updatePrompt handles keys while a prompt overlay is open. It owns the
// keyboard: typing goes to the prompt's text field, Enter advances or
// confirms, Esc cancels, and y/n answers the delete question.
func (m Model) updatePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.prompt {
	case promptDelete:
		switch msg.String() {
		case "y", "Y":
			return m, m.deleteSelected()
		case "n", "N", "esc":
			m.cancelPrompt()
			return m, nil
		}
		return m, nil
	case promptAdd:
		switch msg.String() {
		case "esc":
			m.cancelPrompt()
			return m, nil
		case "enter":
			return m, m.advanceAdd()
		}
	case promptCategory:
		switch msg.String() {
		case "esc":
			m.cancelPrompt()
			return m, nil
		case "enter":
			return m, m.saveCategory()
		}
	case promptExport:
		switch msg.String() {
		case "esc":
			m.cancelPrompt()
			return m, nil
		case "enter":
			return m, m.runExport()
		}
	case promptImport:
		switch msg.String() {
		case "esc":
			m.cancelPrompt()
			return m, nil
		case "enter":
			return m, m.advanceImport()
		}
	}
	var cmd tea.Cmd
	m.formInput, cmd = m.formInput.Update(msg)
	return m, cmd
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, msg.Width-8)
		m.formInput.Width = m.formWidth()
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
		}
		// An open prompt owns the keyboard (except Ctrl+C).
		if m.prompt != promptNone {
			return m.updatePrompt(msg)
		}
		switch msg.String() {
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
		return m, m.toggleFavorite()
	case "f":
		m.cycleLevel(1)
		return m, nil
	case "a":
		return m, m.openAddForm()
	case "C":
		return m, m.openCategoryForm()
	case "d":
		m.openDeleteConfirm()
		return m, nil
	case "E":
		return m, m.openExportForm()
	case "I":
		return m, m.openImportForm()
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
