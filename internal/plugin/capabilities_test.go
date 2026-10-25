package plugin

import (
	"context"
	"github.com/dop251/goja"
	"kyvro/internal/core"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNetworkAndNavigation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Demo", "yes")
		w.Write([]byte(`{"value":42}`))
	}))
	defer server.Close()
	manifest := strings.Replace(validManifest, `["storage"]`, `["network:127.0.0.1"]`, 1)
	rt := newTestRuntime(t, manifest, `let ctx; module.exports.activate=c=>ctx=c;
 module.exports.onAction=(id,args)=>{ const r=ctx.network.request(args[0]);
 return ctx.ui.list([{id:'row',title:r.body,actions:[{type:'copy',value:String(r.status)},{type:'callback',id:'next',shortcut:'right',args:['next']}]}]); };`)
	rows, err := rt.RunAction(context.Background(), "test", []string{server.URL}, time.Second)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if rows[0].Title != `{"value":42}` || len(rows[0].Actions) != 1 || rows[0].Actions[0].Shortcut != "right" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
	_, err = rt.request("https://example.com", "GET", "", nil)
	if code, _ := CodeOf(err); code != ErrNetworkBlocked {
		t.Fatalf("expected blocked: %v", err)
	}
}

func TestNetworkRedirectAndLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/redirect" {
			http.Redirect(w, req, "https://example.com", http.StatusFound)
			return
		}
		w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
	}))
	defer server.Close()
	rt := newTestRuntime(t, strings.Replace(validManifest, `["storage"]`, `["network:127.0.0.1"]`, 1), ``)
	for _, tc := range []struct {
		path string
		code ErrorCode
	}{{"/redirect", ErrNetworkBlocked}, {"/large", ErrHostAPIError}} {
		_, err := rt.request(server.URL+tc.path, "GET", "", nil)
		if code, _ := CodeOf(err); code != tc.code {
			t.Fatalf("%s: %v", tc.path, err)
		}
	}
}

func TestBackgroundLifecycle(t *testing.T) {
	root := t.TempDir()
	manifest := strings.Replace(validManifest, `["storage"]`, `["storage","background"]`, 1)
	writePluginDir(t, root, "com.example.test", "0.1.0", manifest, `let ctx;
 module.exports.activate=c=>{ctx=c; c.background.every(1000,()=>c.storage.set('ticks',String(Number(c.storage.get('ticks')||0)+1)));};
 module.exports.onAction=()=>ctx.ui.list([{id:'ticks',title:ctx.storage.get('ticks')||'0',actions:[{type:'copy',value:'ticks'}]}]);`)
	store := testStore(t)
	mgr := NewManager(root, store, nil)
	mgr.LoadAll()
	defer mgr.Shutdown()
	deadline := time.Now().Add(3 * time.Second)
	for {
		value, _, _ := store.GetNS("plugin:com.example.test", "ticks")
		if value != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timer did not run")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := mgr.SetEnabled("com.example.test", false); err != nil {
		t.Fatal(err)
	}
	// Drain any callback already dispatched before disable.
	mgr.mu.RLock()
	rt := mgr.plugins["com.example.test"].rt
	mgr.mu.RUnlock()
	rt.call(context.Background(), time.Second, func(_ *goja.Runtime, _ time.Time) (any, error) { return nil, nil })
	before, _, _ := store.GetNS("plugin:com.example.test", "ticks")
	time.Sleep(1100 * time.Millisecond)
	after, _, _ := store.GetNS("plugin:com.example.test", "ticks")
	if before != after {
		t.Fatal("disabled timer kept running")
	}
	if err := mgr.SetEnabled("com.example.test", true); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		value, _, _ := store.GetNS("plugin:com.example.test", "ticks")
		if value != after {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timer did not resume")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDemoEntryAndPages(t *testing.T) {
	manifest, err := os.ReadFile("../../plugins-example/com.example.capabilities/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	// The runtime test helper expects index.js; production manifest uses main.js.
	manifest = []byte(strings.Replace(string(manifest), "main.js", "index.js", 1))
	source, err := os.ReadFile("../../plugins-example/com.example.capabilities/main.js")
	if err != nil {
		t.Fatal(err)
	}
	rt, err := buildRuntime(t, manifest, string(source), testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Shutdown()
	entry, err := rt.RunAction(context.Background(), "demo", nil, time.Second)
	if err != nil || len(entry) != 1 || entry[0].PrimaryAction.ActionID != "capabilities" || entry[0].PrimaryAction.Kind != core.ActionPluginView {
		t.Fatalf("entry=%v err=%v", entry, err)
	}
	page, err := rt.renderView(context.Background(), "capabilities", nil, "previous")
	if err != nil || page.Root.Children[0].Title != "Storage" || page.Shortcuts["right"] != "next" {
		t.Fatalf("page=%v err=%v", page, err)
	}

}

func TestCapabilitiesAbsentWithoutPermission(t *testing.T) {
	rt := newTestRuntime(t, strings.Replace(validManifest, `["storage"]`, `[]`, 1), `let ctx;module.exports.activate=c=>ctx=c;module.exports.onAction=()=>[{id:'test',title:String(!ctx.network&&!ctx.background&&!!ctx.ui),actions:[{type:'copy',value:'ok'}]}];`)
	rows, err := rt.RunAction(context.Background(), "test", nil, time.Second)
	if err != nil || len(rows) != 1 || rows[0].Title != "true" {
		t.Fatalf("%v %v", rows, err)
	}
}

func TestNetworkCredentialRedirects(t *testing.T) {
	var received atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		received.Store(true)
		for _, header := range []string{"Authorization", "Cookie", "X-API-Key", "ChatGPT-Account-Id", "Referer"} {
			if req.Header.Get(header) != "" {
				t.Errorf("forwarded sensitive header %s", header)
			}
		}
		if req.URL.Path == "/" {
			http.Redirect(w, req, "/final", http.StatusFound)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, destination.URL, http.StatusFound)
	}))
	defer origin.Close()
	rt := newTestRuntime(t, strings.Replace(validManifest, `["storage"]`, `["network:127.0.0.1"]`, 1), ``)
	headers := map[string]string{"Authorization": "Bearer fixture-secret", "Cookie": "session=fixture-secret", "X-API-Key": "fixture-secret", "ChatGPT-Account-Id": "fixture-account"}
	response, err := rt.request(origin.URL, "GET", "", headers)
	if err != nil || response["status"] != 200 || !received.Load() {
		t.Fatalf("response=%v err=%v received=%v", response, err, received.Load())
	}
	received.Store(false)
	response, err = rt.requestWithRedirects(origin.URL, "GET", "", headers, false)
	if err != nil || response["status"] != 302 || received.Load() {
		t.Fatalf("redirect followed: %v %v", response, err)
	}
}

func TestNetworkErrorsDoNotExposeURL(t *testing.T) {
	rt := newTestRuntime(t, strings.Replace(validManifest, `["storage"]`, `["network:127.0.0.1"]`, 1), ``)
	// Invalid header forces a transport error without external network traffic.
	_, err := rt.request("http://127.0.0.1:1/?token=fixture-secret", "GET", "", map[string]string{"X-Test": "\nfixture-secret"})
	if err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("unsafe error: %v", err)
	}
}
