package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSelectVersion(t *testing.T) {
	host := HostVersion // 0.1.0

	// Highest compatible wins.
	v, err := SelectVersion([]string{"0.1.0", "0.2.0", "1.0.0"}, []string{"0.1.0", "0.1.0", "0.5.0"}, host)
	if err != nil || v != "0.2.0" {
		t.Fatalf("want 0.2.0, got %q err=%v", v, err)
	}

	// All incompatible → INCOMPATIBLE_VERSION.
	if _, err := SelectVersion([]string{"1.0.0", "2.0.0"}, []string{"0.5.0", "9.0.0"}, host); err == nil {
		t.Fatal("want incompatible error")
	} else if code, ok := CodeOf(err); !ok || code != ErrIncompatibleVersion {
		t.Fatalf("want INCOMPATIBLE_VERSION, got %v", err)
	}

	// Empty versions → INVALID_ARGUMENT.
	if _, err := SelectVersion(nil, nil, host); err == nil {
		t.Fatal("want error for empty versions")
	} else if code, ok := CodeOf(err); !ok || code != ErrInvalidArgument {
		t.Fatalf("want INVALID_ARGUMENT, got %v", err)
	}

	// Parallel-array length mismatch → INVALID_ARGUMENT.
	if _, err := SelectVersion([]string{"1.0.0"}, []string{"0.1.0", "0.2.0"}, host); err == nil {
		t.Fatal("want error for length mismatch")
	} else if code, ok := CodeOf(err); !ok || code != ErrInvalidArgument {
		t.Fatalf("want INVALID_ARGUMENT, got %v", err)
	}

	// Malformed entries are skipped, not fatal.
	v, err = SelectVersion([]string{"banana", "0.3.0"}, []string{"nope", "0.1.0"}, host)
	if err != nil || v != "0.3.0" {
		t.Fatalf("want 0.3.0 despite junk entry, got %q err=%v", v, err)
	}

	// Newer host picks the newest version.
	v, err = SelectVersion([]string{"0.1.0", "0.2.0"}, []string{"0.1.0", "0.2.0"}, "0.2.0")
	if err != nil || v != "0.2.0" {
		t.Fatalf("want 0.2.0 on newer host, got %q err=%v", v, err)
	}
}

func TestDecodeListRejectsWrongSchema(t *testing.T) {
	good := `{"version": 1, "lastUpdated": "2026-08-26T00:00:00Z", "plugins": []}`
	if _, err := decodeList([]byte(good)); err != nil {
		t.Fatalf("v1 list must decode: %v", err)
	}

	other := `{"version": 2, "lastUpdated": "2026-08-26T00:00:00Z", "plugins": []}`
	if _, err := decodeList([]byte(other)); err == nil {
		t.Fatal("unknown schema version must be rejected")
	}

	if _, err := decodeList([]byte("not json")); err == nil {
		t.Fatal("malformed list must be rejected")
	}
}

// TestRemoteFromList pins the client view: version selection per entry,
// download URLs only for compatible entries, deterministic id ordering.
func TestRemoteFromList(t *testing.T) {
	list := &RegistryList{
		Version:     registryListVersion,
		LastUpdated: time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC),
		Plugins: []RegistryPlugin{
			{
				ID:          "com.zed.incompatible",
				Name:        "Future Plugin",
				Description: "needs a newer host",
				Versions:    []string{"2.0.0"},
				MinVersions: []string{"9.9.9"},
			},
			{
				ID:          "com.aaa.multi",
				Name:        "Multi Version",
				Versions:    []string{"0.1.0", "0.2.0", "1.0.0"},
				MinVersions: []string{"0.1.0", "0.1.0", "0.5.0"},
			},
			{
				ID:          "com.mid.simple",
				Name:        "Simple",
				Versions:    []string{"0.1.0"},
				MinVersions: []string{"0.1.0"},
			},
		},
	}

	plugins := remoteFromList(list)
	if len(plugins) != 3 {
		t.Fatalf("want 3 entries (incompatible kept visible), got %d", len(plugins))
	}
	if plugins[0].ID != "com.aaa.multi" || plugins[1].ID != "com.mid.simple" || plugins[2].ID != "com.zed.incompatible" {
		t.Fatalf("entries must be sorted by id: %v", []string{plugins[0].ID, plugins[1].ID, plugins[2].ID})
	}

	multi := plugins[0]
	if multi.Version != "0.2.0" {
		t.Fatalf("selection must pick 0.2.0 for host %s, got %q", HostVersion, multi.Version)
	}
	wantURL := registryBaseURL + "/com.aaa.multi/com.aaa.multi-0.2.0.zip"
	if multi.DownloadURL != wantURL {
		t.Fatalf("download url = %q, want %q", multi.DownloadURL, wantURL)
	}
	if multi.UpdatedAt != list.LastUpdated {
		t.Fatal("updated_at must carry the list timestamp")
	}

	incompat := plugins[2]
	if incompat.Version != "" || incompat.DownloadURL != "" {
		t.Fatalf("incompatible entry must have empty version/url, got %q %q", incompat.Version, incompat.DownloadURL)
	}
}

// TestFetchPluginFromLocalList exercises the repository-local fallback
// (network unreachable in tests) end to end against the shipped list.json.
// The selected version must be the highest compatible one and carry a
// matching download URL, without pinning repo data to a specific release.
func TestFetchPluginFromLocalList(t *testing.T) {
	c := newRegistryClient("http://127.0.0.1:1") // unreachable
	rp, err := c.FetchPlugin("com.kyvro.github")
	if err != nil {
		t.Skipf("local plugins/list.json unavailable: %v", err)
	}
	if !validSemver(rp.Version) {
		t.Fatalf("selected version %q is not SemVer", rp.Version)
	}
	if rp.DownloadURL != downloadURL("com.kyvro.github", rp.Version) {
		t.Fatalf("download url = %q, want match for version %q", rp.DownloadURL, rp.Version)
	}

	if _, err := c.FetchPlugin("com.nobody.here"); err == nil {
		t.Fatal("unknown id must fail")
	}
}

// TestFetchPluginsIncrementalCache pins the incremental flow: the cached
// list decodes into the client view and the local stamp reader trims the
// trailing newline remote files commonly carry.
func TestFetchPluginsIncrementalCache(t *testing.T) {
	dir := t.TempDir()
	stamp := "2026-08-26T00:00:00Z"

	list := RegistryList{
		Version:     registryListVersion,
		LastUpdated: time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC),
	}
	list.Plugins = append(list.Plugins, RegistryPlugin{
		ID: "com.cached.p", Name: "Cached", Versions: []string{"1.0.0"}, MinVersions: []string{"0.1.0"},
	})
	data, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "list.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lastUpdated"), []byte(stamp+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := newRegistryClient("http://127.0.0.1:1") // unreachable: a full fetch would fail
	c.cacheDir = dir

	plugins, err := c.getCachedList()
	if err != nil {
		t.Fatalf("cached list must decode: %v", err)
	}
	if len(plugins) != 1 || plugins[0].ID != "com.cached.p" || plugins[0].Version != "1.0.0" {
		t.Fatalf("cached view wrong: %+v", plugins)
	}

	if got := c.getLocalCachedLastUpdated(); got != stamp {
		t.Fatalf("local stamp = %q, want %q", got, stamp)
	}
}
