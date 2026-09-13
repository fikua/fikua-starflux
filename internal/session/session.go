// Package session saves and restores a set of named star positions
// ("Guardar posiciones" / "Recuperar posiciones" in FotoDif's terms), so a
// session's star markers don't need to be re-clicked by hand every time.
package session

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/fikua/fikua-starflux/internal/ui"
)

// File is the on-disk representation of a saved set of star positions.
type File struct {
	Stars []ui.Star `json:"stars"`
}

// Save writes stars to path as indented JSON.
func Save(stars []ui.Star, path string) error {
	data, err := json.MarshalIndent(File{Stars: stars}, "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("session: write %s: %w", path, err)
	}
	return nil
}

// Load reads and parses a star-positions file written by Save.
func Load(path string) ([]ui.Star, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("session: read %s: %w", path, err)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("session: parse %s: %w", path, err)
	}
	return f.Stars, nil
}
