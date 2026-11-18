package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SeedFile is the shape of data/seed.json and of every --import file.
type SeedFile struct {
	Version    int            `json:"version"`
	Categories []SeedCategory `json:"categories"`
	Entries    []SeedEntry    `json:"entries"`
}

// SeedCategory is one category inside a seed file.
type SeedCategory struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// SeedEntry is one entry inside a seed file.
type SeedEntry struct {
	Category    string `json:"category"`
	Command     string `json:"command"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Tags        string `json:"tags"`
	Example     string `json:"example"`
	Level       int    `json:"level"`
}

// EnsureCategory returns the id of a category, creating it if needed.
func (s *Store) EnsureCategory(name, title string) (int64, error) {
	if title == "" {
		title = name
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO categories (name, title) VALUES (?, ?)`, name, title); err != nil {
		return 0, err
	}
	var id int64
	err := s.db.QueryRow(`SELECT id FROM categories WHERE name = ?`, name).Scan(&id)
	return id, err
}

// AddCategory creates a category. The second return value reports whether
// it already existed.
func (s *Store) AddCategory(name, title string) (int64, bool, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM categories WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if err != sql.ErrNoRows {
		return 0, false, err
	}
	if title == "" {
		title = name
	}
	res, err := s.db.Exec(`INSERT INTO categories (name, title) VALUES (?, ?)`, name, title)
	if err != nil {
		return 0, false, err
	}
	id, err = res.LastInsertId()
	return id, false, err
}

// ListCategories returns every category with its entry count, ordered by name.
func (s *Store) ListCategories() ([]Category, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.name, c.title, COUNT(e.id)
		FROM categories c LEFT JOIN entries e ON e.category_id = c.id
		GROUP BY c.id ORDER BY c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Title, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountEntries returns the total number of entries.
func (s *Store) CountEntries() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&n)
	return n, err
}

// GetEntry returns one entry by id.
func (s *Store) GetEntry(id int64) (Entry, error) {
	var e Entry
	var fav int
	err := s.db.QueryRow(`
		SELECT e.id, e.category_id, c.name, c.title, e.command, e.title,
		       e.description, e.tags, e.example, e.source, e.level, e.favorite,
		       e.created_at, e.updated_at
		FROM entries e JOIN categories c ON c.id = e.category_id
		WHERE e.id = ?`, id).Scan(
		&e.ID, &e.CategoryID, &e.Category, &e.CategoryTitle, &e.Command, &e.Title,
		&e.Description, &e.Tags, &e.Example, &e.Source, &e.Level, &fav,
		&e.CreatedAt, &e.UpdatedAt)
	e.Favorite = fav != 0
	return e, err
}

// AddEntry inserts a user entry, creating its category if needed.
func (s *Store) AddEntry(e Entry) (int64, error) {
	catID, err := s.EnsureCategory(e.Category, e.CategoryTitle)
	if err != nil {
		return 0, err
	}
	if e.Title == "" {
		e.Title = e.Command
	}
	if e.Level < LevelEssential || e.Level > LevelAdvanced {
		e.Level = LevelCommon
	}
	res, err := s.db.Exec(`
		INSERT INTO entries (category_id, command, title, description, tags, example, source, level)
		VALUES (?, ?, ?, ?, ?, ?, 'user', ?)`,
		catID, e.Command, e.Title, e.Description, e.Tags, e.Example, e.Level)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateEntry updates an entry (any field except id, source and created_at).
func (s *Store) UpdateEntry(e Entry) error {
	catID, err := s.EnsureCategory(e.Category, e.CategoryTitle)
	if err != nil {
		return err
	}
	if e.Level < LevelEssential || e.Level > LevelAdvanced {
		e.Level = LevelCommon
	}
	_, err = s.db.Exec(`
		UPDATE entries
		SET category_id = ?, command = ?, title = ?, description = ?,
		    tags = ?, example = ?, level = ?, updated_at = datetime('now')
		WHERE id = ?`,
		catID, e.Command, e.Title, e.Description, e.Tags, e.Example, e.Level, e.ID)
	return err
}

// DeleteEntry removes one entry. It reports whether anything was deleted.
func (s *Store) DeleteEntry(id int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM entries WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var fav int
		if err := rows.Scan(
			&e.ID, &e.CategoryID, &e.Category, &e.CategoryTitle, &e.Command, &e.Title,
			&e.Description, &e.Tags, &e.Example, &e.Source, &e.Level, &fav,
			&e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Favorite = fav != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

const entryColumns = `
	SELECT e.id, e.category_id, c.name, c.title, e.command, e.title,
	       e.description, e.tags, e.example, e.source, e.level, e.favorite,
	       e.created_at, e.updated_at
	FROM entries e JOIN categories c ON c.id = e.category_id`

// list returns entries, optionally in one category and at one level.
// category "" means all categories; level 0 means all levels.
func (s *Store) list(category string, level, limit int) ([]Entry, error) {
	q := entryColumns + ` WHERE (? = '' OR c.name = ?) AND (? = 0 OR e.level = ?)
		ORDER BY c.name, e.command`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.Query(q, category, category, level, level)
	if err != nil {
		return nil, err
	}
	return scanEntries(rows)
}

// ftsMatchExpr builds an FTS5 MATCH expression: every token as a prefix
// term, ANDed together. Empty input returns "".
func ftsMatchExpr(query string) string {
	var parts []string
	for _, tok := range strings.Fields(query) {
		tok = strings.ReplaceAll(tok, `"`, "")
		tok = strings.Trim(tok, `'"`)
		if tok == "" {
			continue
		}
		parts = append(parts, `"`+tok+`"*`)
	}
	return strings.Join(parts, " AND ")
}

func (s *Store) likeSearch(query, category string, level, limit int) ([]Entry, error) {
	like := "%" + query + "%"
	q := entryColumns + ` WHERE (? = '' OR c.name = ?) AND (? = 0 OR e.level = ?)
		AND (e.command LIKE ? OR e.title LIKE ? OR e.description LIKE ?
		     OR e.tags LIKE ? OR e.example LIKE ?)
		ORDER BY c.name, e.command`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.Query(q, category, category, level, level, like, like, like, like, like)
	if err != nil {
		return nil, err
	}
	return scanEntries(rows)
}

// Search returns entries matching query with live FTS5 prefix search.
// An empty query lists entries instead. category "" means all categories,
// level 0 means all levels. limit <= 0 means no limit.
func (s *Store) Search(query, category string, level, limit int) ([]Entry, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return s.list(category, level, limit)
	}
	match := ftsMatchExpr(query)
	if match == "" {
		return s.likeSearch(query, category, level, limit)
	}
	q := `
		SELECT e.id, e.category_id, c.name, c.title, e.command, e.title,
		       e.description, e.tags, e.example, e.source, e.level, e.favorite,
		       e.created_at, e.updated_at
		FROM entries_fts f
		JOIN entries e ON e.id = f.rowid
		JOIN categories c ON c.id = e.category_id
		WHERE f MATCH ? AND (? = '' OR c.name = ?) AND (? = 0 OR e.level = ?)
		ORDER BY rank`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.Query(q, match, category, category, level, level)
	if err != nil {
		// Odd punctuation can break MATCH syntax; fall back to LIKE.
		return s.likeSearch(query, category, level, limit)
	}
	return scanEntries(rows)
}

// ListFavorites returns starred entries, most useful first.
func (s *Store) ListFavorites(limit int) ([]Entry, error) {
	q := entryColumns + ` WHERE e.favorite = 1 ORDER BY c.name, e.command`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	return scanEntries(rows)
}

// CountFavorites returns the number of starred entries.
func (s *Store) CountFavorites() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM entries WHERE favorite = 1`).Scan(&n)
	return n, err
}

// SetFavorite stars or unstars one entry. It reports whether the entry exists.
func (s *Store) SetFavorite(id int64, fav bool) (bool, error) {
	v := 0
	if fav {
		v = 1
	}
	res, err := s.db.Exec(`UPDATE entries SET favorite = ?, updated_at = datetime('now') WHERE id = ?`, v, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ImportSeedBytes parses one seed JSON document and upserts its entries
// as seed records. User records are never touched.
// It returns the numbers of inserted and updated seed entries.
func (s *Store) ImportSeedBytes(data []byte) (inserted, updated int, err error) {
	var seed SeedFile
	if err := json.Unmarshal(data, &seed); err != nil {
		return 0, 0, fmt.Errorf("parse seed data: %w", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	catIDs := make(map[string]int64, len(seed.Categories))
	for _, c := range seed.Categories {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO categories (name, title) VALUES (?, ?)`,
			c.Name, firstNonEmpty(c.Title, c.Name)); err != nil {
			return 0, 0, err
		}
		var id int64
		if err := tx.QueryRow(`SELECT id FROM categories WHERE name = ?`, c.Name).Scan(&id); err != nil {
			return 0, 0, err
		}
		catIDs[c.Name] = id
	}
	for _, e := range seed.Entries {
		catID, ok := catIDs[e.Category]
		if !ok {
			// Category not declared in the seed file: create it on the fly.
			if _, err := tx.Exec(`INSERT OR IGNORE INTO categories (name, title) VALUES (?, ?)`,
				e.Category, e.Category); err != nil {
				return 0, 0, err
			}
			if err := tx.QueryRow(`SELECT id FROM categories WHERE name = ?`, e.Category).Scan(&catID); err != nil {
				return 0, 0, err
			}
			catIDs[e.Category] = catID
		}
		title := firstNonEmpty(e.Title, e.Command)
		level := e.Level
		if level < LevelEssential || level > LevelAdvanced {
			level = LevelCommon
		}
		var id int64
		qerr := tx.QueryRow(
			`SELECT id FROM entries WHERE category_id = ? AND command = ? AND source = 'seed'`,
			catID, e.Command).Scan(&id)
		switch qerr {
		case nil:
			if _, err := tx.Exec(`
				UPDATE entries
				SET title = ?, description = ?, tags = ?, example = ?, level = ?,
				    updated_at = datetime('now')
				WHERE id = ?`,
				title, e.Description, e.Tags, e.Example, level, id); err != nil {
				return 0, 0, err
			}
			updated++
		case sql.ErrNoRows:
			if _, err := tx.Exec(`
				INSERT INTO entries (category_id, command, title, description, tags, example, source, level)
				VALUES (?, ?, ?, ?, ?, ?, 'seed', ?)`,
				catID, e.Command, title, e.Description, e.Tags, e.Example, level); err != nil {
				return 0, 0, err
			}
			inserted++
		default:
			return 0, 0, qerr
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return inserted, updated, nil
}

// ImportSeedDir imports every *.json seed file in dir, in file-name order.
func (s *Store) ImportSeedDir(dir string) (inserted, updated int, err error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return 0, 0, err
	}
	sort.Strings(names)
	if len(names) == 0 {
		return 0, 0, fmt.Errorf("no .json seed files in %s", dir)
	}
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			return inserted, updated, fmt.Errorf("read %s: %w", name, err)
		}
		ins, upd, err := s.ImportSeedBytes(data)
		if err != nil {
			return inserted, updated, fmt.Errorf("%s: %w", name, err)
		}
		inserted += ins
		updated += upd
	}
	return inserted, updated, nil
}

// IsSeeded reports whether the embedded seed was already imported once.
func (s *Store) IsSeeded() (bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'seeded'`).Scan(&v)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return v == "1", nil
}

// SeedVersion is bumped whenever the embedded seed changes in a way that
// existing databases must pick up (new columns, new levels, ...).
const SeedVersion = "2"

// EnsureSeeded imports the embedded seed data on first run, and re-imports
// it when the seed version changed (e.g. a v1.0.0 database gaining levels).
func (s *Store) EnsureSeeded(seed []byte) error {
	seeded, err := s.IsSeeded()
	if err != nil {
		return err
	}
	var v string
	verr := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'seed_version'`).Scan(&v)
	needsImport := !seeded || verr == sql.ErrNoRows || v != SeedVersion
	if verr != nil && verr != sql.ErrNoRows {
		return verr
	}
	if !needsImport {
		return nil
	}
	if _, _, err := s.ImportSeedBytes(seed); err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO meta (key, value) VALUES ('seeded', '1'), ('seed_version', ?)`,
		SeedVersion)
	return err
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
