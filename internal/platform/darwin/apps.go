//go:build darwin

// Package darwin implements the platform interfaces for macOS: .app bundle
// discovery via Info.plist parsing and launching via the open(1) command.
package darwin

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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

	// iconCacheDir stores PNGs for bundles whose .icns the pure-Go decoder
	// cannot read (JPEG2000 payloads) or whose icon lives only in a compiled
	// asset catalog (no .icns to point at); empty disables both fallbacks.
	iconCacheDir string
	// renderIcon is injectable so the cache logic is unit-testable without
	// touching AppKit; production uses renderAppIconPNG.
	renderIcon func(appPath string, size int) ([]byte, error)
	// convertIcon rasterises an on-disk image file via AppKit at a shared
	// cache size (DecodeImageFilePNG); injectable for the same reason.
	convertIcon func(path string, size int) ([]byte, error)
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
	return &AppSource{
		roots:       DefaultRoots(),
		renderIcon:  renderAppIconPNG,
		convertIcon: DecodeImageFilePNG,
	}
}

// NewAppSourceWithIconCache creates a default-root source that additionally
// renders icons for asset-catalog-only bundles into cacheDir (see the
// AppSource field docs).
func NewAppSourceWithIconCache(cacheDir string) *AppSource {
	s := NewAppSource()
	s.iconCacheDir = cacheDir
	return s
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
		entries, err := s.scanRoot(root, langs)
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
func (s *AppSource) scanRoot(root string, langs []string) ([]core.AppEntry, error) {
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
			if entry, perr := s.readAppBundle(path, langs); perr == nil {
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
func (s *AppSource) readAppBundle(bundlePath string, langs []string) (core.AppEntry, error) {
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

	iconPath := resolveIconPath(bundlePath, info.CFBundleIconFile)
	switch {
	case iconPath != "" && s.iconCacheDir != "":
		// A resolvable .icns might still be undecodable by the pure-Go
		// decoder (JPEG2000 payloads); scan-time conversion keeps the
		// runtime serve path free of AppKit.
		iconPath = s.cachedOrConvertedIcon(bundlePath, info.CFBundleIdentifier, iconPath)
	case iconPath == "" && s.iconCacheDir != "":
		iconPath = s.renderedIconFallback(bundlePath, info.CFBundleIdentifier)
	}

	return core.AppEntry{
		ID:       info.CFBundleIdentifier,
		Name:     name,
		Path:     bundlePath,
		BundleID: info.CFBundleIdentifier,
		IconPath: iconPath,
		AltNames: altNames(name, rawName, fileBase),
	}, nil
}

// iconCacheKey derives the cache file stem for a bundle: the sanitised
// bundle ID, or a short hash of the bundle path when the ID is missing.
func iconCacheKey(bundlePath, bundleID string) string {
	key := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':':
			return '_'
		}
		return r
	}, bundleID)
	if key == "" {
		sum := sha1.Sum([]byte(bundlePath))
		key = hex.EncodeToString(sum[:6])
	}
	return key
}

// writePNGAtomic stores png at dst via tmp+rename so a crashed scan never
// leaves a truncated image behind.
func writePNGAtomic(dst string, png []byte) error {
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, png, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// cachedOrConvertedIcon guards a resolvable .icns against decode failures:
// a cheap structural sniff decides whether the pure-Go icns decoder will
// read the file; JPEG2000-only or otherwise unreadable icons are converted
// once via AppKit and served as <iconCacheDir>/<bundleID>.png instead.
// Converted files refresh whenever the bundle metadata or the .icns itself
// is newer than the cache. Any failure degrades to the original path — the
// runtime serve path still has its own AppKit fallback.
func (s *AppSource) cachedOrConvertedIcon(bundlePath, bundleID, icnsPath string) string {
	if s.convertIcon == nil || s.iconCacheDir == "" {
		return icnsPath
	}
	dst := filepath.Join(s.iconCacheDir, iconCacheKey(bundlePath, bundleID)+".png")

	newest := newestIconSourceMtime(bundlePath)
	if st, err := os.Stat(icnsPath); err == nil && st.ModTime().After(newest) {
		newest = st.ModTime()
	}
	if st, err := os.Stat(dst); err == nil && !st.IsDir() && !st.ModTime().Before(newest) {
		return dst // converted copy still up to date
	}
	if icnsHasPNGElement(icnsPath) {
		return icnsPath // decodable by the pure-Go serve path as-is
	}
	png, err := s.convertIcon(icnsPath, renderedIconSize)
	if err != nil {
		log.Printf("appicon: convert %s: %v", filepath.Base(icnsPath), err)
		return icnsPath
	}
	if err := os.MkdirAll(s.iconCacheDir, 0o755); err != nil {
		log.Printf("appicon: mkdir %s: %v", s.iconCacheDir, err)
		return icnsPath
	}
	if err := writePNGAtomic(dst, png); err != nil {
		log.Printf("appicon: write %s: %v", dst, err)
		return icnsPath
	}
	return dst
}

// renderedIconFallback keeps an NSWorkspace-rendered PNG under iconCacheDir
// for bundles whose icon lives only in a compiled asset catalog; the file is
// regenerated whenever the bundle's metadata (Info.plist / Assets.car) is
// newer than the cached PNG. Empty return on any failure lets the UI fall
// back to its monogram.
func (s *AppSource) renderedIconFallback(bundlePath, bundleID string) string {
	if s.renderIcon == nil || s.iconCacheDir == "" {
		return ""
	}

	dst := filepath.Join(s.iconCacheDir, iconCacheKey(bundlePath, bundleID)+".png")

	newest := newestIconSourceMtime(bundlePath)
	if st, err := os.Stat(dst); err == nil && !st.IsDir() && !st.ModTime().Before(newest) {
		return dst // cached render still up to date
	}

	png, err := s.renderIcon(bundlePath, renderedIconSize)
	if err != nil {
		log.Printf("appicon: render %s: %v", filepath.Base(bundlePath), err)
		return ""
	}
	if err := os.MkdirAll(s.iconCacheDir, 0o755); err != nil {
		log.Printf("appicon: mkdir %s: %v", s.iconCacheDir, err)
		return ""
	}
	if err := writePNGAtomic(dst, png); err != nil {
		log.Printf("appicon: write %s: %v", dst, err)
		return ""
	}
	return dst
}

// newestIconSourceMtime reports when a bundle's icon-bearing metadata last
// changed — Info.plist always exists; Assets.car carries catalog icons.
func newestIconSourceMtime(bundlePath string) time.Time {
	mt := func(p string) time.Time {
		if st, err := os.Stat(p); err == nil {
			return st.ModTime()
		}
		return time.Time{}
	}
	t1 := mt(filepath.Join(bundlePath, "Contents", "Info.plist"))
	t2 := mt(filepath.Join(bundlePath, "Contents", "Resources", "Assets.car"))
	if t2.After(t1) {
		return t2
	}
	return t1
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
// how Finder shows localized app names (钉钉, 百度网盘, …). Bundles that
// carry only a compiled InfoPlist.loctable instead of per-language .strings
// files (modern system apps: System Settings, Calculator, … ship empty
// .lproj dirs) resolve through that table as a fallback.
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
	return localizedDisplayNameFromLoctable(bundlePath, langs)
}

// localizedDisplayNameFromLoctable resolves the display name through
// Contents/Resources/InfoPlist.loctable — a plain binary plist keyed by
// locale ("zh_CN", "en_AU", …), each mapping the usual Info.plist keys to
// their translations.
func localizedDisplayNameFromLoctable(bundlePath string, langs []string) string {
	p := filepath.Join(bundlePath, "Contents", "Resources", "InfoPlist.loctable")
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var table map[string]struct {
		CFBundleDisplayName string `plist:"CFBundleDisplayName"`
		CFBundleName        string `plist:"CFBundleName"`
	}
	if plist.NewDecoder(bytes.NewReader(data)).Decode(&table) != nil {
		return ""
	}
	for _, lang := range langs {
		loc, ok := table[lang]
		if !ok {
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

// pngMagic is the signature of PNG-encoded icns renditions — the only
// payload format the pure-Go serve-path decoder handles (JPEG2000
// renditions are skipped there, legacy masks are image-data-less).
var pngMagic = []byte{0x89, 'P', 'N', 'G'}

// icnsHasPNGElement reports cheaply whether at least one rendition in the
// .icns carries a PNG payload, i.e. whether jackmordaunt/icns will decode
// the file. Only 8-byte headers are read sequentially with ReadAt — no
// full-file parse; any structural oddity conservatively reports false so
// the scan-time AppKit conversion kicks in.
func icnsHasPNGElement(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var hdr [8]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil || string(hdr[:4]) != "icns" {
		return false
	}
	total := binary.BigEndian.Uint32(hdr[4:8])
	if total < 16 || total > 1<<30 { // implausible size: treat as unreadable
		return false
	}

	magic := make([]byte, len(pngMagic))
	for off := uint64(8); off+8 <= uint64(total); {
		var el [8]byte
		if _, err := f.ReadAt(el[:], int64(off)); err != nil {
			return false
		}
		size := binary.BigEndian.Uint32(el[4:8])
		if size < 8 {
			return false
		}
		if _, err := f.ReadAt(magic, int64(off+8)); err == nil && bytes.Equal(magic, pngMagic) {
			return true
		}
		off += uint64(size)
	}
	return false
}
