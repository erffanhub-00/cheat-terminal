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

// TestTUIViewFitsTerminal guards the layout: the view must never be taller
// than the terminal, or the header and search box scroll off the top.
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
