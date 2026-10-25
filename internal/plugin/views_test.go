package plugin

import (
	"context"
	"encoding/json"
	"kyvro/internal/core"
	"strings"
	"testing"
	"time"
)

const viewJS = `let index=0;
module.exports.onAction=()=>[{id:'entry',title:'Demo',actions:[{type:'open-view',id:'detail',args:['seed']}]}];
module.exports.renderView=(id,args)=>({title:id+':'+args[0],shortcuts:{right:'next'},root:{type:'column',children:[{type:'text',text:String(index)},{type:'progress',value:25,max:100},{type:'button',text:'Next',event:'next'}]}});
module.exports.onViewEvent=(id,event,args)=>{if(event==='next')index++;};`

func TestOpenViewConversion(t *testing.T) {
	rt := newTestRuntime(t, validManifest, viewJS)
	rows, err := rt.RunAction(context.Background(), "test", nil, time.Second)
	if err != nil || len(rows) != 1 || rows[0].PrimaryAction.Kind != core.ActionPluginView || rows[0].PrimaryAction.ActionID != "detail" || rows[0].PrimaryAction.Args[0] != "seed" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

func TestViewSessionEventsAndLifecycle(t *testing.T) {
	root := t.TempDir()
	writePluginDir(t, root, "com.example.test", "0.1.0", validManifest, viewJS)
	mgr := NewManager(root, nil, nil)
	mgr.LoadAll()
	defer mgr.Shutdown()
	action := core.Action{Kind: core.ActionPluginView, PluginID: "com.example.test", ActionID: "detail", Args: []string{"seed"}}
	page, err := mgr.OpenView(context.Background(), action)
	if err != nil || page.Definition.Title != "detail:seed" {
		t.Fatalf("%v %v", page, err)
	}
	token := page.SessionID
	for _, invalid := range []struct{ token, event string }{{"forged", "next"}, {token, "undeclared"}} {
		if _, err := mgr.ViewEvent(context.Background(), invalid.token, invalid.event); err == nil {
			t.Fatal("invalid event accepted")
		}
	}
	page, err = mgr.ViewEvent(context.Background(), token, "next")
	if err != nil || page.Definition.Root.Children[0].Text != "1" || page.SessionID != token {
		t.Fatalf("%v %v", page, err)
	}
	// UI events must not alter the prefix command index.
	mgr.mu.RLock()
	ref, ok := mgr.cmds.match("b64")
	mgr.mu.RUnlock()
	if !ok || ref.commandID != "test.cmd" {
		t.Fatal("command index changed")
	}
	mgr.SetEnabled(action.PluginID, false)
	if _, err = mgr.ViewEvent(context.Background(), token, "next"); err == nil {
		t.Fatal("disabled plugin event accepted")
	}
	mgr.SetEnabled(action.PluginID, true)
	mgr.LoadAll()
	if _, err = mgr.ViewEvent(context.Background(), token, "next"); err == nil {
		t.Fatal("stale runtime session accepted")
	}
	page, err = mgr.OpenView(context.Background(), action)
	if err != nil {
		t.Fatal(err)
	}
	mgr.CloseView(token) // A stale close must not close the new page.
	if _, err = mgr.ViewEvent(context.Background(), page.SessionID, "next"); err != nil {
		t.Fatal(err)
	}
	mgr.CloseView(page.SessionID)
	if _, err = mgr.ViewEvent(context.Background(), page.SessionID, "next"); err == nil {
		t.Fatal("closed session accepted")
	}
}

func TestViewValidation(t *testing.T) {
	for _, js := range []string{
		`({root:{type:'html',text:'<script>'}})`,
		`({root:{type:'progress',value:101,max:100}})`,
		`({root:{type:'button',text:'Click'}})`,
		`({root:{type:'text',text:'ok'},shortcuts:{escape:'override'}})`,
		`({root:{type:'text',text:'ok',children:[{type:'text',text:'hidden'}]}})`,
		`({root:{type:'column',children:Array.from({length:257},()=>({type:'text',text:'x'}))}})`,
		`({root:{type:'text',text:'x'.repeat(16385)}})`,
		`(()=>{let root={type:'column',children:[]};let p=root;for(let i=0;i<18;i++){let n={type:'column',children:[]};p.children.push(n);p=n;}return {root};})()`,
	} {
		rt := newTestRuntime(t, validManifest, "module.exports.renderView=()=>"+js+";")
		_, err := rt.renderView(context.Background(), "detail", nil, "")
		if code, _ := CodeOf(err); code != ErrInvalidArgument {
			t.Fatalf("invalid tree accepted: %s (%v)", js, err)
		}
	}
}

func TestViewRuntimeFailures(t *testing.T) {
	for _, tc := range []struct {
		js, event string
		code      ErrorCode
	}{
		{``, "", ErrInvalidArgument},
		{`module.exports.renderView=()=>{throw new Error('broken')}`, "", ErrPluginException},
		{`module.exports.renderView=()=>({root:{type:'text',text:'ok'}})`, "next", ErrInvalidArgument},
	} {
		rt := newTestRuntime(t, validManifest, tc.js)
		_, err := rt.renderView(context.Background(), "detail", nil, tc.event)
		if code, _ := CodeOf(err); code != tc.code {
			t.Fatalf("%s: %v", tc.js, err)
		}
	}
}

func TestViewPromise(t *testing.T) {
	rt := newTestRuntime(t, validManifest, strings.Replace(viewJS, "module.exports.renderView=(id,args)=>", "module.exports.renderView=async(id,args)=>", 1))
	page, err := rt.renderView(context.Background(), "detail", []string{"async"}, "")
	if err != nil || page.Title != "detail:async" {
		t.Fatalf("%v %v", page, err)
	}
}

func TestViewZeroProgressAndArgumentIsolation(t *testing.T) {
	rt := newTestRuntime(t, validManifest, `module.exports.renderView=(id,args)=>{const title=args[0];args[0]='changed';return {title,root:{type:'progress',value:0,max:100}}};`)
	args := []string{"seed"}
	page, err := rt.renderView(context.Background(), "detail", args, "")
	if err != nil || page.Title != "seed" || args[0] != "seed" {
		t.Fatalf("%v %v args=%v", page, err, args)
	}
	data, err := json.Marshal(page)
	if err != nil || !strings.Contains(string(data), `"value":0`) {
		t.Fatalf("zero progress omitted: %s %v", data, err)
	}
}
