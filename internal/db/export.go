// Export and import in the seed-file shape.
//
// Export writes the database as JSON in exactly the shape of
// data/seed.json ({"version": 1, "categories": [...], "entries": [...]})
// so the user can hand it to an AI, have descriptions enriched and
// missing commands added, and import it back. Import has enrichment
// semantics: a command that already exists in the target category is
// updated only when the incoming data is fuller (a non-empty description
// longer than the stored one, non-empty tags, a level of 1-3); otherwise
// it counts as unchanged. New commands are inserted as user entries, so
// seed re-imports never touch them.
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ImportStats counts the outcome of an import: entries that were new,
// entries that were updated in place, and entries that matched but
// needed no change.
type ImportStats struct {
	New       int
	Updated   int
	Unchanged int
}

// ExportAll returns every entry in the seed-file shape.
func (s *Store) ExportAll() (SeedFile, error) {
	cats, err := s.ListCategories()
	if err != nil {
		return SeedFile{}, err
	}
	entries, err := s.list("", 0, 0)
	if err != nil {
		return SeedFile{}, err
	}
	out := SeedFile{Version: 1}
	for _, c := range cats {
		out.Categories = append(out.Categories, SeedCategory{Name: c.Name, Title: c.Title})
	}
	for _, e := range entries {
		out.Entries = append(out.Entries, toSeedEntry(e))
	}
	return out, nil
}

// ExportCategory returns one category and its entries. An unknown
// category is an error.
func (s *Store) ExportCategory(name string) (SeedFile, error) {
	cats, err := s.ListCategories()
	if err != nil {
		return SeedFile{}, err
	}
	var found *Category
	for i := range cats {
		if cats[i].Name == name {
			found = &cats[i]
			break
		}
	}
	if found == nil {
		return SeedFile{}, fmt.Errorf("unknown category %q", name)
	}
	entries, err := s.list(name, 0, 0)
	if err != nil {
		return SeedFile{}, err
	}
	out := SeedFile{Version: 1, Categories: []SeedCategory{{Name: found.Name, Title: found.Title}}}
	for _, e := range entries {
		out.Entries = append(out.Entries, toSeedEntry(e))
	}
	return out, nil
}

func toSeedEntry(e Entry) SeedEntry {
	return SeedEntry{
		Category:    e.Category,
		Command:     e.Command,
		Title:       e.Title,
		Description: e.Description,
		Tags:        e.Tags,
		Example:     e.Example,
		Level:       e.Level,
	}
}

// ExportToFile writes the export (all entries, or one category) as
// indented JSON in the seed-file shape. It returns the entry count.
// Favorites are not exported: the seed format has no field for them.
func (s *Store) ExportToFile(path, category string) (int, error) {
	var seed SeedFile
	var err error
	if category == "" {
		seed, err = s.ExportAll()
	} else {
		seed, err = s.ExportCategory(category)
	}
	if err != nil {
		return 0, err
	}
	data, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		return 0, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return 0, fmt.Errorf("write %s: %w", path, err)
	}
	return len(seed.Entries), nil
}

// ImportFile reads a seed-format JSON file and imports it. With a target
// category, every entry goes into that category (created if needed);
// without one, each entry goes to the category named in the file
// (created if needed).
func (s *Store) ImportFile(path, targetCategory string) (ImportStats, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ImportStats{}, fmt.Errorf("read %s: %w", path, err)
	}
	return s.ImportBytes(data, targetCategory)
}

// ImportBytes imports one seed-format JSON document with enrichment
// semantics: a command that already exists in the target category is
// updated only when the incoming data is fuller (a non-empty description
// longer than the stored one, non-empty tags, a level of 1-3); otherwise
// it counts as unchanged. New commands are inserted as user entries.
// Missing fields are tolerated: description "", tags "", level 0 (which
// means "not specified": new entries default to Common and updates leave
// the level alone). The whole import runs in one transaction.
func (s *Store) ImportBytes(data []byte, targetCategory string) (stats ImportStats, err error) {
	var seed SeedFile
	if err = json.Unmarshal(data, &seed); err != nil {
		return stats, fmt.Errorf("parse import data: %w", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return stats, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	// Titles declared in the file, for categories created on the fly.
	fileTitles := make(map[string]string, len(seed.Categories))
	for _, c := range seed.Categories {
		fileTitles[c.Name] = firstNonEmpty(c.Title, c.Name)
		if _, err := tx.Exec(`INSERT OR IGNORE INTO categories (name, title) VALUES (?, ?)`,
			c.Name, fileTitles[c.Name]); err != nil {
			return stats, err
		}
	}
	catIDs := make(map[string]int64)
	ensureCat := func(name string) (int64, error) {
		if id, ok := catIDs[name]; ok {
			return id, nil
		}
		title := fileTitles[name]
		if title == "" {
			title = displayTitle(name)
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO categories (name, title) VALUES (?, ?)`,
			name, title); err != nil {
			return 0, err
		}
		var id int64
		if err := tx.QueryRow(`SELECT id FROM categories WHERE name = ?`, name).Scan(&id); err != nil {
			return 0, err
		}
		catIDs[name] = id
		return id, nil
	}
	for _, e := range seed.Entries {
		cmd := strings.TrimSpace(e.Command)
		if cmd == "" {
			continue
		}
		catName := targetCategory
		if catName == "" {
			catName = strings.TrimSpace(e.Category)
			if catName == "" {
				catName = "general"
			}
		}
		catID, err := ensureCat(catName)
		if err != nil {
			return stats, err
		}
		var id int64
		var curDesc, curTags string
		var curLevel int
		qerr := tx.QueryRow(
			`SELECT id, description, tags, level FROM entries WHERE category_id = ? AND command = ?`,
			catID, cmd).Scan(&id, &curDesc, &curTags, &curLevel)
		switch qerr {
		case nil:
			// Existing command: update only where incoming data is fuller.
			newDesc, newTags, newLevel := curDesc, curTags, curLevel
			changed := false
			if d := strings.TrimSpace(e.Description); d != "" &&
				(strings.TrimSpace(curDesc) == "" || len([]rune(d)) > len([]rune(curDesc))) {
				newDesc = d
				changed = true
			}
			if t := strings.TrimSpace(e.Tags); t != "" && t != curTags {
				newTags = t
				changed = true
			}
			if e.Level >= LevelEssential && e.Level <= LevelAdvanced && e.Level != curLevel {
				newLevel = e.Level
				changed = true
			}
			if !changed {
				stats.Unchanged++
				continue
			}
			if _, err := tx.Exec(`
				UPDATE entries
				SET description = ?, tags = ?, level = ?, updated_at = datetime('now')
				WHERE id = ?`, newDesc, newTags, newLevel, id); err != nil {
				return stats, err
			}
			stats.Updated++
		case sql.ErrNoRows:
			level := e.Level
			if level < LevelEssential || level > LevelAdvanced {
				level = LevelCommon
			}
			if _, err := tx.Exec(`
				INSERT INTO entries (category_id, command, title, description, tags, example, source, level)
				VALUES (?, ?, ?, ?, ?, ?, 'user', ?)`,
				catID, cmd, firstNonEmpty(e.Title, cmd), e.Description, e.Tags, e.Example, level); err != nil {
				return stats, err
			}
			stats.New++
		default:
			return stats, qerr
		}
	}
	if err := tx.Commit(); err != nil {
		return stats, err
	}
	return stats, nil
}

// displayTitle turns a category name into a display title: first letter
// upper-case, the rest as typed ("kubernetes" -> "Kubernetes").
func displayTitle(name string) string {
	if name == "" {
		return ""
	}
	r := []rune(name)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}
