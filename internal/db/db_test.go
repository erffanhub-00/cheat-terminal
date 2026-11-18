package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/erffanhub-00/cheat-terminal/data"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSeedImport(t *testing.T) {
	s := openTestStore(t)
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	n, err := s.CountEntries()
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected seeded entries, got 0")
	}
	cats, err := s.ListCategories()
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	if len(cats) != 4 {
		t.Fatalf("expected 4 categories, got %d", len(cats))
	}
	// Second run must be a no-op (seeded flag), not a duplicate import.
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("second EnsureSeeded: %v", err)
	}
	n2, _ := s.CountEntries()
	if n2 != n {
		t.Fatalf("second seed changed count: %d -> %d", n, n2)
	}
	// Re-import is idempotent: upserts, never duplicates.
	ins, upd, err := s.ImportSeedBytes(data.SeedJSON)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if ins != 0 {
		t.Fatalf("re-import inserted %d, want 0", ins)
	}
	if upd != n {
		t.Fatalf("re-import updated %d, want %d", upd, n)
	}
	n3, _ := s.CountEntries()
	if n3 != n {
		t.Fatalf("re-import changed count: %d -> %d", n, n3)
	}
}

func TestSearch(t *testing.T) {
	s := openTestStore(t)
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	// Prefix search finds commands while typing.
	got, err := s.Search("docker ps", "", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected matches for 'docker ps'")
	}
	found := false
	for _, e := range got {
		if e.Command == "docker ps -a" {
			found = true
		}
		if e.Category != "docker" {
			t.Fatalf("search leaked category: %q", e.Category)
		}
	}
	if !found {
		t.Fatalf("'docker ps -a' not in results for 'docker ps'")
	}
	// Category filter.
	got, err = s.Search("ps", "linux", 50)
	if err != nil {
		t.Fatalf("Search linux: %v", err)
	}
	for _, e := range got {
		if e.Category != "linux" {
			t.Fatalf("category filter leaked: %q", e.Category)
		}
	}
	// Empty query lists everything in a category.
	got, err = s.Search("", "git", 0)
	if err != nil {
		t.Fatalf("list git: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected git entries for empty query")
	}
	// Unknown words return nothing, not an error.
	got, err = s.Search("zzzqqqxxx", "", 10)
	if err != nil {
		t.Fatalf("Search unknown: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no results for gibberish, got %d", len(got))
	}
}

func TestCRUD(t *testing.T) {
	s := openTestStore(t)
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	id, err := s.AddEntry(Entry{
		Category:    "linux",
		Command:     "my-test-command --flag",
		Title:       "My test command",
		Description: "Added by the test suite",
		Tags:        "linux,test",
		Example:     "my-test-command --flag /tmp",
	})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	got, err := s.Search("my-test-command", "", 10)
	if err != nil {
		t.Fatalf("Search after add: %v", err)
	}
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("added entry not searchable: %+v", got)
	}
	if got[0].Source != "user" {
		t.Fatalf("added entry source = %q, want user", got[0].Source)
	}
	// Edit: FTS must reflect the new text and forget the old one.
	e, err := s.GetEntry(id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	e.Description = "Edited description"
	if err := s.UpdateEntry(e); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	got, _ = s.Search("Edited description", "", 10)
	if len(got) != 1 {
		t.Fatalf("edited entry not searchable by new text")
	}
	// A seed re-import must never touch the user entry.
	if _, _, err := s.ImportSeedBytes(data.SeedJSON); err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if _, err := s.GetEntry(id); err != nil {
		t.Fatalf("user entry lost after seed re-import: %v", err)
	}
	// Delete.
	ok, err := s.DeleteEntry(id)
	if err != nil || !ok {
		t.Fatalf("DeleteEntry: ok=%v err=%v", ok, err)
	}
	got, _ = s.Search("my-test-command", "", 10)
	if len(got) != 0 {
		t.Fatalf("deleted entry still searchable")
	}
	if _, err := s.GetEntry(id); err == nil {
		t.Fatalf("deleted entry still retrievable")
	}
}

func TestAddCategory(t *testing.T) {
	s := openTestStore(t)
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	id, existed, err := s.AddCategory("kubernetes", "Kubernetes")
	if err != nil || existed || id == 0 {
		t.Fatalf("AddCategory: id=%d existed=%v err=%v", id, existed, err)
	}
	_, existed, err = s.AddCategory("kubernetes", "Kubernetes")
	if err != nil || !existed {
		t.Fatalf("second AddCategory: existed=%v err=%v", existed, err)
	}
	cats, err := s.ListCategories()
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	found := false
	for _, c := range cats {
		if c.Name == "kubernetes" && c.Count == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("new category missing from list")
	}
}

func TestImportDir(t *testing.T) {
	s := openTestStore(t)
	dir := t.TempDir()
	seed := `{"version":1,"categories":[{"name":"tools","title":"Tools"}],
		"entries":[{"category":"tools","command":"mytool --help","title":"mytool --help","description":"Help for mytool","tags":"tools","example":""}]}`
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(seed), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	ins, upd, err := s.ImportSeedDir(dir)
	if err != nil {
		t.Fatalf("ImportSeedDir: %v", err)
	}
	if ins != 1 || upd != 0 {
		t.Fatalf("ImportSeedDir: inserted=%d updated=%d, want 1/0", ins, upd)
	}
	got, err := s.Search("mytool", "", 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("imported entry not searchable: %v %v", got, err)
	}
	if _, _, err := s.ImportSeedDir(dir); err != nil {
		t.Fatalf("second ImportSeedDir: %v", err)
	}
	if _, _, err := s.ImportSeedDir(t.TempDir()); err == nil {
		t.Fatalf("empty dir should fail")
	}
}
