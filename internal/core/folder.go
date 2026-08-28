package core

import "time"

// FolderSource is a user-configured root directory whose subdirectories are
// indexed for folder search (spec §7). Sources live in the folder-sources
// bbolt bucket keyed by ID.
type FolderSource struct {
	ID        string
	Path      string
	MaxDepth  int
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FolderIndexEntry is one indexed directory, persisted in
// cache/folder-index.json (never in bbolt).
type FolderIndexEntry struct {
	ID         string // "folder:" + absolute path
	Name       string // basename
	Path       string // absolute path
	SourceID   string
	SearchKeys []string // precomputed match keys (basename)
	UpdatedAt  time.Time
}

// AppIndexEntry is one cached application for cache/app-index.json.
type AppIndexEntry struct {
	ID         string // "app:<bundleID>" or "app:path:<abs>"
	Name       string
	Path       string
	BundleID   string
	IconPath   string
	AltNames   []string
	SearchKeys []string
}

// AppIndexFile is the JSON envelope of cache/app-index.json.
type AppIndexFile struct {
	Version   int
	UpdatedAt time.Time
	Entries   []AppIndexEntry
}

// FolderIndexFile is the JSON envelope of cache/folder-index.json.
type FolderIndexFile struct {
	Version   int
	UpdatedAt time.Time
	Entries   []FolderIndexEntry
}

// IndexVersion is the current cache-file schema version; files with a
// higher (future) version are ignored and rebuilt.
const IndexVersion = 1

// FolderSourceInfo is the settings-UI view of a folder source plus its
// runtime scan status.
type FolderSourceInfo struct {
	Source FolderSource
	// DisplayPath is the ~-abbreviated display form of Source.Path (the
	// absolute path stays in Source.Path for tooltip/lookup use).
	DisplayPath   string
	IndexedCount  int
	LastScannedAt time.Time
	LastScanError string
	Scanning      bool
}
