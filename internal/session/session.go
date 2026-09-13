// Package session saves and restores a set of named star positions
// ("Guardar posiciones" / "Recuperar posiciones" in FotoDif's terms), so a
// session's star markers don't need to be re-clicked by hand every time.
package session

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/fikua/fikua-starflux/internal/timeseries"
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

// FullSession is a richer, session-scoped save that captures everything
// needed to restore the app to the exact state it was in when the user
// last processed a series — star positions AND every target's accumulated
// light-curve points — matching FotoDif's "Guardar/Recuperar datos"
// semantics ("dejará el programa en el mismo estado que cuando se
// guardaron"). This is distinct from File (star positions only), which
// remains available unchanged via Save/Load for users who only want to
// reuse star placements across nights without carrying forward any
// measurements.
type FullSession struct {
	Stars  []ui.Star                     `json:"stars"`
	Points map[string][]timeseries.Point `json:"points"`
}

// SaveFull writes stars and accumulated per-target points as one JSON
// session file.
func SaveFull(stars []ui.Star, points map[string][]timeseries.Point, path string) error {
	data, err := json.MarshalIndent(FullSession{Stars: stars, Points: points}, "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal full session: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("session: write %s: %w", path, err)
	}
	return nil
}

// LoadFull reads a full session (star positions and accumulated points)
// from path. Loading a star-positions-only file (written by Save) degrades
// gracefully here: it unmarshals successfully with points == nil, exactly
// as if a fresh session had been started with those star positions
// restored — this is deliberate, not an error case.
func LoadFull(path string) (stars []ui.Star, points map[string][]timeseries.Point, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("session: read %s: %w", path, err)
	}
	var f FullSession
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, nil, fmt.Errorf("session: parse %s: %w", path, err)
	}
	return f.Stars, f.Points, nil
}
