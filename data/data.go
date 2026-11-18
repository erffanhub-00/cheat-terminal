// Package data embeds the seed cheat-sheet data (data/seed.json) into the
// binary, so a fresh database is complete from day one with no extra files
// and no network needed at runtime. The seed is generated from Erfan's
// English cheat sheets by tools/generate-seed.py.
package data

import _ "embed"

//go:embed seed.json
var SeedJSON []byte
