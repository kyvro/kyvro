//go:build darwin

package darwin

import (
	"os"
	"path/filepath"
	"testing"

	"kyvro/internal/core"
)

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
	entry, err := readAppBundle(bundle, []string{"zh_CN"})
	if err != nil {
		t.Fatalf("readAppBundle: %v", err)
	}
	if entry.Name != "钉钉" {
		t.Fatalf("name = %q, want 钉钉", entry.Name)
	}

	// …and without a matching localization the plist name is used.
	entry, err = readAppBundle(bundle, []string{"fr"})
	if err != nil {
		t.Fatalf("readAppBundle: %v", err)
	}
	if entry.Name != "DingTalk" {
		t.Fatalf("fallback name = %q, want DingTalk", entry.Name)
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
	entry, err := readAppBundle(localized, []string{"zh_CN"})
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
	entry, err = readAppBundle(plain, []string{"zh_CN"})
	if err != nil {
		t.Fatalf("readAppBundle: %v", err)
	}
	if entry.Name != "Safari" || len(entry.AltNames) != 0 {
		t.Fatalf("plain entry = %+v, want no alt names", entry)
	}
}
