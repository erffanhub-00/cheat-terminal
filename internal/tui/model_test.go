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

func TestTUITypeToSearch(t *testing.T) {
	m := New(openSeededStore(t))
	if len(m.results) == 0 {
		t.Fatalf("expected seeded results on start")
	}
	m = update(m, keyMsg("d"))
	m = update(m, keyMsg("o"))
	m = update(m, keyMsg("c"))
	m = update(m, keyMsg("k"))
	m = update(m, keyMsg("e"))
	m = update(m, keyMsg("r"))
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

func TestTUICategoryTabAndDetail(t *testing.T) {
	m := New(openSeededStore(t))
	// Categories are ordered by name: docker, git, linux, powershell.
	// One Tab moves from All to Docker.
	m = update(m, keyMsg("tab"))
	if m.activeCategoryName() != "docker" {
		t.Fatalf("after one Tab, category = %q, want docker", m.activeCategoryName())
	}
	if len(m.results) == 0 {
		t.Fatalf("expected docker results after Tab")
	}
	// Enter opens the detail view of the first result.
	m = update(m, keyMsg("enter"))
	if m.focus != focusDetail {
		t.Fatalf("Enter did not open detail view")
	}
	if m.detail.Category != "docker" {
		t.Fatalf("detail category = %q, want docker", m.detail.Category)
	}
	if !strings.Contains(m.View(), m.detail.Command) {
		t.Fatalf("detail view does not show the command")
	}
	// Esc goes back to the list.
	m = update(m, keyMsg("esc"))
	if m.focus != focusDetail {
		// focusDetail -> Esc returns to focusList
	}
	if m.focus == focusDetail {
		t.Fatalf("Esc did not leave the detail view")
	}
}

func TestTUIEscClearsQuery(t *testing.T) {
	m := New(openSeededStore(t))
	m = update(m, keyMsg("g"))
	m = update(m, keyMsg("i"))
	m = update(m, keyMsg("t"))
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
