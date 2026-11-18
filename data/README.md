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

The same format is used by `cheat-terminal export` and
`cheat-terminal import`: export writes it, import reads it back with
enrichment semantics (a command already in the target category is only
updated when the file has fuller data — a longer description, tags, a
level of 1-3 — and the import reports new / updated / unchanged
counts). Favorites are not exported: the format has no field for them.
