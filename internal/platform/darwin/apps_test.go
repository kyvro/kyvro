//go:build darwin

package darwin

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"howett.net/plist"

	"kyvro/internal/core"
)

// bareSource resolves bundles without touching the icon-cache fallback
// (iconCacheDir empty → AppKit never invoked in unit tests).
var bareSource = &AppSource{}

func testIconRenderer(t *testing.T, count *int) func(string, int) ([]byte, error) {
	t.Helper()
	var pngBuf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	return func(appPath string, size int) ([]byte, error) {
		*count++
		return pngBuf.Bytes(), nil
	}
}

// Asset-catalog-only bundles (no resolvable .icns) must fall back to a
// rendered PNG kept under iconCacheDir/<bundleID>.png: rendered on first
// scan, reused while newer than the bundle metadata, regenerated when the
// app updates, and leaving IconPath empty when rendering fails.
func TestIconCacheLifecycle(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()

	bundle := writeFakeApp(t, root, "Catalog", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.catalog</string>
	<key>CFBundleName</key>
	<string>Catalog</string>
</dict>
</plist>`)

	calls := 0
	src := NewAppSourceWithIconCache(cache)
	src.roots = []string{root}
	src.renderIcon = testIconRenderer(t, &calls)
	rescan := func() []core.AppEntry {
		if err := src.Rescan(); err != nil {
			t.Fatalf("rescan: %v", err)
		}
		return src.List()
	}
	wantPath := filepath.Join(cache, "com.example.catalog.png")

	apps := rescan()
	if len(apps) != 1 || apps[0].IconPath != wantPath || calls != 1 {
		t.Fatalf("first scan: icon=%q calls=%d", apps[0].IconPath, calls)
	}
	if got, err := os.ReadFile(wantPath); err != nil || len(got) == 0 {
		t.Fatalf("cached png missing/broken: %v", err)
	}

	// Second pass: PNG is newer than Info.plist → served from cache.
	if apps := rescan(); apps[0].IconPath != wantPath || calls != 1 {
		t.Fatalf("second scan re-rendered: icon=%q calls=%d", apps[0].IconPath, calls)
	}

	// App update bumps Info.plist beyond the cached PNG → re-render.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(bundle, "Contents", "Info.plist"), future, future); err != nil {
		t.Fatal(err)
	}
	if apps := rescan(); apps[0].IconPath != wantPath || calls != 2 {
		t.Fatalf("update scan did not re-render: icon=%q calls=%d", apps[0].IconPath, calls)
	}
}

func TestIconCacheFailureFallsBackEmpty(t *testing.T) {
	root := t.TempDir()
	writeFakeApp(t, root, "NoIcon", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.noicon</string>
	<key>CFBundleName</key>
	<string>NoIcon</string>
</dict>
</plist>`)

	src := NewAppSourceWithIconCache(t.TempDir())
	src.roots = []string{root}
	src.renderIcon = func(string, int) ([]byte, error) { return nil, errors.New("appkit down") }
	if err := src.Rescan(); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	apps := src.List()
	if len(apps) != 1 || apps[0].IconPath != "" {
		t.Fatalf("icon = %q, want empty on render failure", apps[0].IconPath)
	}
}

// A real .icns always wins over the rendered fallback even when an icon
// cache is configured.
func TestIconCachePrefersRealIcns(t *testing.T) {
	root := t.TempDir()
	named := writeFakeApp(t, root, "Named", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.named</string>
	<key>CFBundleName</key>
	<string>Named</string>
	<key>CFBundleIconFile</key>
	<string>Custom</string>
</dict>
</plist>`)
	writeIcon(t, named, "Custom.icns")

	calls := 0
	src := NewAppSourceWithIconCache(t.TempDir())
	src.roots = []string{root}
	src.renderIcon = testIconRenderer(t, &calls)
	// Placeholder icon bytes are not a decodable icns; conversion must not
	// touch AppKit in tests and its failure keeps the original path.
	src.convertIcon = func(string, int) ([]byte, error) { return nil, errors.New("no appkit in tests") }
	if err := src.Rescan(); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	apps := src.List()
	if len(apps) != 1 || apps[0].IconPath != filepath.Join(named, "Contents", "Resources", "Custom.icns") || calls != 0 {
		t.Fatalf("icon=%q calls=%d, want real icns and no render calls", apps[0].IconPath, calls)
	}
}

// writeFakeApp materialises a minimal .app bundle with the given plist body.
func writeFakeApp(t *testing.T, dir, name, plist string) string {
	t.Helper()
	bundle := filepath.Join(dir, name+".app")
	contents := filepath.Join(bundle, "Contents", "MacOS")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	return bundle
}

const safariPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>Safari</string>
	<key>CFBundleIdentifier</key>
	<string>com.apple.Safari</string>
	<key>CFBundleName</key>
	<string>Safari</string>
</dict>
</plist>`

func TestRescanFindsAppsInNestedUtilities(t *testing.T) {
	root := t.TempDir()
	writeFakeApp(t, root, "Safari", safariPlist)
	// Nested one level down, like /Applications/Utilities.
	if err := os.MkdirAll(filepath.Join(root, "Utilities"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeApp(t, filepath.Join(root, "Utilities"), "Activity Monitor", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.apple.ActivityMonitor</string>
	<key>CFBundleName</key>
	<string>Activity Monitor</string>
</dict>
</plist>`)

	src := NewAppSourceWithRoots(root)
	if err := src.Rescan(); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	apps := src.List()
	if len(apps) != 2 {
		t.Fatalf("found %d apps, want 2: %+v", len(apps), apps)
	}
	// Results are sorted by display name.
	if apps[0].Name != "Activity Monitor" || apps[1].Name != "Safari" {
		t.Fatalf("unexpected order: %+v", apps)
	}
	if apps[1].ID != "com.apple.Safari" || apps[1].BundleID != "com.apple.Safari" {
		t.Fatalf("bad IDs: %+v", apps[1])
	}
	if apps[1].Path == "" {
		t.Fatal("path must be recorded for launching")
	}
}

func TestRescanSkipsAgentsAndUnreadableRoots(t *testing.T) {
	root := t.TempDir()
	writeFakeApp(t, root, "SomeAgent", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.agent</string>
	<key>CFBundleName</key>
	<string>SomeAgent</string>
	<key>LSUIItem</key>
	<true/>
</dict>
</plist>`)

	src := NewAppSourceWithRoots(root, "/definitely/not/a/real/root")
	if err := src.Rescan(); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if apps := src.List(); len(apps) != 0 {
		t.Fatalf("LSUIItem agents must be skipped, got %+v", apps)
	}
}

func TestRescanResolvesIconPath(t *testing.T) {
	root := t.TempDir()

	// CFBundleIconFile without extension names an .icns in Resources.
	named := writeFakeApp(t, root, "Named", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.named</string>
	<key>CFBundleName</key>
	<string>Named</string>
	<key>CFBundleIconFile</key>
	<string>Custom</string>
</dict>
</plist>`)
	writeIcon(t, named, "Custom.icns")

	// Missing CFBundleIconFile falls back to AppIcon.icns.
	withDefault := writeFakeApp(t, root, "Default", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.default</string>
	<key>CFBundleName</key>
	<string>Default</string>
</dict>
</plist>`)
	writeIcon(t, withDefault, "AppIcon.icns")

	// No resolvable icon: IconPath stays empty.
	writeFakeApp(t, root, "Bare", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.bare</string>
	<key>CFBundleName</key>
	<string>Bare</string>
</dict>
</plist>`)

	src := NewAppSourceWithRoots(root)
	if err := src.Rescan(); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	byID := make(map[string]core.AppEntry)
	for _, a := range src.List() {
		byID[a.ID] = a
	}

	if got := byID["com.example.named"].IconPath; got != filepath.Join(named, "Contents", "Resources", "Custom.icns") {
		t.Fatalf("named icon: got %q, want %q", got, filepath.Join(named, "Contents", "Resources", "Custom.icns"))
	}
	if got := byID["com.example.default"].IconPath; got != filepath.Join(withDefault, "Contents", "Resources", "AppIcon.icns") {
		t.Fatalf("default icon: got %q, want %q", got, filepath.Join(withDefault, "Contents", "Resources", "AppIcon.icns"))
	}
	if got := byID["com.example.bare"].IconPath; got != "" {
		t.Fatalf("unresolvable icon must be empty, got %q", got)
	}
}

// writeIcon drops a placeholder icon file into a bundle's Resources dir.
func writeIcon(t *testing.T, bundle, name string) {
	t.Helper()
	res := filepath.Join(bundle, "Contents", "Resources")
	if err := os.MkdirAll(res, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(res, name), []byte("fake-icns"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalizationCandidates(t *testing.T) {
	got := localizationCandidates([]string{"zh-Hans-CN", "en-US"}, "zh_CN")
	// Preferred language first; each truncation is added in both
	// separator styles, then the legacy dirs for Chinese variants
	// before moving on to the next language.
	want := []string{
		"zh-Hans-CN", "zh_Hans_CN", "zh-Hans", "zh_Hans", "zh", "zh_CN",
		"en-US", "en_US", "en",
	}
	if len(got) < len(want) {
		t.Fatalf("candidates %v shorter than expected prefix %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("candidate[%d] = %q, want %q (full: %v)", i, got[i], w, got)
		}
	}
}

func TestReadAppBundleUsesLocalizedName(t *testing.T) {
	root := t.TempDir()
	bundle := writeFakeApp(t, root, "DingTalk", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.alibaba.DingTalk</string>
	<key>CFBundleName</key>
	<string>DingTalk</string>
</dict>
</plist>`)
	stringsDir := filepath.Join(bundle, "Contents", "Resources", "zh_CN.lproj")
	if err := os.MkdirAll(stringsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stringsDir, "InfoPlist.strings"), []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDisplayName</key>
	<string>钉钉</string>
</dict>
</plist>`), 0o644); err != nil {
		t.Fatal(err)
	}

	// With zh_CN preferred the localized name wins…
	entry, err := bareSource.readAppBundle(bundle, []string{"zh_CN"})
	if err != nil {
		t.Fatalf("readAppBundle: %v", err)
	}
	if entry.Name != "钉钉" {
		t.Fatalf("name = %q, want 钉钉", entry.Name)
	}

	// …and without a matching localization the plist name is used.
	entry, err = bareSource.readAppBundle(bundle, []string{"fr"})
	if err != nil {
		t.Fatalf("readAppBundle: %v", err)
	}
	if entry.Name != "DingTalk" {
		t.Fatalf("fallback name = %q, want DingTalk", entry.Name)
	}
}

// Regression for system apps (System Settings, Calculator, …) that ship no
// InfoPlist.strings at all — their names live in a compiled
// Resources/InfoPlist.loctable binary plist, keyed by legacy-style locale
// ("zh_CN", "en_AU", …).
func TestReadAppBundleUsesLoctable(t *testing.T) {
	root := t.TempDir()
	bundle := writeFakeApp(t, root, "System Settings", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.apple.systempreferences</string>
	<key>CFBundleName</key>
	<string>System Settings</string>
</dict>
</plist>`)

	res := filepath.Join(bundle, "Contents", "Resources")
	if err := os.MkdirAll(res, 0o755); err != nil {
		t.Fatal(err)
	}
	// Real bundles carry empty .lproj dirs alongside the loctable.
	if err := os.MkdirAll(filepath.Join(res, "zh_CN.lproj"), 0o755); err != nil {
		t.Fatal(err)
	}

	type locEntry struct {
		CFBundleDisplayName string `plist:"CFBundleDisplayName"`
		CFBundleName        string `plist:"CFBundleName"`
	}
	data, err := plist.Marshal(map[string]locEntry{
		"en":    {CFBundleName: "System Settings"},
		"zh_CN": {CFBundleName: "系统设置"},
	}, plist.BinaryFormat)
	if err != nil {
		t.Fatalf("marshal loctable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(res, "InfoPlist.loctable"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Mirror Rescan: it passes the fully expanded candidate chain, where
	// zh-Hans preferences include their zh_CN underscore twin — that twin is
	// what hits the loctable key.
	entry, err := bareSource.readAppBundle(bundle, localizationCandidates([]string{"zh-Hans-CN"}, ""))
	if err != nil {
		t.Fatalf("readAppBundle: %v", err)
	}
	if entry.Name != "系统设置" {
		t.Fatalf("name = %q, want 系统设置", entry.Name)
	}
	if len(entry.AltNames) != 1 || entry.AltNames[0] != "System Settings" {
		t.Fatalf("alt names = %+v, want [System Settings]", entry.AltNames)
	}

	// No matching locale in the table falls back to the plist name.
	entry, err = bareSource.readAppBundle(bundle, []string{"fr"})
	if err != nil {
		t.Fatalf("readAppBundle fallback: %v", err)
	}
	if entry.Name != "System Settings" {
		t.Fatalf("fallback name = %q, want System Settings", entry.Name)
	}
}

func TestReadAppBundleCollectsAltNames(t *testing.T) {
	root := t.TempDir()

	// Localized name differs from both the plist name and the filename:
	// both become search keys.
	localized := writeFakeApp(t, root, "BaiduNetdisk_mac", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.baidu.netdisk</string>
	<key>CFBundleName</key>
	<string>BaiduNetdisk</string>
</dict>
</plist>`)
	stringsDir := filepath.Join(localized, "Contents", "Resources", "zh_CN.lproj")
	if err := os.MkdirAll(stringsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stringsDir, "InfoPlist.strings"), []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDisplayName</key>
	<string>百度网盘</string>
</dict>
</plist>`), 0o644); err != nil {
		t.Fatal(err)
	}
	entry, err := bareSource.readAppBundle(localized, []string{"zh_CN"})
	if err != nil {
		t.Fatalf("readAppBundle: %v", err)
	}
	if entry.Name != "百度网盘" {
		t.Fatalf("name = %q, want 百度网盘", entry.Name)
	}
	if len(entry.AltNames) != 2 || entry.AltNames[0] != "BaiduNetdisk" || entry.AltNames[1] != "BaiduNetdisk_mac" {
		t.Fatalf("alt names = %+v, want [BaiduNetdisk BaiduNetdisk_mac]", entry.AltNames)
	}

	// No localization, name equals filename: no alt names.
	plain := writeFakeApp(t, root, "Safari", safariPlist)
	entry, err = bareSource.readAppBundle(plain, []string{"zh_CN"})
	if err != nil {
		t.Fatalf("readAppBundle: %v", err)
	}
	if entry.Name != "Safari" || len(entry.AltNames) != 0 {
		t.Fatalf("plain entry = %+v, want no alt names", entry)
	}
}

// writeTestIcns assembles an .icns file from typed elements.
func writeTestIcns(t *testing.T, path string, elements map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	icnsPNG := func() []byte {
		var buf bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.Set(0, 0, color.RGBA{R: 255, A: 255})
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	if _, ok := elements["ic07"]; ok && len(elements["ic07"]) == 0 {
		elements["ic07"] = icnsPNG()
	}
	var file bytes.Buffer
	file.WriteString("icns")
	var total [4]byte
	body := func() []byte {
		var out bytes.Buffer
		for _, typ := range []string{"s8mk", "is32", "l8mk", "il32", "ic07", "ic09"} {
			payload, ok := elements[typ]
			if !ok {
				continue
			}
			out.WriteString(typ)
			var size [4]byte
			binary.BigEndian.PutUint32(size[:], uint32(len(payload)+8))
			out.Write(size[:])
			out.Write(payload)
		}
		return out.Bytes()
	}()
	binary.BigEndian.PutUint32(total[:], uint32(len(body)+8))
	file.Write(total[:])
	file.Write(body)
	if err := os.WriteFile(path, file.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestICNSHasPNGElement(t *testing.T) {
	dir := t.TempDir()
	pngPayload := func() []byte {
		var buf bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}()

	build := func(name string, elements map[string][]byte) string {
		p := filepath.Join(dir, name)
		writeTestIcns(t, p, elements)
		return p
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"png only", build("png.icns", map[string][]byte{"ic07": pngPayload}), true},
		{"legacy masks then png (XMind-style)", build("mix.icns", map[string][]byte{"s8mk": make([]byte, 64), "is32": make([]byte, 128), "ic07": pngPayload}), true},
		{"jp2-only ic09 (OpenVPN-style)", build("jp2.icns", map[string][]byte{"ic09": append([]byte{0x00, 0x00, 0x00, 0x0c, 0x6a, 0x50, 0x20, 0x20}, make([]byte, 64)...)}), false},
		{"not an icns", filepath.Join(dir, "junk.icns"), false},
	}
	if err := os.WriteFile(cases[3].path, []byte("fake-icns"), 0o644); err != nil {
		t.Fatal(err)
	}
	if missing := filepath.Join(dir, "gone.icns"); icnsHasPNGElement(missing) {
		t.Error("missing file must report false")
	}

	for _, tc := range cases[:4] {
		if got := icnsHasPNGElement(tc.path); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Scan-time conversion: bundles whose .icns fails the pure-Go sniff
// (JPEG2000 payloads) get an AppKit-converted PNG in the icon cache at
// scan time, refreshed when bundle metadata or the .icns changes.
func TestScanConvertsUndecodableIcns(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	bundle := writeFakeApp(t, root, "JP2App", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.jp2</string>
	<key>CFBundleName</key>
	<string>JP2App</string>
	<key>CFBundleIconFile</key>
	<string>icon</string>
</dict>
</plist>`)
	icnsPath := filepath.Join(bundle, "Contents", "Resources", "icon.icns")
	writeTestIcns(t, icnsPath, map[string][]byte{
		"ic09": append([]byte{0x00, 0x00, 0x00, 0x0c, 0x6a, 0x50, 0x20, 0x20}, make([]byte, 64)...),
	})

	var fake []byte
	{
		var buf bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.Set(0, 0, color.RGBA{R: 9, G: 9, B: 9, A: 255})
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		fake = buf.Bytes()
	}
	calls := 0
	src := NewAppSourceWithIconCache(cache)
	src.roots = []string{root}
	src.convertIcon = func(p string, size int) ([]byte, error) {
		calls++
		if p != icnsPath {
			t.Errorf("convert got %q, want %q", p, icnsPath)
		}
		if size != renderedIconSize {
			t.Errorf("convert size = %d, want renderedIconSize %d for uniform cache sizing", size, renderedIconSize)
		}
		return fake, nil
	}

	rescan := func() core.AppEntry {
		if err := src.Rescan(); err != nil {
			t.Fatalf("rescan: %v", err)
		}
		apps := src.List()
		if len(apps) != 1 {
			t.Fatalf("apps = %+v", apps)
		}
		return apps[0]
	}

	dst := filepath.Join(cache, "com.example.jp2.png")
	a := rescan()
	if a.IconPath != dst || calls != 1 {
		t.Fatalf("first scan: icon=%q calls=%d, want converted cache path", a.IconPath, calls)
	}
	if got, err := os.ReadFile(dst); err != nil || !bytes.Equal(got, fake) {
		t.Fatalf("cached conversion wrong: %v", err)
	}

	// Fresh cache is reused without re-converting.
	if a := rescan(); a.IconPath != dst || calls != 1 {
		t.Fatalf("second scan: icon=%q calls=%d, want cached reuse", a.IconPath, calls)
	}

	// Updated .icns invalidates the conversion.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(icnsPath, future, future); err != nil {
		t.Fatal(err)
	}
	if a := rescan(); a.IconPath != dst || calls != 2 {
		t.Fatalf("stale scan: icon=%q calls=%d, want re-conversion", a.IconPath, calls)
	}
}

// A decodable .icns keeps pointing at the original file even when the
// conversion machinery is configured — no conversion call is made.
func TestScanLeavesDecodableIcnsAlone(t *testing.T) {
	root := t.TempDir()
	bundle := writeFakeApp(t, root, "Fine", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.fine</string>
	<key>CFBundleName</key>
	<string>Fine</string>
	<key>CFBundleIconFile</key>
	<string>icon</string>
</dict>
</plist>`)
	icnsPath := filepath.Join(bundle, "Contents", "Resources", "icon.icns")
	writeTestIcns(t, icnsPath, map[string][]byte{"ic07": nil}) // builder fills PNG payload

	src := NewAppSourceWithIconCache(t.TempDir())
	src.roots = []string{root}
	src.convertIcon = func(string, int) ([]byte, error) {
		t.Fatal("decodable icns must not trigger conversion")
		return nil, errors.New("unreachable")
	}
	if err := src.Rescan(); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if apps := src.List(); len(apps) != 1 || apps[0].IconPath != icnsPath {
		t.Fatalf("apps = %+v, want original icns path kept", apps)
	}
}
