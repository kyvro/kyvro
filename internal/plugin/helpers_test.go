package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kyvro/internal/core"
)

// validManifest is a complete, valid manifest for com.example.test with a
// b64 search prefix and one command.
const validManifest = `{
  "schemaVersion": 1,
  "id": "com.example.test",
  "name": "Test Plugin",
  "version": "0.1.0",
  "main": "index.js",
  "minHostVersion": "0.1.0",
  "activationEvents": ["onSearchPrefix:b64", "onCommand:test.cmd"],
  "permissions": ["storage"],
  "commands": [{"id": "test.cmd", "title": "Test Command", "keywords": ["test"]}]
}`

// manifestFor rewrites the plugin id in validManifest.
func manifestFor(id string) string {
	return strings.Replace(validManifest, `"com.example.test"`, `"`+id+`"`, 1)
}

// writePluginDir installs a plugin version dir (root/<id>/<version>) with
// the given manifest and index.js, returning the version dir.
func writePluginDir(t *testing.T, root, id, version, manifest, js string) string {
	t.Helper()
	dir := filepath.Join(root, id, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fixtureJS loads a JS snippet from testdata/fixtures.
func fixtureJS(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// testStore opens a throwaway bbolt store.
func testStore(t *testing.T) *core.Store {
	t.Helper()
	s, err := core.OpenStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// manifestMap decodes a manifest JSON into a generic map for mutation.
func manifestMap(t *testing.T, src string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// marshal re-encodes a manifest map.
func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// buildRuntime loads a runtime over a temp dir with index.js, returning the
// load error instead of failing the test (for load-failure cases). The
// caller owns Shutdown; store may be nil.
func buildRuntime(t *testing.T, manifestJSON []byte, js string, store *core.Store) (*jsRuntime, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(manifestJSON)
	if err != nil {
		return nil, err
	}
	perms := ParsePermissions(m.ID, m.Permissions, nil)
	var storage *PluginStorage
	if store != nil && perms.Granted("storage") {
		storage = NewPluginStorage(store, m.ID)
	}
	return newRuntime(m, dir, storage)
}

// newTestRuntime loads a runtime that must succeed and shuts it down on
// test end.
func newTestRuntime(t *testing.T, manifestJSON, js string) *jsRuntime {
	t.Helper()
	rt, err := buildRuntime(t, []byte(manifestJSON), js, testStore(t))
	if err != nil {
		t.Fatalf("newRuntime: %v", err)
	}
	t.Cleanup(rt.Shutdown)
	return rt
}
