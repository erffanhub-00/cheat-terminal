package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
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

// searchTokens splits a query into lowercase alphanumeric tokens
// (unicode letters and digits), dropping all punctuation. Punctuation
// can never break the search this way: "docker ps -a" becomes
// [docker ps a], "ps | grep" becomes [ps grep], "a:b" becomes [a b].
func searchTokens(query string) []string {
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(query) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// normalizeQuery collapses a query to single spaces, lower-cased, for
// the whole-query match tiers in searchScore.
func normalizeQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

// ftsEntries returns the FTS5 prefix matches for every token (ANDed),
// with each entry's bm25 rank for tie-breaking. The FTS table is
// referenced by its real name: aliasing it breaks the MATCH operator
// (SQLite reports "no such column: f") and used to make every search
// silently fall back to a whole-phrase LIKE.
func (s *Store) ftsEntries(tokens []string, category string, level int) ([]Entry, map[int64]float64, error) {
	var parts []string
	for _, tok := range tokens {
		parts = append(parts, `"`+tok+`"*`)
	}
	rows, err := s.db.Query(`
		SELECT e.id, e.category_id, c.name, c.title, e.command, e.title,
		       e.description, e.tags, e.example, e.source, e.level, e.favorite,
		       e.created_at, e.updated_at, entries_fts.rank
		FROM entries_fts
		JOIN entries e ON e.id = entries_fts.rowid
		JOIN categories c ON c.id = e.category_id
		WHERE entries_fts MATCH ? AND (? = '' OR c.name = ?) AND (? = 0 OR e.level = ?)`,
		strings.Join(parts, " AND "), category, category, level, level)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Entry
	ranks := make(map[int64]float64)
	for rows.Next() {
		var e Entry
		var fav int
		var rank float64
		if err := rows.Scan(
			&e.ID, &e.CategoryID, &e.Category, &e.CategoryTitle, &e.Command, &e.Title,
			&e.Description, &e.Tags, &e.Example, &e.Source, &e.Level, &fav,
			&e.CreatedAt, &e.UpdatedAt, &rank); err != nil {
			return nil, nil, err
		}
		e.Favorite = fav != 0
		out = append(out, e)
		ranks[e.ID] = rank
	}
	return out, ranks, rows.Err()
}

// tokenLikeSearch returns entries where every token (anyToken=false) or
// any token (anyToken=true) appears as a substring in the command,
// title, description, tags or example. Tokens are alphanumeric only,
// so LIKE wildcards can never appear in them.
func (s *Store) tokenLikeSearch(tokens []string, category string, level int, anyToken bool) ([]Entry, error) {
	var conds []string
	args := []any{category, category, level, level}
	for _, tok := range tokens {
		like := "%" + tok + "%"
		conds = append(conds, `(e.command LIKE ? OR e.title LIKE ? OR e.description LIKE ? OR e.tags LIKE ? OR e.example LIKE ?)`)
		args = append(args, like, like, like, like, like)
	}
	joiner := " AND "
	if anyToken {
		joiner = " OR "
	}
	q := entryColumns + ` WHERE (? = '' OR c.name = ?) AND (? = 0 OR e.level = ?)
		AND (` + strings.Join(conds, joiner) + `)`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return scanEntries(rows)
}

// searchScore ranks an entry for a query. The whole-query tiers dominate,
// so the exact command always beats a description that merely mentions
// the words; token coverage then orders the rest, so a command matching
// more of the query outranks one matching less of it.
func searchScore(e Entry, query string, tokens []string) float64 {
	cmd := strings.ToLower(e.Command)
	title := strings.ToLower(e.Title)
	hay := strings.ToLower(e.Description + " " + e.Tags + " " + e.Example)
	score := 0.0
	switch {
	case cmd == query:
		score += 1000
	case strings.HasPrefix(cmd, query):
		score += 500
	case strings.Contains(cmd, query):
		score += 300
	}
	// The user types the command name first: an entry whose command
	// starts with the first word is very likely what they want, even
	// when a later word matches nothing (e.g. `echo "hi"` should show
	// echo commands, not "history").
	if len(tokens) > 0 && strings.HasPrefix(cmd, tokens[0]) {
		score += 150
	}
	for _, tok := range tokens {
		if strings.Contains(cmd, tok) {
			score += 100
		}
		if strings.Contains(title, tok) {
			score += 20
		}
		if strings.Contains(hay, tok) {
			score += 10
		}
	}
	return score
}

// Search returns entries matching query, best match first. An empty
// query lists entries instead. category "" means all categories, level
// 0 means all levels. limit <= 0 means no limit.
//
// How it works: the query is split into alphanumeric tokens, so
// punctuation can never break it. Candidates come from FTS5 prefix
// matching (a partial last word matches while typing) plus substring
// matching (a mid-word fragment matches too); if nothing matches at
// all, any single token may match, so typing a real word plus a word
// the database does not know still shows the real word's entries.
// Candidates are then ranked in Go: exact command, command prefix,
// command substring, token coverage — with the FTS bm25 rank breaking
// ties. A query that is only punctuation is searched literally.
func (s *Store) Search(query, category string, level, limit int) ([]Entry, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return s.list(category, level, limit)
	}
	tokens := searchTokens(query)
	if len(tokens) == 0 {
		return s.likeSearch(query, category, level, limit)
	}
	byID := make(map[int64]Entry)
	ranks := make(map[int64]float64)
	if fts, r, err := s.ftsEntries(tokens, category, level); err == nil {
		for _, e := range fts {
			byID[e.ID] = e
		}
		ranks = r
	}
	// Substring matches: every token somewhere in the entry.
	like, err := s.tokenLikeSearch(tokens, category, level, false)
	if err != nil {
		return nil, err
	}
	for _, e := range like {
		byID[e.ID] = e
	}
	if len(byID) == 0 {
		// Nothing matched every token: loosen to any token, so the
		// search still shows something relevant instead of an
		// empty list.
		any, err := s.tokenLikeSearch(tokens, category, level, true)
		if err != nil {
			return nil, err
		}
		for _, e := range any {
			byID[e.ID] = e
		}
	}
	out := make([]Entry, 0, len(byID))
	for _, e := range byID {
		out = append(out, e)
	}
	norm := normalizeQuery(query)
	sort.Slice(out, func(i, j int) bool {
		si, sj := searchScore(out[i], norm, tokens), searchScore(out[j], norm, tokens)
		if si != sj {
			return si > sj
		}
		ri, iok := ranks[out[i].ID]
		rj, jok := ranks[out[j].ID]
		if iok && jok && ri != rj {
			return ri < rj // lower bm25 rank is more relevant
		}
		if iok != jok {
			return iok // FTS matches before LIKE-only matches
		}
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Command < out[j].Command
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
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
