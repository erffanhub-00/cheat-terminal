package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/erffanhub-00/cheat-terminal/data"
	"github.com/erffanhub-00/cheat-terminal/internal/db"
)

func openSeededStore(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	return s
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	model, ok := next.(Model)
	if !ok {
		panic("Update did not return a Model")
	}
	return model
}

// toList switches to list focus (Esc with an empty query).
func toList(m Model) Model {
	return update(m, keyMsg("esc"))
}

func TestTUITypeToSearch(t *testing.T) {
	m := New(openSeededStore(t), "test")
	if len(m.results) == 0 {
		t.Fatalf("expected seeded results on start")
	}
	if m.activeSide != 1 || m.activeCategoryName() != "" {
		t.Fatalf("start view should be All, got side %d", m.activeSide)
	}
	for _, ch := range "docker" {
		m = update(m, keyMsg(string(ch)))
	}
	if len(m.results) == 0 {
		t.Fatalf("expected results for 'docker'")
	}
	for _, e := range m.results {
		if e.Category != "docker" {
			t.Fatalf("typed search leaked category: %q", e.Category)
		}
	}
	if !strings.Contains(m.View(), "docker") {
		t.Fatalf("view does not show docker results")
	}
}

func TestTUISidebarAndDetailPane(t *testing.T) {
	m := New(openSeededStore(t), "test")
	// A wide terminal shows all three panes (narrow ones hide the detail pane).
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	// The view is a real three-pane layout now, not bare rows.
	view := m.View()
	for _, want := range []string{"cheat-terminal", "Categories", "Favorites", "All", "Docker", "Details"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q", want)
		}
	}
	// One Tab moves from All to Docker (categories are ordered by name).
	m = update(m, keyMsg("tab"))
	if m.activeCategoryName() != "docker" {
		t.Fatalf("after one Tab, category = %q, want docker", m.activeCategoryName())
	}
	if len(m.results) == 0 {
		t.Fatalf("expected docker results after Tab")
	}
	// The detail pane shows the selected command in full.
	if e, ok := m.selected(); ok {
		if !strings.Contains(m.View(), e.Command) {
			t.Fatalf("detail pane does not show the selected command")
		}
	} else {
		t.Fatalf("no selection")
	}
}

func TestTUIEnterCopies(t *testing.T) {
	m := New(openSeededStore(t), "test")
	for _, ch := range "docker ps" {
		m = update(m, keyMsg(string(ch)))
	}
	if len(m.results) == 0 {
		t.Fatalf("expected results for 'docker ps'")
	}
	// Enter copies in search mode (the primary action) and reports it.
	m = update(m, keyMsg("enter"))
	if m.status == "" {
		t.Fatalf("Enter did not set a copy status")
	}
	if !strings.Contains(m.status, "Copied") && !strings.Contains(m.status, "Copy failed") {
		t.Fatalf("unexpected copy status: %q", m.status)
	}
	// The toast clears on its tick.
	m = update(m, clearStatusMsg{seq: m.statusSeq})
	if m.status != "" {
		t.Fatalf("copy toast did not clear: %q", m.status)
	}
}

func TestTUILevelFilter(t *testing.T) {
	m := New(openSeededStore(t), "test")
	m = toList(m)
	if m.focus != focusList {
		t.Fatalf("Esc did not switch to list mode")
	}
	// f cycles All -> Essential -> Common -> Advanced -> All.
	for _, want := range []int{db.LevelEssential, db.LevelCommon, db.LevelAdvanced, db.LevelAll} {
		m = update(m, keyMsg("f"))
		if m.level != want {
			t.Fatalf("after f, level = %d, want %d", m.level, want)
		}
		if want != db.LevelAll {
			for _, e := range m.results {
				if e.Level != want {
					t.Fatalf("level filter leaked: entry level %d in level %d", e.Level, want)
				}
			}
		}
		if len(m.results) == 0 {
			t.Fatalf("no results at level %d", want)
		}
	}
	// The header chip shows the active filter.
	m = update(m, keyMsg("f")) // -> Essential
	if !strings.Contains(m.View(), "Essential") {
		t.Fatalf("view does not show the Essential chip")
	}
}

func TestTUIFavorites(t *testing.T) {
	s := openSeededStore(t)
	m := New(s, "test")
	m = toList(m)
	// Star the first entry.
	m = update(m, keyMsg("s"))
	if !strings.Contains(m.status, "favorites") {
		t.Fatalf("star did not report: %q", m.status)
	}
	if n, _ := s.CountFavorites(); n != 1 {
		t.Fatalf("favorites = %d, want 1", n)
	}
	// Shift+Tab from All goes to the Favorites row.
	m = update(m, keyMsg("shift+tab"))
	if !m.activeIsFavorites() {
		t.Fatalf("expected Favorites row, got side %d", m.activeSide)
	}
	if len(m.results) != 1 || !m.results[0].Favorite {
		t.Fatalf("favorites view wrong: %+v", m.results)
	}
	// Unstar from the Favorites view; the row empties.
	m = update(m, keyMsg("s"))
	if len(m.results) != 0 {
		t.Fatalf("favorites view not empty after unstar: %d", len(m.results))
	}
	if n, _ := s.CountFavorites(); n != 0 {
		t.Fatalf("favorites = %d after unstar, want 0", n)
	}
}

func TestTUIHelp(t *testing.T) {
	m := New(openSeededStore(t), "test")
	m = update(m, keyMsg("?"))
	if !m.showHelp {
		t.Fatalf("? did not open help")
	}
	if !strings.Contains(m.View(), "Keyboard shortcuts") {
		t.Fatalf("help view missing")
	}
	m = update(m, keyMsg("esc"))
	if m.showHelp {
		t.Fatalf("Esc did not close help")
	}
}

func TestTUIEscClearsQuery(t *testing.T) {
	m := New(openSeededStore(t), "test")
	for _, ch := range "git" {
		m = update(m, keyMsg(string(ch)))
	}
	if m.input.Value() != "git" {
		t.Fatalf("input = %q, want git", m.input.Value())
	}
	m = update(m, keyMsg("esc"))
	if m.input.Value() != "" {
		t.Fatalf("Esc did not clear the query: %q", m.input.Value())
	}
	if len(m.results) == 0 {
		t.Fatalf("expected all entries after clearing the query")
	}
}

// typeText types every rune of s into the model, one key message each.
func typeText(m Model, s string) Model {
	for _, ch := range s {
		m = update(m, keyMsg(string(ch)))
	}
	return m
}

func TestTUIAddEntryFlow(t *testing.T) {
	s := openSeededStore(t)
	m := New(s, "test")
	m = toList(m)
	// 'a' opens the add-entry form at step 1 (Command).
	m = update(m, keyMsg("a"))
	if m.prompt != promptAdd || m.addStep != 0 {
		t.Fatalf("a did not open the add form: prompt %d step %d", m.prompt, m.addStep)
	}
	if !strings.Contains(m.View(), "Add entry") {
		t.Fatalf("add form view missing")
	}
	// Empty command: Enter cancels without saving.
	m = update(m, keyMsg("enter"))
	if m.prompt != promptNone {
		t.Fatalf("empty command did not cancel the form")
	}
	if !strings.Contains(m.status, "Command is empty") {
		t.Fatalf("empty command status = %q, want 'Command is empty'", m.status)
	}
	if n, _ := s.CountEntries(); n != 1392 {
		t.Fatalf("empty command added an entry: total %d", n)
	}
	// Full flow: command, description, NEW category, tags, level 1.
	m = update(m, keyMsg("a"))
	m = typeText(m, "my test cmd")
	m = update(m, keyMsg("enter"))
	if m.addStep != 1 {
		t.Fatalf("after command, step = %d, want 1", m.addStep)
	}
	m = typeText(m, "a command I added myself")
	m = update(m, keyMsg("enter"))
	if m.addStep != 2 {
		t.Fatalf("after description, step = %d, want 2", m.addStep)
	}
	if !strings.Contains(m.View(), "Existing:") {
		t.Fatalf("category step does not list existing categories")
	}
	m = typeText(m, "kubernetes")
	m = update(m, keyMsg("enter"))
	if m.addStep != 3 {
		t.Fatalf("after category, step = %d, want 3", m.addStep)
	}
	m = typeText(m, "test, demo")
	m = update(m, keyMsg("enter"))
	if m.addStep != 4 {
		t.Fatalf("after tags, step = %d, want 4", m.addStep)
	}
	// A bad level stays on the step with an inline error.
	m = typeText(m, "bogus")
	m = update(m, keyMsg("enter"))
	if m.prompt != promptAdd || m.addStep != 4 {
		t.Fatalf("bad level left the form: prompt %d step %d", m.prompt, m.addStep)
	}
	if m.formErr == "" {
		t.Fatalf("bad level did not set an inline error")
	}
	m = update(m, keyMsg("enter")) // empty now? No: input still has "bogus".
	if m.prompt != promptAdd {
		t.Fatalf("bad level was accepted on retry")
	}
	// Clear the bad value and enter a valid level.
	for i := 0; i < len("bogus"); i++ {
		m = update(m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m = typeText(m, "1")
	m = update(m, keyMsg("enter"))
	if m.prompt != promptNone {
		t.Fatalf("form did not close after save")
	}
	if !strings.Contains(m.status, "Added to Kubernetes") {
		t.Fatalf("save status = %q, want 'Added to Kubernetes'", m.status)
	}
	// The row exists in the database, in the new category, at level 1.
	found, err := s.Search("my test cmd", "", 0, 0)
	if err != nil || len(found) != 1 {
		t.Fatalf("saved entry not found in db: %v (%d)", err, len(found))
	}
	if found[0].Category != "kubernetes" || found[0].Level != db.LevelEssential {
		t.Fatalf("saved entry wrong: %+v", found[0])
	}
	if found[0].Source != "user" {
		t.Fatalf("saved entry source = %q, want user", found[0].Source)
	}
	cats, _ := s.ListCategories()
	var kc *db.Category
	for i := range cats {
		if cats[i].Name == "kubernetes" {
			kc = &cats[i]
		}
	}
	if kc == nil || kc.Count != 1 {
		t.Fatalf("kubernetes category missing or wrong count: %+v", kc)
	}
	// The model shows the new category, with the new entry selected.
	if m.activeCategoryName() != "kubernetes" {
		t.Fatalf("after save, category = %q, want kubernetes", m.activeCategoryName())
	}
	if e, ok := m.selected(); !ok || e.Command != "my test cmd" {
		t.Fatalf("after save, selected = %+v", e)
	}
	// A second entry with an empty level defaults to Common (2).
	m = update(m, keyMsg("a"))
	m = typeText(m, "my second cmd")
	m = update(m, keyMsg("enter"))
	m = update(m, keyMsg("enter")) // empty description
	m = typeText(m, "docker")
	m = update(m, keyMsg("enter"))
	m = update(m, keyMsg("enter")) // empty tags
	m = update(m, keyMsg("enter")) // empty level -> Common
	if m.prompt != promptNone {
		t.Fatalf("second form did not close after save")
	}
	found, _ = s.Search("my second cmd", "", 0, 0)
	if len(found) != 1 || found[0].Level != db.LevelCommon {
		t.Fatalf("empty level did not default to Common: %+v", found)
	}
	// The new entry is selected in the model, even though it sorts
	// late in a big category (Docker has 214 entries): the result
	// limit must not cut it off.
	if m.activeCategoryName() != "docker" {
		t.Fatalf("after second save, category = %q, want docker", m.activeCategoryName())
	}
	if e, ok := m.selected(); !ok || e.Command != "my second cmd" {
		t.Fatalf("after second save, selected = %+v", e)
	}
	// Esc cancels the form at any step without saving.
	m = update(m, keyMsg("a"))
	m = typeText(m, "will not save")
	m = update(m, keyMsg("esc"))
	if m.prompt != promptNone {
		t.Fatalf("Esc did not cancel the add form")
	}
	if n, _ := s.CountEntries(); n != 1394 {
		t.Fatalf("cancelled add changed the total: %d", n)
	}
}

func TestTUIAddCategoryFlow(t *testing.T) {
	s := openSeededStore(t)
	m := New(s, "test")
	m = toList(m)
	// 'C' opens the category prompt; a new name creates the category
	// and selects it in the sidebar.
	m = update(m, keyMsg("C"))
	if m.prompt != promptCategory {
		t.Fatalf("C did not open the category prompt: %d", m.prompt)
	}
	m = typeText(m, "kubernetes")
	m = update(m, keyMsg("enter"))
	if m.prompt != promptNone {
		t.Fatalf("category prompt did not close after save")
	}
	if !strings.Contains(m.status, "Added category Kubernetes") {
		t.Fatalf("category status = %q", m.status)
	}
	cats, _ := s.ListCategories()
	found := false
	for _, c := range cats {
		if c.Name == "kubernetes" {
			found = true
		}
	}
	if !found {
		t.Fatalf("kubernetes not in categories after add")
	}
	if m.activeCategoryName() != "kubernetes" {
		t.Fatalf("after add, sidebar = %q, want kubernetes", m.activeCategoryName())
	}
	// The same name again (case-insensitive) reports a duplicate.
	m = update(m, keyMsg("C"))
	m = typeText(m, "Kubernetes")
	m = update(m, keyMsg("enter"))
	if !strings.Contains(m.status, "Category already exists") {
		t.Fatalf("duplicate status = %q, want 'Category already exists'", m.status)
	}
	// A built-in category is a duplicate too.
	m = update(m, keyMsg("C"))
	m = typeText(m, "docker")
	m = update(m, keyMsg("enter"))
	if !strings.Contains(m.status, "Category already exists") {
		t.Fatalf("docker duplicate status = %q", m.status)
	}
}

func TestTUIDeleteFlow(t *testing.T) {
	s := openSeededStore(t)
	m := New(s, "test")
	m = toList(m)
	e, ok := m.selected()
	if !ok {
		t.Fatalf("no selection")
	}
	// 'd' asks for confirmation.
	m = update(m, keyMsg("d"))
	if m.prompt != promptDelete {
		t.Fatalf("d did not open the delete confirm: %d", m.prompt)
	}
	if !strings.Contains(m.View(), "Delete this entry?") {
		t.Fatalf("delete confirm view missing")
	}
	// 'n' cancels: the entry stays.
	m = update(m, keyMsg("n"))
	if m.prompt != promptNone {
		t.Fatalf("n did not cancel the delete")
	}
	if _, err := s.GetEntry(e.ID); err != nil {
		t.Fatalf("entry gone after n: %v", err)
	}
	// Esc cancels too.
	m = update(m, keyMsg("d"))
	m = update(m, keyMsg("esc"))
	if m.prompt != promptNone {
		t.Fatalf("Esc did not cancel the delete")
	}
	if _, err := s.GetEntry(e.ID); err != nil {
		t.Fatalf("entry gone after Esc: %v", err)
	}
	// 'y' deletes it.
	m = update(m, keyMsg("d"))
	m = update(m, keyMsg("y"))
	if m.prompt != promptNone {
		t.Fatalf("delete confirm did not close after y")
	}
	if !strings.Contains(m.status, "Deleted") {
		t.Fatalf("delete status = %q", m.status)
	}
	if _, err := s.GetEntry(e.ID); err == nil {
		t.Fatalf("entry still in db after y")
	}
	if n, _ := s.CountEntries(); n != 1391 {
		t.Fatalf("total after delete = %d, want 1391", n)
	}
}

// firstRunes returns the first n runes of s.
func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]|\x1b\\][^\x07]*\x07|\x1b[()][0-9A-B]|\x1b.")

// stripANSI removes terminal escape sequences from s.
func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// listLines returns the rendered result-list lines (raw, with escapes).
func listLines(m Model) []string {
	_, _, listW := m.layout()
	return strings.Split(m.viewList(listW, m.bodyHeight()), "\n")
}

// strippedListLines returns the result-list lines with escapes removed.
func strippedListLines(m Model) []string {
	var out []string
	for _, ln := range listLines(m) {
		out = append(out, stripANSI(ln))
	}
	return out
}

// execCmd runs a command (and any batch it returns) and collects the
// messages it produces.
func execCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, execCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestTUISingleLineRows(t *testing.T) {
	m := New(openSeededStore(t), "test")
	// Single-line rows take 2 lines each (command + blank separator),
	// so more rows fit per page than the old two-line layout.
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	if v := m.visibleRows(); v != 12 {
		t.Fatalf("visible rows at 120x30 = %d, want 12", v)
	}
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if v := m.visibleRows(); v != 9 {
		t.Fatalf("visible rows at 80x24 = %d, want 9", v)
	}
	// Check the list column itself (the full view joins the sidebar
	// next to it, so its lines are never blank).
	_, _, listW := m.layout()
	lines := strings.Split(m.viewList(listW, m.bodyHeight()), "\n")
	// Find a visible entry with a description: its command line is
	// followed by a blank line, and its description is nowhere in the
	// list — it lives in the detail pane.
	checkedIdx := -1
	for i := 0; i < m.visibleRows() && i < len(m.results); i++ {
		e := m.results[i]
		if strings.TrimSpace(e.Description) == "" {
			continue
		}
		cmdIdx := -1
		for j := 0; j < len(lines); j++ {
			if strings.Contains(lines[j], e.Command) {
				cmdIdx = j
				break
			}
		}
		if cmdIdx < 0 {
			t.Fatalf("command %q not in list lines", e.Command)
		}
		if cmdIdx+1 >= len(lines) || lines[cmdIdx+1] != "" {
			t.Fatalf("no blank line after the row")
		}
		for _, ln := range lines {
			if strings.Contains(ln, firstRunes(e.Description, 20)) {
				t.Fatalf("description leaked into the list: %q", ln)
			}
		}
		checkedIdx = i
		break
	}
	if checkedIdx < 0 {
		t.Fatalf("no visible entry with a description to check")
	}
	// ...but the detail pane (wide terminal) still shows it.
	m.cursor = checkedIdx
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	e := m.results[checkedIdx]
	if !strings.Contains(m.View(), firstRunes(e.Description, 20)) {
		t.Fatalf("detail pane does not show the description")
	}
}

func TestTUIViewFitsTerminal(t *testing.T) {
	m := New(openSeededStore(t), "test")
	// The view must be exactly the terminal height at every size the
	// program supports, from a small 60x20 window to a large 160x40 one.
	for _, size := range [][2]int{{60, 20}, {80, 24}, {100, 30}, {120, 30}, {160, 40}} {
		m = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if n := len(strings.Split(m.View(), "\n")); n != size[1] {
			t.Fatalf("view lines = %d at %dx%d, want %d", n, size[0], size[1], size[1])
		}
	}
	// Moving the cursor must never change the height, at any size.
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	for i := 0; i < 60; i++ {
		m = update(m, keyMsg("down"))
		if n := len(strings.Split(m.View(), "\n")); n != 30 {
			t.Fatalf("view lines = %d at cursor %d, want 30", n, m.cursor)
		}
	}
	m = update(m, tea.WindowSizeMsg{Width: 60, Height: 20})
	for i := 0; i < 30; i++ {
		m = update(m, keyMsg("down"))
		if n := len(strings.Split(m.View(), "\n")); n != 20 {
			t.Fatalf("view lines = %d at 60x20 cursor %d, want 20", n, m.cursor)
		}
	}
	// Below 60 columns the sidebar is hidden and the header carries
	// the active category instead.
	m = update(m, tea.WindowSizeMsg{Width: 60, Height: 20})
	if strings.Contains(m.View(), "Categories") {
		t.Fatalf("sidebar shown below 60 columns")
	}
	if !strings.Contains(m.View(), "All · 1392 entries") {
		t.Fatalf("header does not show the category below 60 columns")
	}
	// Below 90 columns the detail pane is hidden; the sidebar stays.
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if strings.Contains(m.View(), "Details") {
		t.Fatalf("detail pane shown below 90 columns")
	}
	if !strings.Contains(m.View(), "Categories") {
		t.Fatalf("sidebar hidden at 80 columns")
	}
}

func TestTUISearchFooterEscNote(t *testing.T) {
	m := New(openSeededStore(t), "test")
	// Search mode (the default): the footer says keys need Esc first.
	if !strings.Contains(m.View(), "once → keys work") {
		t.Fatalf("search footer does not carry the Esc note")
	}
	// List mode: the note is gone, the full key list (with E/I) is there.
	m = toList(m)
	if strings.Contains(m.View(), "once → keys work") {
		t.Fatalf("list footer still carries the Esc note")
	}
	for _, want := range []string{"export", "import"} {
		if !strings.Contains(m.View(), want) {
			t.Fatalf("list footer missing %q", want)
		}
	}
	// The help overlay explains the two modes.
	m = update(m, keyMsg("?"))
	if !strings.Contains(m.View(), "MODES") {
		t.Fatalf("help does not explain the modes")
	}
	if !strings.Contains(m.View(), "Press Esc once") {
		t.Fatalf("help does not say Esc once switches to list mode")
	}
}

func TestTUIExportFlow(t *testing.T) {
	s := openSeededStore(t)
	m := New(s, "test")
	m = toList(m)
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	// 'E' opens the export prompt at step 1 (scope).
	m = update(m, keyMsg("E"))
	if m.prompt != promptExport || m.exportStep != 0 {
		t.Fatalf("E did not open the export prompt: %d step %d", m.prompt, m.exportStep)
	}
	if !strings.Contains(m.View(), "Export entries") {
		t.Fatalf("export prompt view missing")
	}
	if !strings.Contains(m.View(), "step 1 of 2") {
		t.Fatalf("export prompt does not say step 1 of 2")
	}
	if !strings.Contains(m.View(), "Existing:") {
		t.Fatalf("scope step does not list existing categories")
	}
	// An unknown category stays on step 1 with an inline error.
	m = typeText(m, "bogus")
	m = update(m, keyMsg("enter"))
	if m.prompt != promptExport || m.exportStep != 0 {
		t.Fatalf("unknown category left the scope step")
	}
	if !strings.Contains(m.formErr, "Unknown category") {
		t.Fatalf("unknown category error = %q", m.formErr)
	}
	// Esc cancels without writing anything.
	m = update(m, keyMsg("esc"))
	if m.prompt != promptNone {
		t.Fatalf("Esc did not cancel the export prompt")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cancelled export wrote a file")
	}
	// Empty scope means all: Enter advances to the filename step with
	// the all-entries default.
	m = update(m, keyMsg("E"))
	m = update(m, keyMsg("enter"))
	if m.prompt != promptExport || m.exportStep != 1 {
		t.Fatalf("after empty scope, step = %d, want 1", m.exportStep)
	}
	if m.formInput.Placeholder != "cheats-export.json" {
		t.Fatalf("filename placeholder = %q, want cheats-export.json", m.formInput.Placeholder)
	}
	// Esc cancels from step 2 as well.
	m = update(m, keyMsg("esc"))
	if m.prompt != promptNone {
		t.Fatalf("Esc did not cancel the export prompt from step 2")
	}
	// Type the path and export everything.
	m = update(m, keyMsg("E"))
	m = update(m, keyMsg("enter")) // empty scope -> all
	m = typeText(m, path)
	m = update(m, keyMsg("enter"))
	if m.prompt != promptNone {
		t.Fatalf("export prompt did not close after Enter")
	}
	if !strings.Contains(m.status, "Exported 1392 entries") {
		t.Fatalf("export status = %q", m.status)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("export file not written: %v", err)
	}
	var seed db.SeedFile
	if err := json.Unmarshal(raw, &seed); err != nil {
		t.Fatalf("export file is not valid JSON: %v", err)
	}
	if len(seed.Entries) != 1392 {
		t.Fatalf("export file has %d entries, want 1392", len(seed.Entries))
	}
	// Per-category export: scope "docker" suggests cheats-docker.json
	// and writes only that category.
	catPath := filepath.Join(dir, "docker.json")
	m = update(m, keyMsg("E"))
	m = typeText(m, "docker")
	m = update(m, keyMsg("enter"))
	if m.prompt != promptExport || m.exportStep != 1 {
		t.Fatalf("after docker scope, step = %d, want 1", m.exportStep)
	}
	if m.exportScope != "docker" {
		t.Fatalf("export scope = %q, want docker", m.exportScope)
	}
	if m.formInput.Placeholder != "cheats-docker.json" {
		t.Fatalf("filename placeholder = %q, want cheats-docker.json", m.formInput.Placeholder)
	}
	m = typeText(m, catPath)
	m = update(m, keyMsg("enter"))
	if m.prompt != promptNone {
		t.Fatalf("category export did not close the prompt")
	}
	if !strings.Contains(m.status, "Exported 214 entries from docker to ") {
		t.Fatalf("category export status = %q", m.status)
	}
	raw, err = os.ReadFile(catPath)
	if err != nil {
		t.Fatalf("category export file not written: %v", err)
	}
	if err := json.Unmarshal(raw, &seed); err != nil {
		t.Fatalf("category export file is not valid JSON: %v", err)
	}
	if len(seed.Entries) != 214 || len(seed.Categories) != 1 {
		t.Fatalf("category export has %d entries in %d categories, want 214 in 1",
			len(seed.Entries), len(seed.Categories))
	}
	// The scope "all" is the same as an empty scope.
	m = update(m, keyMsg("E"))
	m = typeText(m, "all")
	m = update(m, keyMsg("enter"))
	if m.exportScope != "" || m.formInput.Placeholder != "cheats-export.json" {
		t.Fatalf("scope all: scope %q placeholder %q", m.exportScope, m.formInput.Placeholder)
	}
}

func TestTUIExportDefaultFilename(t *testing.T) {
	if got := exportDefaultFilename(""); got != "cheats-export.json" {
		t.Fatalf("default for all = %q", got)
	}
	if got := exportDefaultFilename("docker"); got != "cheats-docker.json" {
		t.Fatalf("default for docker = %q", got)
	}
	if got := exportDefaultFilename("My Category"); got != "cheats-my-category.json" {
		t.Fatalf("default for 'My Category' = %q", got)
	}
	// An empty filename really writes the default: run the flow in a
	// temp working directory.
	s := openSeededStore(t)
	m := New(s, "test")
	m = toList(m)
	dir := t.TempDir()
	t.Chdir(dir)
	m = update(m, keyMsg("E"))
	m = typeText(m, "docker")
	m = update(m, keyMsg("enter"))
	m = update(m, keyMsg("enter")) // empty filename -> default
	if m.prompt != promptNone {
		t.Fatalf("export did not close after Enter")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "cheats-docker.json"))
	if err != nil {
		t.Fatalf("default file not written: %v", err)
	}
	var seed db.SeedFile
	if err := json.Unmarshal(raw, &seed); err != nil {
		t.Fatalf("default file is not valid JSON: %v", err)
	}
	if len(seed.Entries) != 214 {
		t.Fatalf("default file has %d entries, want 214", len(seed.Entries))
	}
}

func TestTUIImportFlow(t *testing.T) {
	s := openSeededStore(t)
	m := New(s, "test")
	m = toList(m)
	dir := t.TempDir()
	path := filepath.Join(dir, "in.json")
	// One new entry in a new category, plus a fuller description for
	// an existing docker command.
	docker, err := s.Search("", "docker", 0, 1)
	if err != nil || len(docker) == 0 {
		t.Fatalf("no docker entry to enrich: %v", err)
	}
	ex := docker[0]
	content := fmt.Sprintf(`{"version": 1,
		"categories": [{"name": "kubernetes", "title": "Kubernetes"}],
		"entries": [
			{"category": "kubernetes", "command": "kubectl get pods", "description": "List pods", "level": 1},
			{"category": "docker", "command": %q, "description": %q, "level": 1}]}`,
		ex.Command, ex.Description+" — enriched with more detail here")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write import file: %v", err)
	}
	// 'I' opens the import prompt at step 1 (file path).
	m = update(m, keyMsg("I"))
	if m.prompt != promptImport || m.importStep != 0 {
		t.Fatalf("I did not open the import prompt: %d step %d", m.prompt, m.importStep)
	}
	// An empty file path sets an inline error and stays on step 1.
	m = update(m, keyMsg("enter"))
	if m.prompt != promptImport || m.importStep != 0 {
		t.Fatalf("empty file path left the import prompt")
	}
	if m.formErr == "" {
		t.Fatalf("empty file path did not set an inline error")
	}
	// Esc cancels.
	m = update(m, keyMsg("esc"))
	if m.prompt != promptNone {
		t.Fatalf("Esc did not cancel the import prompt")
	}
	// Full flow: file path, then empty category (use the file's).
	m = update(m, keyMsg("I"))
	m = typeText(m, path)
	m = update(m, keyMsg("enter"))
	if m.prompt != promptImport || m.importStep != 1 {
		t.Fatalf("after file path, step = %d, want 1", m.importStep)
	}
	if !strings.Contains(m.View(), "Existing:") {
		t.Fatalf("category step does not list existing categories")
	}
	m = update(m, keyMsg("enter")) // empty category -> from the file
	if m.prompt != promptNone {
		t.Fatalf("import prompt did not close after Enter")
	}
	if !strings.Contains(m.status, "Imported 1 new, 1 updated, 0 unchanged") {
		t.Fatalf("import status = %q", m.status)
	}
	// The new entry and category exist; the docker description grew.
	found, _ := s.Search("kubectl get pods", "", 0, 0)
	if len(found) != 1 || found[0].Category != "kubernetes" {
		t.Fatalf("imported entry wrong: %+v", found)
	}
	after, err := s.GetEntry(ex.ID)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if after.Description != ex.Description+" — enriched with more detail here" {
		t.Fatalf("description not enriched: %q", after.Description)
	}
	// The sidebar shows the new category.
	if !m.selectCategoryByName("kubernetes") {
		t.Fatalf("kubernetes not in sidebar after import")
	}
	// A missing file reports an error toast and stays in the list.
	m = update(m, keyMsg("I"))
	m = typeText(m, filepath.Join(dir, "missing.json"))
	m = update(m, keyMsg("enter"))
	m = update(m, keyMsg("enter")) // empty category
	if m.prompt != promptNone {
		t.Fatalf("missing-file import did not close the prompt")
	}
	if !strings.Contains(m.status, "Import failed") {
		t.Fatalf("missing-file status = %q", m.status)
	}
}

func TestTUIArrowCursor(t *testing.T) {
	// Force the richest color profile so the "no background" check is
	// real: with a background fill the renderer would emit 48; codes.
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := New(openSeededStore(t), "test")
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m = toList(m)
	rows := strippedListLines(m)
	// The selected row (cursor 0) carries the arrow; the next row has
	// the 2-space gutter, so nothing shifts when the cursor moves.
	if !strings.HasPrefix(rows[0], "> ") {
		t.Fatalf("selected row has no arrow: %q", rows[0])
	}
	if !strings.HasPrefix(rows[2], "  ") || strings.HasPrefix(rows[2], "> ") {
		t.Fatalf("second row gutter wrong: %q", rows[2])
	}
	// No background fill on any row line: no 48; SGR sequence.
	for _, ln := range listLines(m) {
		if strings.Contains(ln, "48;") {
			t.Fatalf("row line has a background fill: %q", ln)
		}
	}
	// j moves the arrow down one row, k moves it back up.
	m = update(m, keyMsg("j"))
	rows = strippedListLines(m)
	if !strings.HasPrefix(rows[0], "  ") || strings.HasPrefix(rows[0], "> ") {
		t.Fatalf("arrow did not leave the first row: %q", rows[0])
	}
	if !strings.HasPrefix(rows[2], "> ") {
		t.Fatalf("arrow did not move to the second row: %q", rows[2])
	}
	m = update(m, keyMsg("k"))
	rows = strippedListLines(m)
	if !strings.HasPrefix(rows[0], "> ") {
		t.Fatalf("arrow did not move back to the first row: %q", rows[0])
	}
	// The sidebar's active row carries the same arrow, not a fill.
	view := m.View()
	if !strings.Contains(stripANSI(view), "> ● All") {
		t.Fatalf("sidebar active row has no arrow")
	}
	for _, ln := range strings.Split(view, "\n") {
		if strings.Contains(stripANSI(ln), "> ● All") && strings.Contains(ln, "48;") {
			t.Fatalf("sidebar active row has a background fill: %q", ln)
		}
	}
}

func TestTUINoTruecolor(t *testing.T) {
	// Force the richest profile: with hex colors the renderer would
	// emit 24-bit sequences here, so their absence proves every color
	// is an ANSI palette number.
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := New(openSeededStore(t), "test")
	for _, size := range [][2]int{{60, 20}, {80, 24}, {120, 30}} {
		m = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View()
		if strings.Contains(view, "38;2;") || strings.Contains(view, "48;2;") {
			t.Fatalf("truecolor sequence in view at %dx%d", size[0], size[1])
		}
	}
	// The help overlay and a prompt use the same styles: check them too.
	m = update(m, keyMsg("?"))
	view := m.View()
	if strings.Contains(view, "38;2;") || strings.Contains(view, "48;2;") {
		t.Fatalf("truecolor sequence in help view")
	}
	m = update(m, keyMsg("esc"))
	m = toList(m)
	m = update(m, keyMsg("a"))
	view = m.View()
	if strings.Contains(view, "38;2;") || strings.Contains(view, "48;2;") {
		t.Fatalf("truecolor sequence in add-form view")
	}
	// ...and colors really are on (otherwise the check is vacuous).
	m = update(m, keyMsg("esc"))
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	if !strings.Contains(m.View(), "38;5;") {
		t.Fatalf("no ANSI colors in the view at all — profile not applied?")
	}
}

func TestTUIWindowTitle(t *testing.T) {
	m := New(openSeededStore(t), "test")
	msgs := execCmd(m.Init())
	found := false
	for _, msg := range msgs {
		if fmt.Sprintf("%T", msg) == "tea.setWindowTitleMsg" &&
			fmt.Sprintf("%v", msg) == "cheat-terminal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Init did not set the window title: %#v", msgs)
	}
}

func TestTUIDetailPaneWidth(t *testing.T) {
	m := New(openSeededStore(t), "test")
	// The detail pane takes about 45% of the width (v1.4.0 gave it 42
	// columns at 120); the results list keeps at least 30 columns.
	for _, tc := range []struct {
		w, detail, list int
	}{
		{90, 34, 30},
		{100, 44, 30},
		{110, 49, 35},
		{120, 54, 40},
		{160, 72, 62},
	} {
		m = update(m, tea.WindowSizeMsg{Width: tc.w, Height: 30})
		_, d, l := m.layout()
		if d != tc.detail || l != tc.list {
			t.Fatalf("layout at %d: detail %d list %d, want %d/%d", tc.w, d, l, tc.detail, tc.list)
		}
	}
	// Below 90 columns there is no detail pane.
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if _, d, _ := m.layout(); d != 0 {
		t.Fatalf("detail pane at 80 columns: %d, want 0", d)
	}
	// A long command wraps cleanly inside the wider pane: no view
	// line is ever wider than the terminal.
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	for _, ln := range strings.Split(m.View(), "\n") {
		if lipgloss.Width(ln) > 120 {
			t.Fatalf("view line wider than the terminal: %q", ln)
		}
	}
}

func TestTUITagChipsReadable(t *testing.T) {
	// Force the richest color profile so the style checks are real.
	lipgloss.SetColorProfile(termenv.TrueColor)
	// Tags render as bright bracket labels with no background fill:
	// [tag] in a bright color, never dark text on a colored block.
	lines := tagChips("containers, list, docker", 40)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"[containers]", "[list]", "[docker]"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tag chips missing %q: %q", want, joined)
		}
	}
	if strings.Contains(joined, "48;") {
		t.Fatalf("tag chips use a background fill: %q", joined)
	}
	// ...and the text is a bright foreground color (standard bright
	// 90-97, or a bright 256-color code) — never a dark one.
	brightRe := regexp.MustCompile("\x1b\\[[0-9;]*(9[0-7]|38;5;(9|1[0-5]|208))m")
	if !brightRe.MatchString(joined) {
		t.Fatalf("tag chips carry no bright ANSI color: %q", joined)
	}
	// The level chip and the category chip use the same style.
	if chip := levelChip(db.LevelEssential); !strings.Contains(chip, "[● Essential]") || strings.Contains(chip, "48;") {
		t.Fatalf("level chip wrong: %q", chip)
	}
	if cc := categoryChip("docker", "Docker"); !strings.Contains(cc, "[Docker]") || strings.Contains(cc, "48;") {
		t.Fatalf("category chip wrong: %q", cc)
	}
	// In the detail pane the section labels are bright (bold white),
	// not dim: the escape sequence right before "Description:" must
	// carry color 15, not the dim color 7.
	m := New(openSeededStore(t), "test")
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	view := m.View()
	if !strings.Contains(stripANSI(view), "Description:") {
		t.Fatalf("detail pane has no Description label")
	}
	re := regexp.MustCompile("\x1b\\[[0-9;]*mDescription:")
	mm := re.FindString(view)
	if mm == "" {
		t.Fatalf("no styled Description label in the view")
	}
	if strings.Contains(mm, "38;5;7") || strings.Contains(mm, ";37m") {
		t.Fatalf("detail Description label is dim: %q", mm)
	}
	// Bright white: color 15 renders as standard code 97 (or 38;5;15).
	if !strings.Contains(mm, "97m") && !strings.Contains(mm, "38;5;15") {
		t.Fatalf("detail Description label is not bright: %q", mm)
	}
	// ...and the tags show as bracket labels in the pane.
	if !strings.Contains(stripANSI(view), "[") {
		t.Fatalf("detail pane shows no bracket tags")
	}
}

func TestTUIEmptySearchHint(t *testing.T) {
	m := New(openSeededStore(t), "test")
	m = toList(m)
	m = update(m, keyMsg("f")) // Essential
	// "tldr" exists only at Advanced: at Essential the search is
	// empty, and the list must say the filter is the cause.
	m = typeText(m, "tldr")
	if len(m.results) != 0 {
		t.Fatalf("expected no results for tldr at Essential, got %d", len(m.results))
	}
	if !strings.Contains(m.View(), "No matches.") {
		t.Fatalf("empty list does not say No matches")
	}
	if !strings.Contains(m.View(), "Level filter is Essential") {
		t.Fatalf("empty list does not blame the level filter")
	}
	// Clearing the filter brings the results back and the hint goes.
	m = update(m, keyMsg("esc")) // clear query
	m = update(m, keyMsg("esc")) // list mode
	m = update(m, keyMsg("f"))   // Common
	m = update(m, keyMsg("f"))   // Advanced
	m = update(m, keyMsg("f"))   // All
	m = typeText(m, "tldr")
	if len(m.results) == 0 {
		t.Fatalf("expected results for tldr at All")
	}
	if strings.Contains(m.View(), "Level filter is") {
		t.Fatalf("hint shown although results exist")
	}
}

func TestTUISearchRanking(t *testing.T) {
	// Typing "docker ps" puts the exact command first, not a
	// description match.
	m := New(openSeededStore(t), "test")
	m = typeText(m, "docker ps")
	if len(m.results) == 0 || m.results[0].Command != "docker ps" {
		t.Fatalf("typing 'docker ps' first = %q, want docker ps", firstResultCommand(m))
	}
	// A partial word finds its commands while typing.
	m = New(openSeededStore(t), "test")
	m = typeText(m, "dock")
	if len(m.results) == 0 || !strings.HasPrefix(strings.ToLower(m.results[0].Command), "dock") {
		t.Fatalf("typing 'dock' first = %q", firstResultCommand(m))
	}
	// Punctuation in the query does not break the search.
	m = New(openSeededStore(t), "test")
	m = typeText(m, "ps | grep")
	if len(m.results) == 0 || m.results[0].Command != "ps aux | grep nginx" {
		t.Fatalf("typing 'ps | grep' first = %q", firstResultCommand(m))
	}
}

func firstResultCommand(m Model) string {
	if len(m.results) == 0 {
		return "<none>"
	}
	return m.results[0].Command
}
