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
	switch args[0] {
	case "--help", "-h", "help":
		printUsage()
		return 0
	case "--version", "-v":
		fmt.Printf("cheat-terminal %s\n", version)
		return 0
	case "--list":
		if len(args) > 1 {
			return listCategory(store, args[1])
		}
		return listCategories(store)
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
		return search(store, strings.Join(args, " "))
	}
}

func printUsage() {
	fmt.Print(`cheat-terminal - search your cheat sheets from the terminal

Usage:
  cheat-terminal                    Start the interactive browser (TUI)
  cheat-terminal <query>            Print matching entries
  cheat-terminal --list             List categories with entry counts
  cheat-terminal --list <category>  List all entries in a category
  cheat-terminal add                Add an entry (interactive prompts)
  cheat-terminal add --command <cmd> [--title T] [--description D]
                     [--tags T] [--example E] [--category C]
  cheat-terminal edit <id>          Edit an entry (prompts, Enter keeps)
  cheat-terminal delete <id>        Delete an entry (asks first)
  cheat-terminal category add <name> [--title T]
  cheat-terminal --import <dir>     Import *.json seed files from a directory
  cheat-terminal --db <path>        Use a different database file
  cheat-terminal --version          Print the version

TUI keys: type to search, Up/Down move, Tab switch category,
Enter details, c copy command, q quit, Esc back, Ctrl+C quit.
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

func listCategory(store *db.Store, name string) int {
	entries, err := store.Search("", name, 0)
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

func search(store *db.Store, query string) int {
	entries, err := store.Search(query, "", 20)
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

// printEntries prints one entry per two lines: id, category, command,
// then the description indented.
func printEntries(entries []db.Entry) {
	for _, e := range entries {
		fmt.Printf("[%d] %s — %s\n", e.ID, e.Category, e.Command)
		if strings.TrimSpace(e.Description) != "" {
			fmt.Printf("     %s\n", truncate(e.Description, 100))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
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
	} else {
		fmt.Printf("Editing entry #%d (Enter keeps the current value):\n", id)
		e.Command = prompt("Command", e.Command)
		e.Title = prompt("Title", e.Title)
		e.Description = prompt("Description", e.Description)
		e.Tags = prompt("Tags (comma separated)", e.Tags)
		e.Example = prompt("Example", e.Example)
		e.Category = prompt("Category", e.Category)
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
