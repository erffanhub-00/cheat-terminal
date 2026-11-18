// Package db owns the cheat-terminal SQLite database: schema, FTS5 index,
// seeding and all queries.
//
// The database file is created on first run next to the executable (or in
// the user config directory when that is not writable). Override the location
// with the CHEAT_TERMINAL_DB environment variable or the --db flag.
//
// Seed records (source = 'seed') come from data/seed.json and are upserted
// on import; user records (source = 'user') are never touched by an import,
// so a seed update can never wipe the user's own entries.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    category_id INTEGER NOT NULL REFERENCES categories (id),
    command TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    tags TEXT NOT NULL DEFAULT '',
    example TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'user' CHECK (source IN ('seed', 'user')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
    command, title, description, tags, example,
    content = 'entries', content_rowid = 'id'
);
CREATE TRIGGER IF NOT EXISTS entries_ai AFTER INSERT ON entries BEGIN
    INSERT INTO entries_fts (rowid, command, title, description, tags, example)
    VALUES (new.id, new.command, new.title, new.description, new.tags, new.example);
END;
CREATE TRIGGER IF NOT EXISTS entries_ad AFTER DELETE ON entries BEGIN
    INSERT INTO entries_fts (entries_fts, rowid, command, title, description, tags, example)
    VALUES ('delete', old.id, old.command, old.title, old.description, old.tags, old.example);
END;
CREATE TRIGGER IF NOT EXISTS entries_au AFTER UPDATE ON entries BEGIN
    INSERT INTO entries_fts (entries_fts, rowid, command, title, description, tags, example)
    VALUES ('delete', old.id, old.command, old.title, old.description, old.tags, old.example);
    INSERT INTO entries_fts (rowid, command, title, description, tags, example)
    VALUES (new.id, new.command, new.title, new.description, new.tags, new.example);
END;
`

// Entry is one cheat-sheet row.
type Entry struct {
	ID            int64
	CategoryID    int64
	Category      string // category name, e.g. "docker"
	CategoryTitle string
	Command       string
	Title         string
	Description   string
	Tags          string
	Example       string
	Source        string // "seed" or "user"
	CreatedAt     string
	UpdatedAt     string
}

// Category is one sidebar group with its entry count.
type Category struct {
	ID    int64
	Name  string
	Title string
	Count int
}

// Store wraps a SQLite connection.
type Store struct {
	db   *sql.DB
	path string
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// DefaultPath picks where cheats.db should live: next to the executable
// when that directory is writable, otherwise the user config directory.
func DefaultPath() string {
	if p := os.Getenv("CHEAT_TERMINAL_DB"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if probe, err := os.CreateTemp(dir, ".cheat-terminal-write-test-*"); err == nil {
			probe.Close()
			_ = os.Remove(probe.Name())
			return filepath.Join(dir, "cheats.db")
		}
	}
	dataDir, err := os.UserConfigDir()
	if err != nil {
		dataDir = os.TempDir()
	}
	dir := filepath.Join(dataDir, "cheat-terminal")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "cheats.db")
}

// Open creates (or opens) the database at path and applies the schema.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if _, err := sqlDB.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: sqlDB, path: path}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }
