package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseManifestValid(t *testing.T) {
	m, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	if m.ID != "com.example.test" || m.Version != "0.1.0" || m.Main != "index.js" {
		t.Errorf("unexpected fields: %+v", m)
	}
	if len(m.SearchPrefixes) != 1 || m.SearchPrefixes[0] != "b64" {
		t.Errorf("SearchPrefixes = %v", m.SearchPrefixes)
	}
	if len(m.CommandEventIDs) != 1 || !m.CommandEventIDs["test.cmd"] {
		t.Errorf("commands = %v", m.CommandEventIDs)
	}
	if m.DisplayName() != "Test Plugin" {
		t.Errorf("DisplayName = %q", m.DisplayName())
	}
}

func TestParseManifestErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(m map[string]any)
		code   ErrorCode
	}{
		{"bad schemaVersion", func(m map[string]any) { m["schemaVersion"] = 2 }, ErrIncompatibleVersion},
		{"missing schemaVersion", func(m map[string]any) { delete(m, "schemaVersion") }, ErrIncompatibleVersion},
		{"bad id", func(m map[string]any) { m["id"] = "Not A Domain" }, ErrInvalidArgument},
		{"single-label id", func(m map[string]any) { m["id"] = "kyvro" }, ErrInvalidArgument},
		{"bad version", func(m map[string]any) { m["version"] = "next.week" }, ErrInvalidArgument},
		{"bad minHostVersion", func(m map[string]any) { m["minHostVersion"] = "x.y.z" }, ErrInvalidArgument},
		{"minHostVersion too high", func(m map[string]any) { m["minHostVersion"] = "999.0.0" }, ErrIncompatibleVersion},
		{"main absolute", func(m map[string]any) { m["main"] = "/etc/passwd" }, ErrInvalidArgument},
		{"main escape", func(m map[string]any) { m["main"] = "../evil.js" }, ErrInvalidArgument},
		{"main deep escape", func(m map[string]any) { m["main"] = "dist/../../evil.js" }, ErrInvalidArgument},
		{"main empty", func(m map[string]any) { m["main"] = "" }, ErrInvalidArgument},
		{"icon absolute", func(m map[string]any) { m["icon"] = "/etc/logo.svg" }, ErrInvalidArgument},
		{"icon escape", func(m map[string]any) { m["icon"] = "../logo.svg" }, ErrInvalidArgument},
		{"platform mismatch", func(m map[string]any) { m["platforms"] = []string{"windows"} }, ErrIncompatibleVersion},
		{"onCommand references missing command", func(m map[string]any) {
			m["activationEvents"] = []string{"onCommand:missing.cmd"}
		}, ErrInvalidArgument},
		{"empty search prefix", func(m map[string]any) {
			m["activationEvents"] = []string{"onSearchPrefix:"}
		}, ErrInvalidArgument},
		{"unknown activation event", func(m map[string]any) {
			m["activationEvents"] = []string{"onStartup"}
		}, ErrInvalidArgument},
		{"duplicate command id", func(m map[string]any) {
			m["commands"] = []map[string]any{{"id": "a"}, {"id": "a"}}
		}, ErrInvalidArgument},
		{"empty command id", func(m map[string]any) {
			m["commands"] = []map[string]any{{"title": "x"}}
		}, ErrInvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := manifestMap(t, validManifest)
			tc.mutate(m)
			data := marshal(t, m)
			_, err := ParseManifest(data)
			code, ok := CodeOf(err)
			if err == nil || !ok || code != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}
}

func TestParseManifestDefaults(t *testing.T) {
	m := manifestMap(t, validManifest)
	delete(m, "activationEvents")
	delete(m, "permissions")
	delete(m, "commands")
	delete(m, "name")
	pm, err := ParseManifest(marshal(t, m))
	if err != nil {
		t.Fatalf("minimal manifest rejected: %v", err)
	}
	if len(pm.SearchPrefixes) != 0 || pm.DisplayName() != pm.ID {
		t.Errorf("derived fields wrong: %+v", pm)
	}
}

func TestResolveVersionDir(t *testing.T) {
	root := t.TempDir()
	pid := filepath.Join(root, "com.example.test")

	write := func(version string) {
		writePluginDir(t, root, "com.example.test", version, validManifest,
			"module.exports = {provider:{search:function(){return []}}}")
	}
	write("0.1.0")
	write("0.2.0")
	write("0.10.0") // semver: 0.10.0 > 0.2.0 > 0.1.0
	// junk entries must be ignored
	if err := os.MkdirAll(filepath.Join(pid, "backup"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pid, "not-semver"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pid, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Highest semver wins without a pin.
	dir, err := ResolveVersionDir(pid)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "0.10.0" {
		t.Errorf("want 0.10.0, got %s", dir)
	}

	// current.json pin overrides.
	if err := os.WriteFile(filepath.Join(pid, CurrentFile), []byte(`{"version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err = ResolveVersionDir(pid)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "0.1.0" {
		t.Errorf("pin ignored: %s", dir)
	}

	// A pin to a missing version falls back to the highest.
	if err := os.WriteFile(filepath.Join(pid, CurrentFile), []byte(`{"version":"9.9.9"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err = ResolveVersionDir(pid)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "0.10.0" {
		t.Errorf("stale pin not ignored: %s", dir)
	}
}

func TestLoadManifestFileIDMustMatchDir(t *testing.T) {
	root := t.TempDir()
	dir := writePluginDir(t, root, "com.example.other", "0.1.0", validManifest, "module.exports={}")
	_, err := LoadManifestFile(dir)
	if code, ok := CodeOf(err); !ok || code != ErrInvalidArgument {
		t.Fatalf("id/dir mismatch must be INVALID_ARGUMENT, got %v", err)
	}
}
