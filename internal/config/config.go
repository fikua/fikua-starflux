// Package config holds Starflux's user-editable measurement parameters
// (centroid box size, aperture/annulus radii, field-shift tolerance),
// persisted across app restarts, mirroring FotoDif's "Configuración" /
// "Fotometría" dialog. Unlike internal/session, this is a single
// auto-loaded/auto-saved app-wide setting, not a per-observation artifact
// the user explicitly picks a file for.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fikua/fikua-starflux/internal/photometry"
)

// Config holds every user-tunable measurement parameter. Obtain one via
// Default() or Load()/LoadOrDefault(), rather than using a zero value.
type Config struct {
	CentroidHalfWidth int                 `json:"centroidHalfWidth"`
	Aperture          photometry.Aperture `json:"aperture"`
	ToleranceLevel    int                 `json:"toleranceLevel"`
}

// Default returns Starflux's built-in defaults, matching the constants that
// previously lived in cmd/starflux/main.go.
func Default() Config {
	return Config{
		CentroidHalfWidth: 10,
		Aperture:          photometry.Aperture{R: 6, RIn: 10, ROut: 15},
		ToleranceLevel:    7,
	}
}

// Path returns the OS-appropriate config file path
// (os.UserConfigDir()/starflux/config.json), creating the "starflux"
// directory if it doesn't yet exist.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: locate user config dir: %w", err)
	}
	appDir := filepath.Join(dir, "starflux")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", fmt.Errorf("config: create %s: %w", appDir, err)
	}
	return filepath.Join(appDir, "config.json"), nil
}

// Save writes cfg as JSON to path.
func Save(cfg Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}

// Load reads Config JSON from path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return cfg, nil
}

// LoadOrDefault loads Config from its standard path, falling back silently
// to Default() if the file doesn't exist yet or fails to load — a missing
// config file is the expected first-run state, not an error worth
// surfacing to the user.
func LoadOrDefault() Config {
	path, err := Path()
	if err != nil {
		return Default()
	}
	cfg, err := Load(path)
	if err != nil {
		return Default()
	}
	return cfg
}
