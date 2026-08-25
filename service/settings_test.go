package service

import (
	"path/filepath"
	"strings"
	"testing"

	"kyvro/internal/core"
	"kyvro/internal/platform"
)

// newTestService builds a SearchService with only the pieces the settings
// methods need (no Wails app, no providers).
func newTestService(t *testing.T) *SearchService {
	t.Helper()
	store, err := core.OpenStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &SearchService{store: store}
}

func TestExternalBrowserRoundTrip(t *testing.T) {
	s := newTestService(t)

	if v, err := s.ExternalBrowser(); err != nil || v != "" {
		t.Fatalf("default = %q, %v; want \"\", nil", v, err)
	}

	installed := platform.InstalledBrowsers()
	if len(installed) == 0 {
		t.Skip("no detectable browsers on this machine")
	}
	want := installed[0]
	if err := s.SetExternalBrowser(want); err != nil {
		t.Fatalf("set %q: %v", want, err)
	}
	if v, err := s.ExternalBrowser(); err != nil || v != want {
		t.Fatalf("read back = %q, %v; want %q", v, err, want)
	}

	// "" resets to system default.
	if err := s.SetExternalBrowser(""); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.ExternalBrowser(); v != "" {
		t.Fatalf("reset failed: %q", v)
	}
}

func TestSetExternalBrowserValidates(t *testing.T) {
	s := newTestService(t)
	err := s.SetExternalBrowser("Definitely Not A Browser 42")
	if err == nil || !strings.Contains(err.Error(), "unknown browser") {
		t.Fatalf("want unknown-browser error, got %v", err)
	}
}
