package service

import (
	"os"
	"path/filepath"
	"testing"

	"kyvro/internal/core"
	"kyvro/internal/plugin"
)

func TestPluginDetailSessionBridge(t *testing.T) {
	row := core.SearchResult{ID: "plugin:com.example.view:entry", PrimaryAction: core.Action{Kind: core.ActionPluginView, PluginID: "com.example.view", ActionID: "detail", Args: []string{"start"}}}
	svc, _, _, _ := newExecService(t, row)
	root := t.TempDir()
	dir := filepath.Join(root, "com.example.view", "0.1.0")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schemaVersion":1,"id":"com.example.view","version":"0.1.0","main":"main.js","minHostVersion":"0.1.0"}`
	source := `let count=0;module.exports.renderView=(id,args)=>({title:args[0],root:{type:'button',text:String(count),event:'increment'}});module.exports.onViewEvent=()=>{count++};`
	for name, content := range map[string]string{"plugin.json": manifest, "main.js": source} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	svc.mgr = plugin.NewManager(root, svc.store, nil)
	svc.mgr.LoadAll()
	t.Cleanup(svc.mgr.Shutdown)
	if _, err := svc.OpenPluginView("missing", ""); err == nil {
		t.Fatal("unknown result accepted")
	}
	if _, err := svc.OpenPluginView(row.ID, "missing"); err == nil {
		t.Fatal("unknown action accepted")
	}
	page, err := svc.OpenPluginView(row.ID, "")
	if err != nil || page.Definition.Title != "start" {
		t.Fatalf("%v %v", page, err)
	}
	page, err = svc.PluginViewEvent(page.SessionID, "increment")
	if err != nil || page.Definition.Root.Text != "1" {
		t.Fatalf("%v %v", page, err)
	}
	svc.ClosePluginView(page.SessionID)
	if _, err := svc.PluginViewEvent(page.SessionID, "increment"); err == nil {
		t.Fatal("closed session accepted")
	}
	svc.last["plain"] = core.SearchResult{ID: "plain", PrimaryAction: core.Action{Kind: core.ActionCopyText, Arg: "text"}}
	if _, err := svc.OpenPluginView("plain", ""); err == nil {
		t.Fatal("non-view action accepted")
	}
}
