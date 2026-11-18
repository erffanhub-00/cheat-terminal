# cheat-terminal

Search your cheat sheets from the terminal. A fast, categorized SQLite
database of commands with live suggestions while you type, a full-screen
browser, and a small CLI for searching and editing.

- **1392 commands** seeded from day one: Linux, PowerShell, Docker, Git
- **Live search** with full-text suggestions as you type
- **A real browser**: category sidebar, live results, and a detail pane
  showing the selected command in full — two-line rows with room to
  breathe, and every category in its own color
- **Copy** any command to the clipboard with one key (Enter)
- **Filter by usefulness**: Essential, Common or Advanced — every command
  is rated, so you can see only the ones that matter
- **Favorites**: star the commands you use most, find them in one place
- **Your own entries**: add them inside the browser with `a` (step by
  step), make your own categories with `C`, delete with `d`; edit and
  delete from the CLI too. Seed updates never touch them
- **One static binary, one database file** (`cheats.db`), no install, no network

## Install

Download the binary for your OS from the [Releases](../../releases) page
(Linux, Windows, macOS), or build it yourself:

```sh
git clone https://github.com/erffanhub-00/cheat-terminal.git
cd cheat-terminal
go build -o cheat-terminal .
```

Requires Go 1.25 or newer to build. The binary needs nothing else at
runtime: no CGO, no config files, no network.

## Use

Run it with no arguments for the interactive browser:

```sh
./cheat-terminal
```

Keys:

- Type to search, Up/Down move, Tab switch category
- Enter copies the selected command to the clipboard
- Esc switches to list mode (press again to go back to search)
- In list mode: `c` copy, `s` star/unstar a favorite,
  `f` filter by usefulness (All → Essential → Common → Advanced),
  `a` add a new entry (Command → Description → Category → Tags → Level;
  typing a new category name creates it),
  `C` add a new category, `d` delete the selected entry (asks first),
  `/` back to search, `q` quit
- `?` shows all shortcuts, Ctrl+C quits

Or use it from the command line:

```sh
./cheat-terminal docker ps        # print matching entries
./cheat-terminal docker ps --level essential   # only essential commands
./cheat-terminal --list           # categories with entry counts
./cheat-terminal --list docker    # all Docker entries
./cheat-terminal --favorites      # your starred entries
./cheat-terminal favorite 42      # star entry #42
./cheat-terminal unfavorite 42    # unstar entry #42
./cheat-terminal add              # add your own entry (prompts)
./cheat-terminal edit 42          # edit entry #42
./cheat-terminal delete 42        # delete entry #42 (asks first)
./cheat-terminal category add kubernetes
./cheat-terminal --import data/   # import seed JSON files
./cheat-terminal --help           # everything
```

## Database

The first run creates `cheats.db` next to the binary (or in the user config
directory when that folder is not writable) and fills it with the seed
data: 1392 entries in 4 categories. Change the location with
`--db <path>` or the `CHEAT_TERMINAL_DB` environment variable.

Databases created by v1.0.0 upgrade themselves on the first v1.1.0 run:
new columns are added and the seed levels are filled in. Your own entries
and favorites are kept. v1.2.0 needs no database changes: a v1.1.0
`cheats.db` just works, and your entries and categories are kept.

## Seed data

Seed entries live in `data/seed.json`, generated from the English cheat
sheets (Git, Linux, PowerShell, Docker) by `tools/generate-seed.py`:

```sh
python3 tools/generate-seed.py
./cheat-terminal --import data/
```

Importing only upserts seed records. Your own entries are stored
separately (`source = 'user'`) and are never changed or deleted by an
import.

## Development

```sh
go test ./...   # unit tests (database, search, CRUD, import, TUI)
go vet ./...    # static checks
```

CI runs vet, tests, and cross-compiles Linux, Windows and macOS binaries
from `.github/workflows/ci.yml`. (The GitHub App used for pushes cannot
create workflow files, so that file is managed on the website.)

## License

MIT — see [LICENSE](LICENSE).

## Me
[![Telegram](https://img.shields.io/badge/Telegram-erffan__hub-blue?style=for-the-badge&logo=telegram)](https://t.me/erffan_hub)
[![Twitter](https://img.shields.io/badge/Twitter-@Erffanhub__00-000000?style=for-the-badge&logo=x&logoColor=white)](https://x.com/Erffanhub_00)
[![Gist](https://img.shields.io/badge/Gist-Profile-000000?style=for-the-badge&logo=github)](https://gist.github.com/erffanhub-00)
[![Github](https://img.shields.io/badge/Github-Profile-000000?style=for-the-badge&logo=github)](https://github.com/erffanhub-00)
