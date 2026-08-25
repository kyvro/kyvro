//go:build darwin

// Package darwin implements the platform interfaces for macOS: .app bundle
// discovery via Info.plist parsing and launching via the open(1) command.
package darwin

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"howett.net/plist"

	"kyvro/internal/core"
)

// maxDepth bounds the directory walk below each root so that app bundles
// nested in well-known subfolders (e.g. /Applications/Utilities) are found
// without descending into bundle internals.
const maxDepth = 2

// AppSource discovers macOS applications by scanning well-known roots.
type AppSource struct {
	mu   sync.RWMutex
	apps []core.AppEntry

	roots []string
}

// DefaultRoots returns the application directories scanned by default.
func DefaultRoots() []string {
	roots := []string{
		"/Applications",
		"/System/Applications",
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	return roots
}

// NewAppSource creates a source scanning the default roots.
func NewAppSource() *AppSource {
	return &AppSource{roots: DefaultRoots()}
}

// NewAppSourceWithRoots creates a source scanning custom roots (tests).
func NewAppSourceWithRoots(roots ...string) *AppSource {
	return &AppSource{roots: roots}
}

// List returns a copy of the cached application list.
func (s *AppSource) List() []core.AppEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.AppEntry, len(s.apps))
	copy(out, s.apps)
	return out
}

// Rescan walks every root and rebuilds the cache.
func (s *AppSource) Rescan() error {
	langs := preferredLocalizations()
	var apps []core.AppEntry
	seen := make(map[string]struct{})
	for _, root := range s.roots {
		entries, err := scanRoot(root, langs)
		if err != nil {
			// Unreadable roots (e.g. missing ~/Applications) are skipped.
			continue
		}
		for _, e := range entries {
			if e.ID == "" {
				continue
			}
			if _, dup := seen[e.ID]; dup {
				continue
			}
			seen[e.ID] = struct{}{}
			apps = append(apps, e)
		}
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })

	s.mu.Lock()
	s.apps = apps
	s.mu.Unlock()
	return nil
}

// scanRoot walks root up to maxDepth collecting .app bundles.
func scanRoot(root string, langs []string) ([]core.AppEntry, error) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("root %q unavailable: %w", root, err)
	}
	var apps []core.AppEntry
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint: skip unreadable entries
		}
		if !d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator))
		if strings.HasSuffix(path, ".app") {
			if entry, perr := readAppBundle(path, langs); perr == nil {
				apps = append(apps, entry)
			}
			return filepath.SkipDir // never descend into a bundle
		}
		if depth >= maxDepth {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return apps, nil
}

// infoPlist mirrors the subset of keys Kyvro cares about.
type infoPlist struct {
	CFBundleName        string `plist:"CFBundleName"`
	CFBundleDisplayName string `plist:"CFBundleDisplayName"`
	CFBundleIdentifier  string `plist:"CFBundleIdentifier"`
	CFBundleIconFile    string `plist:"CFBundleIconFile"`
	LSUIItem            bool   `plist:"LSUIItem"`
}

// readAppBundle parses <bundle>/Contents/Info.plist into an AppEntry.
// langs is the preferred localization order used to resolve display
// names from <bundle>/Contents/Resources/<lang>.lproj/InfoPlist.strings.
func readAppBundle(bundlePath string, langs []string) (core.AppEntry, error) {
	f, err := os.Open(filepath.Join(bundlePath, "Contents", "Info.plist"))
	if err != nil {
		return core.AppEntry{}, err
	}
	defer f.Close()

	var info infoPlist
	if err := plist.NewDecoder(f).Decode(&info); err != nil {
		return core.AppEntry{}, fmt.Errorf("parse %s: %w", bundlePath, err)
	}

	// Background-only agents (LSUIItem) clutter a launcher; skip them.
	if info.LSUIItem {
		return core.AppEntry{}, fmt.Errorf("%s: LSUIItem agent", bundlePath)
	}

	name := localizedDisplayName(bundlePath, langs)
	rawName := info.CFBundleDisplayName
	if rawName == "" {
		rawName = info.CFBundleName
	}
	fileBase := strings.TrimSuffix(filepath.Base(bundlePath), ".app")
	if name == "" {
		name = rawName
	}
	if name == "" {
		name = fileBase
	}

	return core.AppEntry{
		ID:       info.CFBundleIdentifier,
		Name:     name,
		Path:     bundlePath,
		BundleID: info.CFBundleIdentifier,
		IconPath: resolveIconPath(bundlePath, info.CFBundleIconFile),
		AltNames: altNames(name, rawName, fileBase),
	}, nil
}

// altNames collects the un-localized raw name and the bundle filename
// base as extra search keys, skipping duplicates of the display name
// (case-insensitive) and of each other.
func altNames(name, rawName, fileBase string) []string {
	var alt []string
	for _, cand := range []string{rawName, fileBase} {
		if cand == "" || strings.EqualFold(cand, name) {
			continue
		}
		dup := false
		for _, a := range alt {
			if strings.EqualFold(a, cand) {
				dup = true
				break
			}
		}
		if !dup {
			alt = append(alt, cand)
		}
	}
	return alt
}

// localizedDisplayName reads CFBundleDisplayName / CFBundleName from the
// first InfoPlist.strings found in the preferred localizations, mirroring
// how Finder shows localized app names (钉钉, 百度网盘, …).
func localizedDisplayName(bundlePath string, langs []string) string {
	for _, lang := range langs {
		p := filepath.Join(bundlePath, "Contents", "Resources", lang+".lproj", "InfoPlist.strings")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var loc struct {
			CFBundleDisplayName string `plist:"CFBundleDisplayName"`
			CFBundleName        string `plist:"CFBundleName"`
		}
		if plist.NewDecoder(bytes.NewReader(data)).Decode(&loc) != nil {
			continue
		}
		if loc.CFBundleDisplayName != "" {
			return loc.CFBundleDisplayName
		}
		if loc.CFBundleName != "" {
			return loc.CFBundleName
		}
	}
	return ""
}

// preferredLocalizations returns the user's preferred .lproj names in
// lookup order, read once from ~/Library/Preferences/.GlobalPreferences.plist
// (AppleLanguages / AppleLocale).
func preferredLocalizations() []string {
	langsOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		data, err := os.ReadFile(filepath.Join(home, "Library", "Preferences", ".GlobalPreferences.plist"))
		if err != nil {
			return
		}
		var prefs struct {
			AppleLanguages []string `plist:"AppleLanguages"`
			AppleLocale    string   `plist:"AppleLocale"`
		}
		if plist.NewDecoder(bytes.NewReader(data)).Decode(&prefs) != nil {
			return
		}
		langs = localizationCandidates(prefs.AppleLanguages, prefs.AppleLocale)
	})
	return langs
}

var (
	langsOnce sync.Once
	langs     []string
)

// localizationCandidates expands preferred languages into an ordered
// .lproj lookup list: every language plus its truncations in both
// separators ("zh-Hans-CN" → zh-Hans-CN, zh-Hans, zh, …) and the legacy
// locale-style dirs modern bundles still use (zh_CN, zh_TW, zh_HK).
func localizationCandidates(langs []string, locale string) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, l := range append(append([]string{}, langs...), locale) {
		norm := strings.ReplaceAll(l, "_", "-")
		parts := strings.Split(norm, "-")
		for i := len(parts); i >= 1; i-- {
			dashed := strings.Join(parts[:i], "-")
			add(dashed)
			add(strings.ReplaceAll(dashed, "-", "_"))
		}
		switch {
		case strings.HasPrefix(norm, "zh-Hant-HK"), norm == "zh-HK":
			add("zh_HK")
			add("zh_TW")
		case strings.HasPrefix(norm, "zh-Hant"):
			add("zh_TW")
		case strings.HasPrefix(norm, "zh-Hans"):
			add("zh_CN")
		}
	}
	return out
}

// resolveIconPath locates the bundle icon inside Contents/Resources.
// CFBundleIconFile omits the ".icns" extension by convention; when the
// key is missing, modern bundles name their icon AppIcon.icns. The path
// is returned only when the file exists.
func resolveIconPath(bundlePath, iconFile string) string {
	name := iconFile
	if name == "" {
		name = "AppIcon.icns"
	} else if filepath.Ext(name) == "" {
		name += ".icns"
	}
	p := filepath.Join(bundlePath, "Contents", "Resources", name)
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}
