// Package folders implements first-class folder search: a scanner that
// indexes the subdirectories of a user-configured root and a provider that
// searches the in-memory index (spec §7).
package folders

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kyvro/internal/core"
)

// excludedNames are directory basenames never indexed (and never descended
// into): VCS data, dependency trees and build caches.
var excludedNames = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"vendor":       {},
	"dist":         {},
	"build":        {},
	".next":        {},
	".turbo":       {},
	".cache":       {},
}

// Scan walks the root recorded on src and returns an index entry for every
// directory up to src.MaxDepth levels below the root (depth 1 = direct
// children only). Hidden and excluded directories are skipped entirely;
// symlinks are not followed (filepath.WalkDir uses lstat semantics). The
// walk aborts with ctx's error once cancelled.
func Scan(ctx context.Context, src core.FolderSource, now time.Time) ([]core.FolderIndexEntry, error) {
	if src.MaxDepth < 1 {
		return nil, fmt.Errorf("folders: max depth must be >= 1, got %d", src.MaxDepth)
	}
	fi, err := os.Stat(src.Path)
	if err != nil {
		return nil, fmt.Errorf("folders: stat root: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("folders: root %q is not a directory", src.Path)
	}

	var entries []core.FolderIndexEntry
	err = filepath.WalkDir(src.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == src.Path {
			return nil // skip the root itself
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !d.IsDir() {
			return nil // only directories are indexed
		}
		base := filepath.Base(path)
		if hidden(base) || excluded(base) {
			return fs.SkipDir
		}
		depth := 1
		if rel, err := filepath.Rel(src.Path, path); err == nil && rel != "." {
			depth = 1 + strings.Count(rel, string(filepath.Separator))
		}
		if depth > src.MaxDepth {
			return fs.SkipDir
		}
		entries = append(entries, core.FolderIndexEntry{
			ID:         "folder:" + path,
			Name:       base,
			Path:       path,
			SourceID:   src.ID,
			SearchKeys: []string{base},
			UpdatedAt:  now,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("folders: scan %s: %w", src.Path, err)
	}
	return entries, nil
}

func hidden(base string) bool { return len(base) > 1 && base[0] == '.' }

func excluded(base string) bool {
	_, ok := excludedNames[base]
	return ok
}
