package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	if m.addErr == "" {
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

func TestTUITwoLineRows(t *testing.T) {
	m := New(openSeededStore(t), "test")
	// Two-line rows take 3 lines each, so fewer rows fit per page.
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	if v := m.visibleRows(); v != 8 {
		t.Fatalf("visible rows at 120x30 = %d, want 8", v)
	}
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if v := m.visibleRows(); v != 6 {
		t.Fatalf("visible rows at 80x24 = %d, want 6", v)
	}
	// Check the list column itself (the full view joins the sidebar
	// next to it, so its lines are never blank).
	_, _, listW := m.layout()
	lines := strings.Split(m.viewList(listW, m.bodyHeight()), "\n")
	// Find a visible entry with a description and check its command and
	// description sit on two adjacent lines, with a blank line after.
	checked := false
	for i := 0; i < m.visibleRows() && i < len(m.results); i++ {
		e := m.results[i]
		if strings.TrimSpace(e.Description) == "" {
			continue
		}
		// Scan for the command line whose next line holds the description.
		cmdIdx := -1
		for j := 0; j+1 < len(lines); j++ {
			if strings.Contains(lines[j], e.Command) &&
				strings.Contains(lines[j+1], firstRunes(e.Description, 20)) {
				cmdIdx = j
				break
			}
		}
		if cmdIdx < 0 {
			t.Fatalf("no two-line row found for %q", e.Command)
		}
		if cmdIdx+2 >= len(lines) || lines[cmdIdx+2] != "" {
			t.Fatalf("no blank line after the row: %q", lines[cmdIdx+2])
		}
		checked = true
		break
	}
	if !checked {
		t.Fatalf("no visible entry with a description to check")
	}
}
func TestTUIViewFitsTerminal(t *testing.T) {
	m := New(openSeededStore(t), "test")
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	if n := len(strings.Split(m.View(), "\n")); n != 30 {
		t.Fatalf("view lines = %d, want 30", n)
	}
	for i := 0; i < 60; i++ {
		m = update(m, keyMsg("down"))
		if n := len(strings.Split(m.View(), "\n")); n != 30 {
			t.Fatalf("view lines = %d at cursor %d, want 30", n, m.cursor)
		}
	}
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if n := len(strings.Split(m.View(), "\n")); n != 24 {
		t.Fatalf("narrow view lines = %d, want 24", n)
	}
}
