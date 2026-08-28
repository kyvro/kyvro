package service

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"kyvro/internal/core"
)

// fakeLauncher records Launch calls.
type fakeLauncher struct {
	mu   sync.Mutex
	apps []core.AppEntry
}

func (f *fakeLauncher) Launch(app core.AppEntry) error {
	f.mu.Lock()
	f.apps = append(f.apps, app)
	f.mu.Unlock()
	return nil
}

// fakeOpener records OpenPath/RevealPath calls.
type fakeOpener struct {
	mu       sync.Mutex
	opened   []string
	revealed []string
}

func (f *fakeOpener) OpenPath(p string) error {
	f.mu.Lock()
	f.opened = append(f.opened, p)
	f.mu.Unlock()
	return nil
}

func (f *fakeOpener) RevealPath(p string) error {
	f.mu.Lock()
	f.revealed = append(f.revealed, p)
	f.mu.Unlock()
	return nil
}

// fakeClipboard records copied text.
type fakeClipboard struct {
	mu   sync.Mutex
	text []string
}

func (f *fakeClipboard) WriteText(text string) error {
	f.mu.Lock()
	f.text = append(f.text, text)
	f.mu.Unlock()
	return nil
}

// fakeSource satisfies platform.AppSource without touching the disk.
type fakeSource struct{}

func (fakeSource) List() []core.AppEntry { return nil }
func (fakeSource) Rescan() error         { return nil }

// newExecService builds a minimal service wired for Execute tests: real
// store (usage recording), fake platform collaborators, session cache
// preloaded with rows.
func newExecService(t *testing.T, rows ...core.SearchResult) (*SearchService, *fakeLauncher, *fakeOpener, *fakeClipboard) {
	t.Helper()
	dir := t.TempDir()
	store, err := core.OpenStore(filepath.Join(dir, "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	launch := &fakeLauncher{}
	opener := &fakeOpener{}
	clip := &fakeClipboard{}
	last := make(map[string]core.SearchResult, len(rows))
	for _, r := range rows {
		last[r.ID] = r
	}
	svc := &SearchService{
		store:      store,
		launcher:   launch,
		pathOpener: opener,
		clipboard:  clip,
		last:       last,
	}
	return svc, launch, opener, clip
}

func appRow(id, path string) core.SearchResult {
	return core.SearchResult{
		ID:            id,
		Kind:          core.KindApp,
		Title:         "App",
		PrimaryAction: core.Action{Kind: core.ActionLaunchApp, Arg: path},
	}
}

func folderRow(path string) core.SearchResult {
	return core.SearchResult{
		ID:            "folder:" + path,
		Kind:          core.KindFolder,
		Title:         "proj",
		Data:          map[string]any{"path": path},
		PrimaryAction: core.Action{Kind: core.ActionOpenPath, Arg: path},
		Actions: []core.ActionItem{
			{ID: "reveal", Title: "Reveal in Finder", Shortcut: "cmd+enter", Action: core.Action{Kind: core.ActionRevealPath, Arg: path}},
			{ID: "copy-path", Title: "Copy Path", Shortcut: "cmd+c", Action: core.Action{Kind: core.ActionCopyText, Arg: path}},
		},
	}
}

// Spec §15: New must not open the store — ServiceStartup owns bbolt (the
// single-instance guard must run first).
func TestNewDoesNotOpenStore(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "data.db")
	_ = New(dataPath, fakeSource{}, &fakeLauncher{})
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("New must not create data.db (stat err=%v)", err)
	}
}

func TestExecuteDispatchesAllActionKinds(t *testing.T) {
	rows := []core.SearchResult{
		appRow("app:com.example.X", "/Applications/X.app"),
		{
			ID: "web:q", Kind: core.KindURL,
			PrimaryAction: core.Action{Kind: core.ActionOpenURL, Arg: "about:blank"},
		},
		{ID: "calc:1+1", Kind: core.KindText, PrimaryAction: core.Action{Kind: core.ActionCopyText, Arg: "2"}},
		folderRow("/tmp/proj"),
	}
	svc, launch, opener, clip := newExecService(t, rows...)

	// App: launcher receives the entry with the action path.
	if res, err := svc.Execute("app:com.example.X", ""); err != nil || res != nil {
		t.Fatalf("app execute: %v, %+v", err, res)
	}
	if len(launch.apps) != 1 || launch.apps[0].Path != "/Applications/X.app" || launch.apps[0].ID != "app:com.example.X" {
		t.Fatalf("launch calls = %+v", launch.apps)
	}

	// URL: dispatch reaches platform.OpenURL. Point the external-browser
	// preference at a non-existent app so `open -a` fails fast and
	// deterministically, without opening anything on the test machine.
	if err := svc.store.PutNS("settings", "external-browser", "NoSuchBrowser_KyvroTest"); err != nil {
		t.Fatal(err)
	}
	if res, err := svc.Execute("web:q", ""); err == nil {
		t.Fatalf("url dispatch should surface the open error, got %+v", res)
	}

	// Copy: clipboard receives the value.
	if res, err := svc.Execute("calc:1+1", ""); err != nil || res != nil {
		t.Fatalf("calc execute: %v", err)
	}
	if len(clip.text) != 1 || clip.text[0] != "2" {
		t.Fatalf("clipboard = %+v", clip.text)
	}

	// Folder primary: opener receives the path.
	if res, err := svc.Execute("folder:/tmp/proj", ""); err != nil || res != nil {
		t.Fatalf("folder execute: %v", err)
	}
	if len(opener.opened) != 1 || opener.opened[0] != "/tmp/proj" {
		t.Fatalf("open calls = %+v", opener.opened)
	}

	// Usage was recorded for each executed ID.
	for _, id := range []string{"app:com.example.X", "calc:1+1", "folder:/tmp/proj"} {
		if u, err := svc.store.Get(id); err != nil || u.Count != 1 {
			t.Fatalf("usage(%s) = %+v, %v", id, u, err)
		}
	}
}

func TestExecuteActionItem(t *testing.T) {
	svc, _, opener, clip := newExecService(t, folderRow("/tmp/proj"))

	// Reveal via ActionItem ID.
	if res, err := svc.Execute("folder:/tmp/proj", "reveal"); err != nil || res != nil {
		t.Fatalf("reveal: %v", err)
	}
	if len(opener.revealed) != 1 || opener.revealed[0] != "/tmp/proj" {
		t.Fatalf("reveals = %+v", opener.revealed)
	}

	// Copy-path via ActionItem ID.
	if res, err := svc.Execute("folder:/tmp/proj", "copy-path"); err != nil || res != nil {
		t.Fatalf("copy-path: %v", err)
	}
	if len(clip.text) != 1 || clip.text[0] != "/tmp/proj" {
		t.Fatalf("clipboard = %+v", clip.text)
	}

	// Unknown action item errors.
	if _, err := svc.Execute("folder:/tmp/proj", "nope"); err == nil {
		t.Fatal("unknown actionID must error")
	}
	// Unknown result ID errors.
	if _, err := svc.Execute("missing", ""); err == nil {
		t.Fatal("unknown result id must error")
	}
}
