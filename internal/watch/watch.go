// Package watch finds newly arrived FITS files in a folder that's being
// polled for automatic ("Activar AUTO"-style) processing, so a new image
// dropped into a capture folder mid-session can be measured without
// re-scanning or re-measuring anything already seen.
package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/fikua/fikua-starflux/internal/fits"
)

// FindNewFiles lists FITS files directly inside dir whose full path is not
// already present in seen, sorted by filename for a stable, deterministic
// processing order within a single poll (matters if several files land
// between two polls at once).
func FindNewFiles(dir string, seen map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("watch: read dir %s: %w", dir, err)
	}

	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !fits.IsFITSExt(e.Name()) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if !seen[path] {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}
