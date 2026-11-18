package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/erffanhub-00/cheat-terminal/data"
)

func TestExportImportRoundTrip(t *testing.T) {
	s := openTestStore(t)
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	path := filepath.Join(t.TempDir(), "export.json")
	n, err := s.ExportToFile(path, "")
	if err != nil {
		t.Fatalf("ExportToFile: %v", err)
	}
	if n != 1392 {
		t.Fatalf("exported %d, want 1392", n)
	}
	// The file is valid seed-format JSON.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	var seed SeedFile
	if err := json.Unmarshal(raw, &seed); err != nil {
		t.Fatalf("export is not valid seed JSON: %v", err)
	}
	if seed.Version != 1 {
		t.Fatalf("export version = %d, want 1", seed.Version)
	}
	if len(seed.Categories) != 4 {
		t.Fatalf("export categories = %d, want 4", len(seed.Categories))
	}
	if len(seed.Entries) != 1392 {
		t.Fatalf("export entries = %d, want 1392", len(seed.Entries))
	}
	// Import into a fresh store: everything is new.
	s2 := openTestStore(t)
	stats, err := s2.ImportFile(path, "")
	if err != nil {
		t.Fatalf("ImportFile: %v", err)
	}
	if stats.New != 1392 || stats.Updated != 0 || stats.Unchanged != 0 {
		t.Fatalf("import stats = %+v, want 1392 new", stats)
	}
	total, _ := s2.CountEntries()
	if total != 1392 {
		t.Fatalf("imported total = %d, want 1392", total)
	}
	// Spot check: description, level and tags survived the round trip.
	entries, err := s2.Search("", "", 0, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var found *Entry
	for i := range entries {
		if entries[i].Command == "man <cmd>" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("round-tripped entry 'man <cmd>' not found")
	}
	if found.Description != "Full manual for a command (quit with q)" {
		t.Fatalf("description = %q", found.Description)
	}
	if found.Level != LevelEssential {
		t.Fatalf("level = %d, want Essential", found.Level)
	}
	if found.Tags != "linux,getting-help-system-info" {
		t.Fatalf("tags = %q", found.Tags)
	}
	if found.Category != "linux" {
		t.Fatalf("category = %q, want linux", found.Category)
	}
	// Re-importing the same file changes nothing: everything unchanged.
	stats, err = s2.ImportFile(path, "")
	if err != nil {
		t.Fatalf("second ImportFile: %v", err)
	}
	if stats.New != 0 || stats.Updated != 0 || stats.Unchanged != 1392 {
		t.Fatalf("second import stats = %+v, want 1392 unchanged", stats)
	}
}

func TestExportCategory(t *testing.T) {
	s := openTestStore(t)
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	path := filepath.Join(t.TempDir(), "docker.json")
	n, err := s.ExportToFile(path, "docker")
	if err != nil {
		t.Fatalf("ExportToFile docker: %v", err)
	}
	if n != 214 {
		t.Fatalf("exported %d docker entries, want 214", n)
	}
	raw, _ := os.ReadFile(path)
	var seed SeedFile
	if err := json.Unmarshal(raw, &seed); err != nil {
		t.Fatalf("parse export: %v", err)
	}
	if len(seed.Categories) != 1 || seed.Categories[0].Name != "docker" {
		t.Fatalf("export categories = %+v, want only docker", seed.Categories)
	}
	for _, e := range seed.Entries {
		if e.Category != "docker" {
			t.Fatalf("export leaked category %q", e.Category)
		}
	}
	if _, err := s.ExportToFile(filepath.Join(t.TempDir(), "nope.json"), "nosuch"); err == nil {
		t.Fatalf("export of an unknown category did not error")
	}
}

func writeImportFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "import.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write import file: %v", err)
	}
	return path
}

func TestImportUpdateSemantics(t *testing.T) {
	s := openTestStore(t)
	id, err := s.AddEntry(Entry{
		Command: "my cmd", Category: "general",
		Description: "", Tags: "", Level: LevelCommon,
	})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	// A fuller description updates the entry.
	path := writeImportFile(t, `{"version": 1, "categories": [{"name": "general", "title": "General"}],
		"entries": [{"category": "general", "command": "my cmd",
		"description": "does the thing", "tags": "a, b", "level": 1}]}`)
	stats, err := s.ImportFile(path, "")
	if err != nil {
		t.Fatalf("ImportFile: %v", err)
	}
	if stats.New != 0 || stats.Updated != 1 || stats.Unchanged != 0 {
		t.Fatalf("stats = %+v, want 1 updated", stats)
	}
	e, err := s.GetEntry(id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if e.Description != "does the thing" || e.Tags != "a, b" || e.Level != LevelEssential {
		t.Fatalf("entry after update = %+v", e)
	}
	// The same file again: nothing changes.
	stats, err = s.ImportFile(path, "")
	if err != nil {
		t.Fatalf("second ImportFile: %v", err)
	}
	if stats.New != 0 || stats.Updated != 0 || stats.Unchanged != 1 {
		t.Fatalf("second import stats = %+v, want 1 unchanged", stats)
	}
	// A shorter description does NOT replace a fuller one.
	path = writeImportFile(t, `{"version": 1,
		"entries": [{"category": "general", "command": "my cmd",
		"description": "short", "tags": "a, b", "level": 1}]}`)
	stats, err = s.ImportFile(path, "")
	if err != nil {
		t.Fatalf("shorter ImportFile: %v", err)
	}
	if stats.Unchanged != 1 || stats.Updated != 0 {
		t.Fatalf("shorter import stats = %+v, want 1 unchanged", stats)
	}
	e, _ = s.GetEntry(id)
	if e.Description != "does the thing" {
		t.Fatalf("shorter description replaced the fuller one: %q", e.Description)
	}
	// A longer description does replace it.
	path = writeImportFile(t, `{"version": 1,
		"entries": [{"category": "general", "command": "my cmd",
		"description": "does the thing, and also the other thing", "tags": "a, b", "level": 1}]}`)
	stats, err = s.ImportFile(path, "")
	if err != nil {
		t.Fatalf("longer ImportFile: %v", err)
	}
	if stats.Updated != 1 {
		t.Fatalf("longer import stats = %+v, want 1 updated", stats)
	}
	e, _ = s.GetEntry(id)
	if e.Description != "does the thing, and also the other thing" {
		t.Fatalf("longer description did not win: %q", e.Description)
	}
	// A missing level (field absent) leaves the level alone.
	path = writeImportFile(t, `{"version": 1,
		"entries": [{"category": "general", "command": "my cmd",
		"description": "does the thing, and also the other thing", "tags": "a, b"}]}`)
	stats, err = s.ImportFile(path, "")
	if err != nil {
		t.Fatalf("no-level ImportFile: %v", err)
	}
	if stats.Unchanged != 1 {
		t.Fatalf("no-level import stats = %+v, want 1 unchanged", stats)
	}
	e, _ = s.GetEntry(id)
	if e.Level != LevelEssential {
		t.Fatalf("missing level changed the level to %d", e.Level)
	}
	// A brand-new command in the file is inserted, defaulting to Common.
	path = writeImportFile(t, `{"version": 1,
		"entries": [{"category": "general", "command": "my other cmd",
		"description": "a new one"}]}`)
	stats, err = s.ImportFile(path, "")
	if err != nil {
		t.Fatalf("new command ImportFile: %v", err)
	}
	if stats.New != 1 {
		t.Fatalf("new command stats = %+v, want 1 new", stats)
	}
	found, _ := s.Search("my other cmd", "", 0, 0)
	if len(found) != 1 || found[0].Level != LevelCommon || found[0].Source != "user" {
		t.Fatalf("new command wrong: %+v", found)
	}
}

func TestImportCategoryTarget(t *testing.T) {
	s := openTestStore(t)
	path := writeImportFile(t, `{"version": 1,
		"categories": [{"name": "alpha", "title": "Alpha"}, {"name": "beta", "title": "Beta"}],
		"entries": [
			{"category": "alpha", "command": "alpha cmd", "description": "in alpha", "level": 1},
			{"category": "beta", "command": "beta cmd", "description": "in beta", "level": 2}]}`)
	// With a target category, every entry goes there (created on the fly).
	stats, err := s.ImportFile(path, "gamma")
	if err != nil {
		t.Fatalf("ImportFile gamma: %v", err)
	}
	if stats.New != 2 {
		t.Fatalf("gamma import stats = %+v, want 2 new", stats)
	}
	cats, _ := s.ListCategories()
	for _, c := range cats {
		if c.Name == "gamma" && c.Count != 2 {
			t.Fatalf("gamma count = %d, want 2", c.Count)
		}
		if (c.Name == "alpha" || c.Name == "beta") && c.Count != 0 {
			t.Fatalf("%s count = %d, want 0 (entries went to gamma)", c.Name, c.Count)
		}
	}
	// Without a target, entries go to the categories named in the file.
	stats, err = s.ImportFile(path, "")
	if err != nil {
		t.Fatalf("ImportFile no target: %v", err)
	}
	if stats.New != 2 {
		t.Fatalf("no-target import stats = %+v, want 2 new", stats)
	}
	cats, _ = s.ListCategories()
	for _, c := range cats {
		if (c.Name == "alpha" || c.Name == "beta") && c.Count != 1 {
			t.Fatalf("%s count = %d, want 1", c.Name, c.Count)
		}
	}
}

func TestImportErrors(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.ImportFile(filepath.Join(t.TempDir(), "missing.json"), ""); err == nil {
		t.Fatalf("missing file did not error")
	}
	path := writeImportFile(t, `{"version": 1, "entries": [broken json`)
	if _, err := s.ImportFile(path, ""); err == nil {
		t.Fatalf("invalid JSON did not error")
	}
	// Entries with an empty command are skipped, not counted.
	path = writeImportFile(t, `{"version": 1,
		"entries": [{"category": "general", "command": "", "description": "no command"},
		            {"category": "general", "command": "real cmd", "description": "ok"}]}`)
	stats, err := s.ImportFile(path, "")
	if err != nil {
		t.Fatalf("empty-command ImportFile: %v", err)
	}
	if stats.New != 1 || stats.Updated != 0 || stats.Unchanged != 0 {
		t.Fatalf("empty-command stats = %+v, want 1 new", stats)
	}
}
