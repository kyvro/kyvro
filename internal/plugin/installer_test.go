package plugin

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZip builds a zip archive at path from a name→content map.
func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// importManifest is a valid manifest for com.example.imported, usable as a
// zip payload.
const importManifest = `{"schemaVersion":1,"id":"com.example.imported","version":"0.2.0","main":"index.js","minHostVersion":"0.1.0"}`

func TestInstallFromZipFileFlat(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "p.zip")
	writeZip(t, zipPath, map[string]string{
		"plugin.json": importManifest,
		"index.js":    "module.exports = {onAction:function(){return []}}",
	})

	in := NewInstaller(root)
	m, err := in.InstallFromZipFile(zipPath, SourceLocal)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil || m.ID != "com.example.imported" || m.Version != "0.2.0" {
		t.Fatalf("InstallFromZipFile returned %+v", m)
	}

	dir := filepath.Join(root, "com.example.imported", "0.2.0")
	for _, name := range []string{"plugin.json", "index.js"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		} else if !strings.Contains(string(b), "com.example.imported") && name == "plugin.json" {
			t.Errorf("%s content wrong: %q", name, b)
		}
	}
	// The imported version is pinned, marked as a local install.
	pin, err := in.InstalledVersion("com.example.imported")
	if err != nil || pin != "0.2.0" {
		t.Fatalf("InstalledVersion = %q, %v; want 0.2.0", pin, err)
	}
	if cp, ok := ReadCurrentPin(filepath.Join(root, "com.example.imported")); !ok ||
		cp.Version != "0.2.0" || cp.Source != SourceLocal {
		t.Fatalf("pin = %+v (ok=%v), want 0.2.0/%s", cp, ok, SourceLocal)
	}
	// No staging leftovers.
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".import-") {
			t.Errorf("staging dir leaked: %s", e.Name())
		}
	}
}

func TestInstallFromZipFileLeadingDirectoryAndSubdirs(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "p.zip")
	writeZip(t, zipPath, map[string]string{
		"com.example.imported-0.2.0/plugin.json":        importManifest,
		"com.example.imported-0.2.0/index.js":           "module.exports = {}",
		"com.example.imported-0.2.0/lib/impl.js":        "exports.x = 1",
		"com.example.imported-0.2.0/lib/deep/util.js":   "exports.y = 2",
		"__MACOSX/com.example.imported-0.2.0/.DS_Store": "junk",
	})

	in := NewInstaller(root)
	if _, err := in.InstallFromZipFile(zipPath, SourceLocal); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "com.example.imported", "0.2.0")
	for _, rel := range []string{"index.js", "lib/impl.js", "lib/deep/util.js"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s missing: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".DS_Store")); err == nil {
		t.Error(".DS_Store must not be extracted")
	}
}

func TestInstallFromZipFileBadManifestRejected(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "p.zip")
	writeZip(t, zipPath, map[string]string{
		"plugin.json": `{"schemaVersion":99,"id":"com.example.imported","version":"0.2.0","main":"index.js","minHostVersion":"0.1.0"}`,
		"index.js":    "module.exports = {}",
	})
	in := NewInstaller(root)
	if _, err := in.InstallFromZipFile(zipPath, SourceLocal); err == nil {
		t.Fatal("invalid manifest must fail the import")
	}
	if _, err := os.Stat(filepath.Join(root, "com.example.imported")); !os.IsNotExist(err) {
		t.Errorf("failed import must leave no install dir, got %v", err)
	}
}

func TestInstallFromZipFileMissingMainRejected(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "p.zip")
	writeZip(t, zipPath, map[string]string{
		"plugin.json": importManifest, // main = index.js not in archive
	})
	in := NewInstaller(root)
	if _, err := in.InstallFromZipFile(zipPath, SourceLocal); err == nil {
		t.Fatal("missing main must fail the import")
	}
	if _, err := os.Stat(filepath.Join(root, "com.example.imported")); !os.IsNotExist(err) {
		t.Errorf("failed import must leave no install dir, got %v", err)
	}
}

func TestInstallFromZipFileZipSlipRejected(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "p.zip")
	writeZip(t, zipPath, map[string]string{
		"plugin.json":   importManifest,
		"../outside.js": "evil",
	})
	in := NewInstaller(root)
	if _, err := in.InstallFromZipFile(zipPath, SourceLocal); err == nil {
		t.Fatal("escaping entry must fail the import")
	}
	outside := filepath.Join(root, "outside.js")
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Errorf("zip-slip wrote outside the install dir: %v", err)
	}
}

func TestInstallFromZipFileMultipleManifestsRejected(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "p.zip")
	writeZip(t, zipPath, map[string]string{
		"plugin.json":       importManifest,
		"other/plugin.json": importManifest,
	})
	in := NewInstaller(root)
	if _, err := in.InstallFromZipFile(zipPath, SourceLocal); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("ambiguous archive must be rejected, got %v", err)
	}
}

func TestInstallFromZipFileOverwritesSameVersion(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "p.zip")
	writeZip(t, zipPath, map[string]string{
		"plugin.json": importManifest,
		"index.js":    "version one",
	})
	in := NewInstaller(root)
	if _, err := in.InstallFromZipFile(zipPath, SourceLocal); err != nil {
		t.Fatal(err)
	}

	writeZip(t, zipPath, map[string]string{
		"plugin.json": importManifest,
		"index.js":    "version two",
		"extra.txt":   "new file",
	})
	if _, err := in.InstallFromZipFile(zipPath, SourceLocal); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "com.example.imported", "0.2.0")
	b, err := os.ReadFile(filepath.Join(dir, "index.js"))
	if err != nil || string(b) != "version two" {
		t.Fatalf("same-version reinstall did not replace content: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "extra.txt")); err != nil {
		t.Errorf("new file missing: %v", err)
	}
	// Exactly one version dir remains (no stale duplicates).
	ids, _ := os.ReadDir(filepath.Join(root, "com.example.imported"))
	if len(ids) != 2 { // 0.2.0 + current.json
		t.Errorf("want version dir + current.json, got %d entries", len(ids))
	}
}

func TestInstallFromZipFileNoManifestRejected(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "p.zip")
	writeZip(t, zipPath, map[string]string{"readme.txt": "not a plugin"})
	in := NewInstaller(root)
	if _, err := in.InstallFromZipFile(zipPath, SourceLocal); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("archive without manifest must be rejected, got %v", err)
	}
}

// TestInstallSourceMarking pins the source rules: market installs record
// "market", anything else is normalized to "local", and the source follows
// the most recent install of the version.
func TestInstallSourceMarking(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "p.zip")
	files := func(js string) map[string]string {
		return map[string]string{
			"plugin.json": importManifest,
			"index.js":    js,
		}
	}
	in := NewInstaller(root)

	// Market source on a real archive.
	writeZip(t, zipPath, files("market build"))
	if _, err := in.InstallFromZipFile(zipPath, SourceMarket); err != nil {
		t.Fatal(err)
	}
	pin, ok := ReadCurrentPin(filepath.Join(root, "com.example.imported"))
	if !ok || pin.Source != SourceMarket {
		t.Fatalf("market pin = %+v (ok=%v)", pin, ok)
	}

	// Unknown sources normalize to local.
	writeZip(t, zipPath, files("local build"))
	if _, err := in.InstallFromZipFile(zipPath, "weird"); err != nil {
		t.Fatal(err)
	}
	pin, _ = ReadCurrentPin(filepath.Join(root, "com.example.imported"))
	if pin.Source != SourceLocal {
		t.Fatalf("unknown source must normalize to local, got %q", pin.Source)
	}
}

// TestInstallRemoteIncompatible: an empty Version/DownloadURL on the remote
// entry (selection found nothing for this host) must fail with
// INCOMPATIBLE_VERSION without touching the install tree.
func TestInstallRemoteIncompatible(t *testing.T) {
	root := t.TempDir()
	in := NewInstaller(root)
	err := in.InstallRemote(&RemotePlugin{ID: "com.example.imported"})
	if err == nil {
		t.Fatal("incompatible remote must fail")
	}
	if code, ok := CodeOf(err); !ok || code != ErrIncompatibleVersion {
		t.Fatalf("want INCOMPATIBLE_VERSION, got %v", err)
	}
	if _, serr := os.Stat(filepath.Join(root, "com.example.imported")); !os.IsNotExist(serr) {
		t.Errorf("failed install must leave no install dir, got %v", serr)
	}
}
