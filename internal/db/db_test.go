package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erffanhub-00/cheat-terminal/data"
	_ "modernc.org/sqlite"
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
	got, err := s.Search("docker ps", "", 0, 10)
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
	got, err = s.Search("ps", "linux", 0, 50)
	if err != nil {
		t.Fatalf("Search linux: %v", err)
	}
	for _, e := range got {
		if e.Category != "linux" {
			t.Fatalf("category filter leaked: %q", e.Category)
		}
	}
	// Empty query lists everything in a category.
	got, err = s.Search("", "git", 0, 0)
	if err != nil {
		t.Fatalf("list git: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected git entries for empty query")
	}
	// Unknown words return nothing, not an error.
	got, err = s.Search("zzzqqqxxx", "", 0, 10)
	if err != nil {
		t.Fatalf("Search unknown: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no results for gibberish, got %d", len(got))
	}
}

// TestSearchRanking pins the search behavior the user asked for: the
// exact command first, live prefix matching while typing, punctuation
// that never breaks the query, and sensible results when a word in the
// query matches nothing at all.
func TestSearchRanking(t *testing.T) {
	s := openTestStore(t)
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	top := func(q string) (Entry, error) {
		got, err := s.Search(q, "", 0, 10)
		if err != nil {
			return Entry{}, err
		}
		if len(got) == 0 {
			return Entry{}, errNoResults
		}
		return got[0], nil
	}
	// Exact commands rank first, ahead of entries whose description
	// merely mentions the words.
	for _, tc := range []struct{ query, want string }{
		{"docker ps", "docker ps"},
		{"git log --oneline", "git log --oneline"},
		{"docker ps -a", "docker ps -a"},
		{"git stash", "git stash"},
	} {
		first, err := top(tc.query)
		if err != nil {
			t.Fatalf("Search(%q): %v", tc.query, err)
		}
		if first.Command != tc.want {
			t.Fatalf("Search(%q) first = %q, want %q", tc.query, first.Command, tc.want)
		}
	}
	// Prefix matching while typing: a partial word finds its commands,
	// and the top hit is a command starting with that word.
	for _, q := range []string{"dock", "doc", "git sta"} {
		first, err := top(q)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if !strings.HasPrefix(strings.ToLower(first.Command), strings.Fields(q)[0]) {
			t.Fatalf("Search(%q) first = %q, want a command starting with %q",
				q, first.Command, strings.Fields(q)[0])
		}
	}
	// Punctuation never breaks the search and never errors: pipes,
	// slashes, dashes, quotes, colons and stars are literal text.
	for _, tc := range []struct {
		query string
		want  string // a command that must be in the results
	}{
		{"ps | grep", "ps aux | grep nginx"},
		{"find / -name", `find . -name "*.log"`},
		{"docker ps | grep", "Port unreachable"},
		{`echo "hi"`, `echo "$TOKEN" | docker login -u user --password-stdin`},
		{"git log --oneline --graph", "git log --graph --oneline --all"},
	} {
		got, err := s.Search(tc.query, "", 0, 0)
		if err != nil {
			t.Fatalf("Search(%q): %v", tc.query, err)
		}
		if len(got) == 0 {
			t.Fatalf("Search(%q) returned nothing", tc.query)
		}
		found := false
		for _, e := range got {
			if e.Command == tc.want {
				found = true
			}
		}
		if !found {
			t.Fatalf("Search(%q) missing %q (got %d results, first %q)",
				tc.query, tc.want, len(got), got[0].Command)
		}
	}
	// A query that is only punctuation, or has a lone star, still works.
	for _, q := range []string{"docker *", "a:b", "(", "docker ps -a"} {
		got, err := s.Search(q, "", 0, 10)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(got) == 0 {
			t.Fatalf("Search(%q) returned nothing", q)
		}
	}
	// A mid-word fragment matches too (substring tier).
	got, err := s.Search("ompose", "", 0, 10)
	if err != nil {
		t.Fatalf("Search(ompose): %v", err)
	}
	if len(got) == 0 || !strings.Contains(strings.ToLower(got[0].Command), "compose") {
		t.Fatalf("Search(ompose) first = %q, want a compose command", firstCommand(got))
	}
	// A real word plus a word the database does not know still shows
	// the real word's entries (any-token fallback) instead of nothing.
	got, err = s.Search("echo zzzqqq", "", 0, 10)
	if err != nil {
		t.Fatalf("Search(echo zzzqqq): %v", err)
	}
	if len(got) == 0 || !strings.HasPrefix(strings.ToLower(got[0].Command), "echo") {
		t.Fatalf("Search(echo zzzqqq) first = %q, want an echo command", firstCommand(got))
	}
	// The level filter still scopes the search: Essential only.
	got, err = s.Search("docker ps", "", LevelEssential, 0)
	if err != nil {
		t.Fatalf("Search with level: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("level-filtered search returned nothing")
	}
	for _, e := range got {
		if e.Level != LevelEssential {
			t.Fatalf("level filter leaked: %q level %d", e.Command, e.Level)
		}
	}
	if got[0].Command != "docker ps" {
		t.Fatalf("level-filtered first = %q, want docker ps", got[0].Command)
	}
}

func firstCommand(got []Entry) string {
	if len(got) == 0 {
		return "<none>"
	}
	return got[0].Command
}

var errNoResults = errString("no results")

type errString string

func (e errString) Error() string { return string(e) }

// TestSearchTokens pins the tokenizer: punctuation splits tokens, and
// a query of only punctuation yields no tokens.
func TestSearchTokens(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"docker ps -a", []string{"docker", "ps", "a"}},
		{"ps | grep", []string{"ps", "grep"}},
		{"find / -name", []string{"find", "name"}},
		{`echo "hi"`, []string{"echo", "hi"}},
		{"a:b", []string{"a", "b"}},
		{"git log --oneline", []string{"git", "log", "oneline"}},
		{"  docker   ps  ", []string{"docker", "ps"}},
		{"|", nil},
		{"", nil},
	} {
		got := searchTokens(tc.query)
		if len(got) != len(tc.want) {
			t.Fatalf("searchTokens(%q) = %q, want %q", tc.query, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("searchTokens(%q) = %q, want %q", tc.query, got, tc.want)
			}
		}
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
	got, err := s.Search("my-test-command", "", 0, 10)
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
	got, _ = s.Search("Edited description", "", 0, 10)
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
	got, _ = s.Search("my-test-command", "", 0, 10)
	for _, e := range got {
		if e.ID == id {
			t.Fatalf("deleted entry still searchable: %+v", e)
		}
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
	got, err := s.Search("mytool", "", 0, 10)
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

func TestLevels(t *testing.T) {
	s := openTestStore(t)
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	// Every level has entries in the seed.
	for _, lvl := range []int{LevelEssential, LevelCommon, LevelAdvanced} {
		got, err := s.Search("", "", lvl, 0)
		if err != nil {
			t.Fatalf("Search level %d: %v", lvl, err)
		}
		if len(got) == 0 {
			t.Fatalf("no entries at level %d", lvl)
		}
		for _, e := range got {
			if e.Level != lvl {
				t.Fatalf("level filter leaked: entry level %d in level %d", e.Level, lvl)
			}
		}
	}
	// Level and category filters combine.
	got, err := s.Search("", "docker", LevelEssential, 0)
	if err != nil {
		t.Fatalf("Search docker essential: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("no essential docker entries")
	}
	for _, e := range got {
		if e.Category != "docker" || e.Level != LevelEssential {
			t.Fatalf("combined filter leaked: %+v", e)
		}
	}
	// A text query respects the level filter too.
	got, err = s.Search("docker ps", "", LevelAdvanced, 10)
	if err != nil {
		t.Fatalf("Search with level: %v", err)
	}
	for _, e := range got {
		if e.Level != LevelAdvanced {
			t.Fatalf("query level filter leaked: %+v", e)
		}
	}
	// Level names and parsing.
	if LevelName(LevelEssential) != "Essential" || LevelName(0) != "All" {
		t.Fatalf("LevelName wrong")
	}
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"essential", LevelEssential}, {"1", LevelEssential},
		{"common", LevelCommon}, {"advanced", LevelAdvanced},
		{"", LevelAll}, {"all", LevelAll},
	} {
		got, err := ParseLevel(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("ParseLevel(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
		}
	}
	if _, err := ParseLevel("nonsense"); err == nil {
		t.Fatalf("ParseLevel accepted a bad level")
	}
}

func TestFavorites(t *testing.T) {
	s := openTestStore(t)
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	got, err := s.Search("docker ps", "", 0, 5)
	if err != nil || len(got) == 0 {
		t.Fatalf("seed search: %v %v", got, err)
	}
	id := got[0].ID
	if n, _ := s.CountFavorites(); n != 0 {
		t.Fatalf("favorites not empty at start: %d", n)
	}
	ok, err := s.SetFavorite(id, true)
	if err != nil || !ok {
		t.Fatalf("SetFavorite: ok=%v err=%v", ok, err)
	}
	favs, err := s.ListFavorites(0)
	if err != nil || len(favs) != 1 || favs[0].ID != id {
		t.Fatalf("ListFavorites: %v %v", favs, err)
	}
	if !favs[0].Favorite {
		t.Fatalf("starred entry does not report Favorite")
	}
	e, err := s.GetEntry(id)
	if err != nil || !e.Favorite {
		t.Fatalf("GetEntry after star: %v %+v", err, e)
	}
	// A seed re-import must keep the star.
	if _, _, err := s.ImportSeedBytes(data.SeedJSON); err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if n, _ := s.CountFavorites(); n != 1 {
		t.Fatalf("star lost after seed re-import: %d", n)
	}
	// Unstar.
	ok, err = s.SetFavorite(id, false)
	if err != nil || !ok {
		t.Fatalf("unstar: ok=%v err=%v", ok, err)
	}
	if n, _ := s.CountFavorites(); n != 0 {
		t.Fatalf("favorites not empty after unstar: %d", n)
	}
	// Starring a missing id reports false, not an error.
	ok, err = s.SetFavorite(999999, true)
	if err != nil || ok {
		t.Fatalf("SetFavorite on missing id: ok=%v err=%v", ok, err)
	}
}

// v1Schema is the schema as of v1.0.0: no level/favorite columns, no
// seed_version in meta. A database built with it must migrate cleanly.
const v1Schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS categories (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, title TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
  command TEXT NOT NULL, title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '',
  example TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT 'user' CHECK (source IN ('seed','user')),
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (category_id, command, source)
);
CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(command, title, description, tags, example, content='entries', content_rowid='id');
CREATE TRIGGER IF NOT EXISTS entries_ai AFTER INSERT ON entries BEGIN
  INSERT INTO entries_fts(rowid, command, title, description, tags, example)
  VALUES (new.id, new.command, new.title, new.description, new.tags, new.example);
END;
CREATE TRIGGER IF NOT EXISTS entries_ad AFTER DELETE ON entries BEGIN
  INSERT INTO entries_fts(entries_fts, rowid, command, title, description, tags, example)
  VALUES ('delete', old.id, old.command, old.title, old.description, old.tags, old.example);
END;
CREATE TRIGGER IF NOT EXISTS entries_au AFTER UPDATE ON entries BEGIN
  INSERT INTO entries_fts(entries_fts, rowid, command, title, description, tags, example)
  VALUES ('delete', old.id, old.command, old.title, old.description, old.tags, old.example);
  INSERT INTO entries_fts(rowid, command, title, description, tags, example)
  VALUES (new.id, new.command, new.title, new.description, new.tags, new.example);
END;
CREATE INDEX IF NOT EXISTS entries_category_idx ON entries(category_id);
`

func TestMigrationFromV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := raw.Exec(v1Schema); err != nil {
		t.Fatalf("v1 schema: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO categories (name, title) VALUES ('docker', 'Docker')`); err != nil {
		t.Fatalf("v1 category: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO entries (category_id, command, title, description, source)
		VALUES (1, 'docker ps -a', 'docker ps -a', 'List all containers', 'seed')`); err != nil {
		t.Fatalf("v1 seed entry: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO entries (category_id, command, title, description, source)
		VALUES (1, 'my-old-command', 'my-old-command', 'My own entry', 'user')`); err != nil {
		t.Fatalf("v1 user entry: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO meta (key, value) VALUES ('seeded', '1')`); err != nil {
		t.Fatalf("v1 meta: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open v1 database: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// The migration added the columns; old rows get the defaults.
	got, err := s.Search("docker ps", "", 0, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("search after migrate: %v %v", got, err)
	}
	if got[0].Level != LevelCommon || got[0].Favorite {
		t.Fatalf("migrated defaults wrong: level=%d favorite=%v", got[0].Level, got[0].Favorite)
	}
	// The user entry survived the migration.
	if _, err := s.GetEntry(2); err != nil {
		t.Fatalf("user entry lost in migration: %v", err)
	}
	// EnsureSeeded sees the old seed version and re-imports the levels.
	if err := s.EnsureSeeded(data.SeedJSON); err != nil {
		t.Fatalf("EnsureSeeded after migrate: %v", err)
	}
	got, err = s.Search("docker ps", "", 0, 10)
	if err != nil {
		t.Fatalf("search after re-seed: %v", err)
	}
	var ps *Entry
	for i := range got {
		if got[i].Command == "docker ps -a" {
			ps = &got[i]
		}
	}
	if ps == nil {
		t.Fatalf("'docker ps -a' missing after re-seed")
	}
	if ps.Level != LevelEssential {
		t.Fatalf("'docker ps -a' level = %d, want Essential (1)", ps.Level)
	}
	// And the user entry is still there, untouched.
	if _, err := s.GetEntry(2); err != nil {
		t.Fatalf("user entry lost in re-seed: %v", err)
	}
	if n, _ := s.CountEntries(); n < 1000 {
		t.Fatalf("re-seed did not import the full seed: %d entries", n)
	}
}
