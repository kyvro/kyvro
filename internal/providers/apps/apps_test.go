package apps

import (
	"context"
	"errors"
	"testing"
	"time"

	"kyvro/internal/core"
)

type fakeSource struct {
	apps  []core.AppEntry
	scans int
	err   error
}

func (f *fakeSource) List() []core.AppEntry { return f.apps }

func (f *fakeSource) Rescan() error {
	f.scans++
	return f.err
}

var testApps = []core.AppEntry{
	{BundleID: "com.apple.Safari", Name: "Safari", Path: "/Applications/Safari.app"},
	{BundleID: "com.apple.Notes", Name: "Notes", Path: "/System/Applications/Notes.app"},
	{BundleID: "com.google.Chrome", Name: "Google Chrome", Path: "/Applications/Google Chrome.app"},
}

func TestSearchFuzzyMatch(t *testing.T) {
	p := New(&fakeSource{apps: testApps})

	got := p.Search(context.Background(), "safa")
	if len(got) != 1 || got[0].ID != "app:com.apple.Safari" {
		t.Fatalf("safa should match Safari only, got %+v", got)
	}
	if got[0].PrimaryAction.Kind != core.ActionLaunchApp || got[0].PrimaryAction.Arg != "/Applications/Safari.app" {
		t.Fatalf("bad action: %+v", got[0].PrimaryAction)
	}

	// Case-insensitive substring letters across words.
	got = p.Search(context.Background(), "gc")
	if len(got) != 1 || got[0].ID != "app:com.google.Chrome" {
		t.Fatalf("gc should match Google Chrome, got %+v", got)
	}

	// No match at all.
	if got = p.Search(context.Background(), "zzz"); len(got) != 0 {
		t.Fatalf("zzz should match nothing, got %+v", got)
	}
}

func TestSearchPassesIconThrough(t *testing.T) {
	src := &fakeSource{apps: []core.AppEntry{{
		ID:       "com.example.Iconed",
		Name:     "Iconed",
		Path:     "/Applications/Iconed.app",
		IconPath: "/Applications/Iconed.app/Contents/Resources/AppIcon.icns",
	}}}
	got := New(src).Search(context.Background(), "ico")
	if len(got) != 1 || got[0].IconPath != "/Applications/Iconed.app/Contents/Resources/AppIcon.icns" {
		t.Fatalf("icon path must be passed through, got %+v", got)
	}
}

func TestSearchPinyinAndAltNames(t *testing.T) {
	src := &fakeSource{apps: []core.AppEntry{
		{BundleID: "com.alibaba.DingTalk", Name: "钉钉", Path: "/Applications/DingTalk.app", AltNames: []string{"DingTalk"}},
		{BundleID: "com.baidu.netdisk", Name: "百度网盘", Path: "/Applications/BaiduNetdisk_mac.app", AltNames: []string{"BaiduNetdisk_mac"}},
		{BundleID: "com.apple.Safari", Name: "Safari", Path: "/Applications/Safari.app"},
	}}
	p := New(src)

	cases := []struct {
		query string
		want  string
	}{
		// full pinyin and initial letters match the Han name
		{"dingding", "app:com.alibaba.DingTalk"},
		{"dd", "app:com.alibaba.DingTalk"},
		{"baiduwangpan", "app:com.baidu.netdisk"},
		{"bdwp", "app:com.baidu.netdisk"},
		// raw un-localized names still match
		{"dingtalk", "app:com.alibaba.DingTalk"},
		{"BaiduNetdisk_mac", "app:com.baidu.netdisk"},
		{"baidunetdisk", "app:com.baidu.netdisk"},
		// unchanged for pure-ASCII names
		{"safa", "app:com.apple.Safari"},
	}
	for _, tc := range cases {
		got := p.Search(context.Background(), tc.query)
		if len(got) == 0 {
			t.Errorf("%q: no results", tc.query)
			continue
		}
		if got[0].ID != tc.want {
			t.Errorf("%q: top = %s, want %s (all: %+v)", tc.query, got[0].ID, tc.want, got)
		}
	}
}

func TestSearchKeys(t *testing.T) {
	keys := searchKeys(core.AppEntry{Name: "钉钉", AltNames: []string{"DingTalk", "DingTalk"}})
	want := []string{"钉钉", "DingTalk", "dingding", "dd"}
	if len(keys) != len(want) {
		t.Fatalf("keys = %+v, want %+v", keys, want)
	}
	for i, k := range want {
		if keys[i] != k {
			t.Fatalf("keys[%d] = %q, want %q (all: %+v)", i, keys[i], k, keys)
		}
	}

	// Pure-ASCII names gain no pinyin keys.
	if got := searchKeys(core.AppEntry{Name: "Safari"}); len(got) != 1 || got[0] != "Safari" {
		t.Fatalf("ascii keys = %+v", got)
	}

	// Mixed names keep the ASCII part verbatim.
	full, initials, ok := pinyinOf("微信Mac")
	if !ok || full != "weixinMac" || initials != "wxMac" {
		t.Fatalf("pinyinOf(微信Mac) = %q, %q, %v", full, initials, ok)
	}
	if _, _, ok := pinyinOf("Chrome"); ok {
		t.Fatal("pinyinOf(Chrome) should not apply")
	}
}

func TestSearchEmptyQueryReturnsAll(t *testing.T) {
	p := New(&fakeSource{apps: testApps})
	got := p.Search(context.Background(), "")
	if len(got) != len(testApps) {
		t.Fatalf("got %d, want %d", len(got), len(testApps))
	}
	for _, r := range got {
		if r.Score != 0 {
			t.Fatalf("empty-query scores must be 0 (engine ranks by frecency), got %v", r.Score)
		}
	}
}

func TestRescanThrottled(t *testing.T) {
	src := &fakeSource{apps: testApps}
	p := New(src)

	current := time.Now()
	p.now = func() time.Time { return current }

	p.Search(context.Background(), "") // triggers first scan
	deadline := time.Now().Add(time.Second)
	for src.scans == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if src.scans != 1 {
		t.Fatalf("scans = %d, want 1", src.scans)
	}

	// Within the interval: no new scan.
	current = current.Add(10 * time.Second)
	p.Search(context.Background(), "")
	time.Sleep(20 * time.Millisecond)
	if src.scans != 1 {
		t.Fatalf("scans within interval = %d, want 1", src.scans)
	}

	// Past the interval: exactly one more scan (async, wait for it).
	current = current.Add(rescanInterval)
	p.Search(context.Background(), "")
	deadline = time.Now().Add(time.Second)
	for src.scans < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if src.scans != 2 {
		t.Fatalf("scans after interval = %d, want 2", src.scans)
	}
}

func TestWarmup(t *testing.T) {
	src := &fakeSource{apps: testApps, err: errors.New("boom")}
	p := New(src)
	p.Warmup() // must not panic despite scan error
	p.Search(context.Background(), "")
	time.Sleep(20 * time.Millisecond)
	if src.scans != 1 {
		t.Fatalf("warmup should mark cache fresh; scans = %d, want 1", src.scans)
	}
}

func TestNewWithCacheSeedsWithoutScan(t *testing.T) {
	src := &fakeSource{apps: testApps}
	seed := []core.AppIndexEntry{{
		ID:       "app:com.apple.Safari",
		Name:     "Safari",
		Path:     "/Applications/Safari.app",
		BundleID: "com.apple.Safari",
	}}
	p := NewWithCache(src, seed)

	got := p.Search(context.Background(), "safa")
	if len(got) != 1 || got[0].ID != "app:com.apple.Safari" {
		t.Fatalf("cache-seeded search = %+v", got)
	}
	if got = p.Search(context.Background(), ""); len(got) != 1 {
		t.Fatalf("empty query = %+v", got)
	}
	if src.scans != 0 {
		t.Fatalf("search over cache must not scan; scans = %d", src.scans)
	}
}

func TestRescanSwapsSnapshotAndFiresHook(t *testing.T) {
	src := &fakeSource{apps: testApps}
	p := New(src)

	hookCalled := make(chan []core.AppEntry, 1)
	p.SetCacheHook(func(list []core.AppEntry) { hookCalled <- list })

	updated := []core.AppEntry{
		{BundleID: "com.apple.Terminal", Name: "Terminal", Path: "/Applications/Utilities/Terminal.app"},
	}
	src.apps = updated
	p.Warmup()

	select {
	case list := <-hookCalled:
		if len(list) != 1 || list[0].BundleID != "com.apple.Terminal" {
			t.Fatalf("hook got %+v", list)
		}
	case <-time.After(time.Second):
		t.Fatal("cache hook not called after rescan")
	}

	got := p.Search(context.Background(), "term")
	if len(got) != 1 || got[0].ID != "app:com.apple.Terminal" {
		t.Fatalf("post-rescan search = %+v", got)
	}
	if got = p.Search(context.Background(), "safa"); got != nil {
		t.Fatalf("stale entries must be gone, got %+v", got)
	}
}

func TestFailedRescanKeepsSnapshot(t *testing.T) {
	src := &fakeSource{apps: testApps}
	p := New(src)
	p.Warmup()

	src.err = errors.New("disk on fire")
	src.apps = nil // even an emptied source cache must not clear the snapshot
	p.mu.Lock()
	p.lastRescan = time.Now().Add(-2 * rescanInterval)
	p.mu.Unlock()

	p.Search(context.Background(), "safa")
	deadline := time.Now().Add(time.Second)
	for src.scans < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	got := p.Search(context.Background(), "safa")
	if len(got) != 1 || got[0].ID != "app:com.apple.Safari" {
		t.Fatalf("failed rescan must keep old snapshot, got %+v", got)
	}
}

func TestAppIndexEntriesAndFallbackID(t *testing.T) {
	list := []core.AppEntry{
		{BundleID: "com.apple.Safari", Name: "Safari", Path: "/a.app"},
		{Name: "NoBundle", Path: "/b/NoBundle.app"},
	}
	entries := AppIndexEntries(list)
	if entries[0].ID != "app:com.apple.Safari" {
		t.Fatalf("bundle ID entry = %q", entries[0].ID)
	}
	if entries[1].ID != "app:path:/b/NoBundle.app" {
		t.Fatalf("fallback entry = %q", entries[1].ID)
	}
	if len(entries[0].SearchKeys) == 0 || entries[0].SearchKeys[0] != "Safari" {
		t.Fatalf("search keys = %+v", entries[0].SearchKeys)
	}
	if entries[0].Path != "/a.app" || entries[0].Name != "Safari" {
		t.Fatalf("entry = %+v", entries[0])
	}
}
