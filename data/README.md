# Seed data for cheat-terminal

`seed.json` is the seed database: every entry the program ships with on
first run (1392 entries in 4 categories: Linux, PowerShell, Docker, Git).

It is generated from Erfan's English cheat sheets by
`tools/generate-seed.py` — edit the cheat sheets, regenerate, and
re-import:

```sh
python3 tools/generate-seed.py
./cheat-terminal --import data/
```

The file is embedded in the binary (`data/data.go`), so a fresh database
is complete with no extra files and no network.

Format: `{"version": 1, "categories": [...], "entries": [...]}`. Each
entry has `category`, `command`, `title`, `description`, `tags`,
`example` and `level` (1 Essential, 2 Common, 3 Advanced — the
usefulness rating used by the TUI and CLI filters). Importing upserts
seed records only; user records are never touched.
