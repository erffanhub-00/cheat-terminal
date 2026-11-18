// Command cheat-terminal is a terminal cheat-sheet searcher: a fast,
// categorized SQLite database of commands with live suggestions while
// typing, a Bubbletea TUI, and a small CLI for searching and editing.
//
// Run with no arguments for the interactive browser; run
// `cheat-terminal --help` for the CLI commands.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/erffanhub-00/cheat-terminal/data"
	"github.com/erffanhub-00/cheat-terminal/internal/cli"
	"github.com/erffanhub-00/cheat-terminal/internal/db"
	"github.com/erffanhub-00/cheat-terminal/internal/tui"
)

// version is the release version, shown by --version.
const version = "1.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	dbPath := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--db" && i+1 < len(args) {
			dbPath = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	if dbPath == "" {
		dbPath = db.DefaultPath()
	}
	store, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cheat-terminal: %v\n", err)
		return 1
	}
	defer store.Close()
	if err := store.EnsureSeeded(data.SeedJSON); err != nil {
		fmt.Fprintf(os.Stderr, "cheat-terminal: seed database: %v\n", err)
		return 1
	}
	if len(rest) == 0 {
		if !isTerminal() {
			fmt.Fprintln(os.Stderr, "cheat-terminal: no terminal detected; use `cheat-terminal <query>` or --help")
			return 1
		}
		p := tea.NewProgram(tui.New(store, version), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "cheat-terminal: %v\n", err)
			return 1
		}
		return 0
	}
	return cli.Run(rest, store, version, dbPath)
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
