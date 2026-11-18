// Package cli implements the non-interactive mode of cheat-terminal:
// direct search, listing, and database editing from the command line.
// Running the program with no arguments starts the interactive TUI instead.
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/erffanhub-00/cheat-terminal/internal/db"
)

// Run executes one CLI command and returns an exit code.
func Run(args []string, store *db.Store, version, dbPath string) int {
	if len(args) == 0 {
		return 0
	}
	// --level <name> is accepted anywhere (search, --list, --favorites).
	level := db.LevelAll
	if v, ok := flags(args)["level"]; ok {
		l, err := db.ParseLevel(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid --level: %v\n", err)
			return 1
		}
		level = l
	}
	switch args[0] {
	case "--help", "-h", "help":
		printUsage()
		return 0
	case "--version", "-v":
		fmt.Printf("cheat-terminal %s\n", version)
		return 0
	case "--list":
		cat := firstPositional(withoutLevel(args[1:]))
		if cat != "" {
			return listCategory(store, cat, level)
		}
		return listCategories(store)
	case "--favorites":
		return listFavorites(store, level)
	case "favorite":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cheat-terminal favorite <id>")
			return 1
		}
		return setFavorite(store, args[1], true)
	case "unfavorite":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cheat-terminal unfavorite <id>")
			return 1
		}
		return setFavorite(store, args[1], false)
	case "--import":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cheat-terminal --import <dir>")
			return 1
		}
		ins, upd, err := store.ImportSeedDir(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "import failed: %v\n", err)
			return 1
		}
		fmt.Printf("Imported %d new, updated %d seed entries from %s\n", ins, upd, args[1])
		return 0
	case "export":
		return exportEntries(store, args[1:])
	case "import":
		return importEntries(store, args[1:])
	case "add":
		return addEntry(store, args[1:])
	case "edit":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cheat-terminal edit <id>")
			return 1
		}
		return editEntry(store, args[1], args[2:])
	case "delete":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cheat-terminal delete <id>")
			return 1
		}
		return deleteEntry(store, args[1])
	case "category":
		if len(args) >= 3 && args[1] == "add" {
			return addCategory(store, args[2], args[3:])
		}
		fmt.Fprintln(os.Stderr, "usage: cheat-terminal category add <name> [--title <title>]")
		return 1
	default:
		// Everything else is a search query.
		return search(store, strings.Join(withoutLevel(args), " "), level)
	}
}

// withoutLevel returns args with the "--level <value>" pair removed, so a
// search query keeps working even when a level filter is given.
func withoutLevel(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--level" && i+1 < len(args) {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// firstPositional returns the first argument that is not a flag.
func firstPositional(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

func printUsage() {
	fmt.Print(`cheat-terminal - search your cheat sheets from the terminal

Usage:
  cheat-terminal                    Start the interactive browser (TUI)
  cheat-terminal <query>            Print matching entries
  cheat-terminal <query> --level essential|common|advanced
                                    Search only at one usefulness level
  cheat-terminal --list             List categories with entry counts
  cheat-terminal --list <category> [--level L]
                                    List all entries in a category
  cheat-terminal --favorites [--level L]
                                    List starred entries
  cheat-terminal favorite <id>      Star an entry
  cheat-terminal unfavorite <id>    Unstar an entry
  cheat-terminal add                Add an entry (interactive prompts)
  cheat-terminal add --command <cmd> [--title T] [--description D]
                     [--tags T] [--example E] [--category C] [--level L]
  cheat-terminal edit <id>          Edit an entry (prompts, Enter keeps)
  cheat-terminal delete <id>        Delete an entry (asks first)
  cheat-terminal category add <name> [--title T]
  cheat-terminal export [file] [--category C]
                                    Export entries as seed-format JSON
                                    (default file cheats-export.json)
  cheat-terminal import <file> [--category C]
                                    Import a seed-format JSON file: new
                                    commands are added, existing ones are
                                    updated only when the file has fuller
                                    data; without --category each entry
                                    goes to its own category
  cheat-terminal --import <dir>     Import *.json seed files from a directory
  cheat-terminal --db <path>        Use a different database file
  cheat-terminal --version          Print the version

TUI keys: type to search, Up/Down move, Tab switch category,
Enter copy command, s star favorite, f filter by usefulness,
E export entries to a file, I import entries from a file,
Esc once switches to list mode (where the keys work), ? help,
q quit, Ctrl+C quit.
`)
}

func listCategories(store *db.Store) int {
	cats, err := store.ListCategories()
	if err != nil {
		fmt.Fprintf(os.Stderr, "list categories: %v\n", err)
		return 1
	}
	total := 0
	for _, c := range cats {
		fmt.Printf("%-12s %4d  %s\n", c.Name, c.Count, c.Title)
		total += c.Count
	}
	fmt.Printf("%-12s %4d  %s\n", "all", total, "All categories")
	return 0
}

func listCategory(store *db.Store, name string, level int) int {
	entries, err := store.Search("", name, level, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list %s: %v\n", name, err)
		return 1
	}
	if len(entries) == 0 {
		fmt.Printf("No entries in category %q.\n", name)
		return 0
	}
	printEntries(entries)
	return 0
}

func listFavorites(store *db.Store, level int) int {
	entries, err := store.ListFavorites(0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list favorites: %v\n", err)
		return 1
	}
	if level != db.LevelAll {
		var kept []db.Entry
		for _, e := range entries {
			if e.Level == level {
				kept = append(kept, e)
			}
		}
		entries = kept
	}
	if len(entries) == 0 {
		fmt.Println("No favorites yet. Star entries with the 's' key in the TUI,")
		fmt.Println("or with: cheat-terminal favorite <id>")
		return 0
	}
	printEntries(entries)
	return 0
}

func setFavorite(store *db.Store, idArg string, fav bool) int {
	id, err := strconv.ParseInt(idArg, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "favorite: %q is not a numeric id\n", idArg)
		return 1
	}
	e, err := store.GetEntry(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "favorite: no entry #%d\n", id)
		return 1
	}
	if _, err := store.SetFavorite(id, fav); err != nil {
		fmt.Fprintf(os.Stderr, "favorite: %v\n", err)
		return 1
	}
	if fav {
		fmt.Printf("★ Starred entry #%d: %s\n", e.ID, e.Command)
	} else {
		fmt.Printf("Unstarred entry #%d: %s\n", e.ID, e.Command)
	}
	return 0
}

func search(store *db.Store, query string, level int) int {
	entries, err := store.Search(query, "", level, 20)
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %v\n", err)
		return 1
	}
	if len(entries) == 0 {
		fmt.Printf("No matches for %q.\n", query)
		return 0
	}
	printEntries(entries)
	return 0
}

// printEntries prints one entry per two lines: id, category, command and
// usefulness level, then the description indented. On a terminal the
// command is bold and the description dim, so the output reads like the
// TUI; piped output stays plain.
func printEntries(entries []db.Entry) {
	tty := isTerminal()
	for _, e := range entries {
		cmd := e.Command
		if tty {
			cmd = "\x1b[1m" + cmd + "\x1b[0m"
		}
		line := fmt.Sprintf("[%d] %s — %s", e.ID, e.Category, cmd)
		if e.Favorite {
			line = "★ " + line
		}
		level := " · " + db.LevelName(e.Level)
		if tty {
			level = "\x1b[2m" + level + "\x1b[0m"
		}
		fmt.Println(line + level)
		if strings.TrimSpace(e.Description) != "" {
			desc := truncate(e.Description, 100)
			if tty {
				desc = "\x1b[2m" + desc + "\x1b[0m"
			}
			fmt.Printf("     %s\n", desc)
		}
	}
}

// isTerminal reports whether stdout is a terminal.
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// stripFlagPair removes a "--name value" pair from args and returns the
// remaining args and the value ("" if absent).
func stripFlagPair(args []string, name string) ([]string, string) {
	var rest []string
	value := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--"+name && i+1 < len(args) {
			value = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	return rest, value
}

// normalizeCategory lower-cases a category name so "Docker" matches the
// existing "docker" category instead of creating a duplicate.
func normalizeCategory(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func exportEntries(store *db.Store, args []string) int {
	rest, cat := stripFlagPair(args, "category")
	file := firstPositional(rest)
	if file == "" {
		file = "cheats-export.json"
	}
	n, err := store.ExportToFile(file, normalizeCategory(cat))
	if err != nil {
		fmt.Fprintf(os.Stderr, "export: %v\n", err)
		return 1
	}
	if cat != "" {
		fmt.Printf("Exported %d entries from category %q to %s\n", n, cat, file)
	} else {
		fmt.Printf("Exported %d entries to %s\n", n, file)
	}
	return 0
}

func importEntries(store *db.Store, args []string) int {
	rest, cat := stripFlagPair(args, "category")
	file := firstPositional(rest)
	if file == "" {
		fmt.Fprintln(os.Stderr, "usage: cheat-terminal import <file> [--category C]")
		return 1
	}
	stats, err := store.ImportFile(file, normalizeCategory(cat))
	if err != nil {
		fmt.Fprintf(os.Stderr, "import: %v\n", err)
		return 1
	}
	if cat != "" {
		fmt.Printf("Imported %d new, %d updated, %d unchanged into category %q from %s\n",
			stats.New, stats.Updated, stats.Unchanged, cat, file)
	} else {
		fmt.Printf("Imported %d new, %d updated, %d unchanged from %s\n",
			stats.New, stats.Updated, stats.Unchanged, file)
	}
	return 0
}

// flags parses --name value pairs out of args.
func flags(args []string) map[string]string {
	out := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") && i+1 < len(args) {
			out[strings.TrimPrefix(args[i], "--")] = args[i+1]
			i++
		}
	}
	return out
}

var stdinReader = bufio.NewReader(os.Stdin)

// prompt asks one question; an empty answer keeps def.
func prompt(label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := stdinReader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func addEntry(store *db.Store, args []string) int {
	f := flags(args)
	e := db.Entry{
		Command:     f["command"],
		Title:       f["title"],
		Description: f["description"],
		Tags:        f["tags"],
		Example:     f["example"],
		Category:    f["category"],
	}
	if v, ok := f["level"]; ok {
		l, err := db.ParseLevel(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "add: %v\n", err)
			return 1
		}
		e.Level = l
	}
	if e.Command == "" {
		fmt.Println("Add a new entry (Enter keeps the default):")
		e.Command = prompt("Command", "")
		for strings.TrimSpace(e.Command) == "" {
			fmt.Println("Command is required.")
			e.Command = prompt("Command", "")
		}
		e.Title = prompt("Title", e.Command)
		e.Description = prompt("Description", "")
		e.Tags = prompt("Tags (comma separated)", "")
		e.Example = prompt("Example", "")
		e.Category = prompt("Category", "general")
		l, err := db.ParseLevel(prompt("Level (essential/common/advanced)", "common"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "add: %v\n", err)
			return 1
		}
		e.Level = l
	}
	if e.Title == "" {
		e.Title = e.Command
	}
	if e.Category == "" {
		e.Category = "general"
	}
	id, err := store.AddEntry(e)
	if err != nil {
		fmt.Fprintf(os.Stderr, "add: %v\n", err)
		return 1
	}
	fmt.Printf("Added entry #%d to category %q.\n", id, e.Category)
	return 0
}

func editEntry(store *db.Store, idArg string, args []string) int {
	id, err := strconv.ParseInt(idArg, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "edit: %q is not a numeric id\n", idArg)
		return 1
	}
	e, err := store.GetEntry(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "edit: no entry #%d\n", id)
		return 1
	}
	f := flags(args)
	if len(f) > 0 {
		if v, ok := f["command"]; ok {
			e.Command = v
		}
		if v, ok := f["title"]; ok {
			e.Title = v
		}
		if v, ok := f["description"]; ok {
			e.Description = v
		}
		if v, ok := f["tags"]; ok {
			e.Tags = v
		}
		if v, ok := f["example"]; ok {
			e.Example = v
		}
		if v, ok := f["category"]; ok {
			e.Category = v
		}
		if v, ok := f["level"]; ok {
			l, err := db.ParseLevel(v)
			if err != nil {
				fmt.Fprintf(os.Stderr, "edit: %v\n", err)
				return 1
			}
			e.Level = l
		}
	} else {
		fmt.Printf("Editing entry #%d (Enter keeps the current value):\n", id)
		e.Command = prompt("Command", e.Command)
		e.Title = prompt("Title", e.Title)
		e.Description = prompt("Description", e.Description)
		e.Tags = prompt("Tags (comma separated)", e.Tags)
		e.Example = prompt("Example", e.Example)
		e.Category = prompt("Category", e.Category)
		l, err := db.ParseLevel(prompt("Level (essential/common/advanced)", db.LevelName(e.Level)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "edit: %v\n", err)
			return 1
		}
		e.Level = l
	}
	if strings.TrimSpace(e.Command) == "" {
		fmt.Fprintln(os.Stderr, "edit: command cannot be empty")
		return 1
	}
	if err := store.UpdateEntry(e); err != nil {
		fmt.Fprintf(os.Stderr, "edit: %v\n", err)
		return 1
	}
	fmt.Printf("Updated entry #%d.\n", id)
	return 0
}

func deleteEntry(store *db.Store, idArg string) int {
	id, err := strconv.ParseInt(idArg, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "delete: %q is not a numeric id\n", idArg)
		return 1
	}
	e, err := store.GetEntry(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "delete: no entry #%d\n", id)
		return 1
	}
	fmt.Printf("[%d] %s — %s\n", e.ID, e.Category, e.Command)
	if strings.TrimSpace(e.Description) != "" {
		fmt.Printf("     %s\n", e.Description)
	}
	answer := prompt("Delete this entry? [y/N]", "n")
	if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
		fmt.Println("Kept.")
		return 0
	}
	if _, err := store.DeleteEntry(id); err != nil {
		fmt.Fprintf(os.Stderr, "delete: %v\n", err)
		return 1
	}
	fmt.Printf("Deleted entry #%d.\n", id)
	return 0
}

func addCategory(store *db.Store, name string, args []string) int {
	f := flags(args)
	title := firstNonEmpty(f["title"], name)
	_, existed, err := store.AddCategory(name, title)
	if err != nil {
		fmt.Fprintf(os.Stderr, "category add: %v\n", err)
		return 1
	}
	if existed {
		fmt.Printf("Category %q already exists.\n", name)
		return 0
	}
	fmt.Printf("Added category %q.\n", name)
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
