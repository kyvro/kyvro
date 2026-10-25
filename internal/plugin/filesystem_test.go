package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScopedFileRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	file := filepath.Join(home, "allowed.json")
	outside := filepath.Join(home, "other.json")
	os.WriteFile(file, []byte(`{"fixture":true}`), 0600)
	os.WriteFile(outside, []byte("secret"), 0600)
	rt := newTestRuntime(t, strings.Replace(validManifest, `["storage"]`, `["filesystem:read:~/allowed.json"]`, 1), `let c;module.exports.activate=x=>c=x;module.exports.onAction=(id,args)=>[{id:'x',title:c.filesystem.readText(args[0]),actions:[{type:'copy',value:'ok'}]}];`)
	for _, name := range []string{"~/allowed.json", file} {
		rows, err := rt.RunAction(context.Background(), "x", []string{name}, time.Second)
		if err != nil || len(rows) != 1 || rows[0].Title != `{"fixture":true}` {
			t.Fatalf("%v %v", rows, err)
		}
	}
	for _, name := range []string{outside, "~/other.json", "~/sub/../../other.json", "relative"} {
		if _, err := rt.readTextFile(name); err == nil {
			t.Fatalf("read unauthorized %s", name)
		}
	}
	os.Remove(file)
	os.Symlink(outside, file)
	if _, err := rt.readTextFile(file); err == nil {
		t.Fatal("read symlink")
	}
	os.Remove(file)
	os.Mkdir(file, 0700)
	if _, err := rt.readTextFile(file); err == nil {
		t.Fatal("read directory")
	}
	os.Remove(file)
	os.WriteFile(file, []byte(strings.Repeat("x", maxResponseBytes+1)), 0600)
	if _, err := rt.readTextFile(file); err == nil {
		t.Fatal("read oversized file")
	}
	os.WriteFile(file, []byte{0xff}, 0600)
	if _, err := rt.readTextFile(file); err == nil {
		t.Fatal("read invalid UTF-8")
	}
}

func TestFileReadPermissions(t *testing.T) {
	for _, name := range []string{"filesystem", "filesystem:read:~/**", "filesystem:read:relative", "filesystem:write:/tmp/x", "system:codex-usage"} {
		if ParsePermissions("p", []string{name}, nil).Granted(name) {
			t.Fatalf("granted %s", name)
		}
	}
	name := "filesystem:read:~/allowed.json"
	if err := ParsePermissions("p", []string{name}, nil).Require("p", name); err != nil {
		t.Fatal(err)
	}
	denied := ParsePermissions("p", []string{name}, func(string, []string) []string { return nil })
	if code, _ := CodeOf(denied.Require("p", name)); code != ErrPermissionDenied {
		t.Fatal("custom denial ignored")
	}
	for _, permissions := range []string{`[]`, `["filesystem:read:~/allowed.json"]`, `["filesystem:write:/tmp/x"]`} {
		rt := newTestRuntime(t, strings.Replace(validManifest, `["storage"]`, permissions, 1), `let c;module.exports.activate=x=>c=x;module.exports.onAction=()=>[{id:'x',title:String(!!c.filesystem),actions:[{type:'copy',value:'ok'}]}];`)
		rows, err := rt.RunAction(context.Background(), "x", nil, time.Second)
		if err != nil || len(rows) != 1 || (rows[0].Title == "true") != (permissions == `["filesystem:read:~/allowed.json"]`) {
			t.Fatalf("%v %v", rows, err)
		}
	}
}

func TestCodexPlugin(t *testing.T) {
	source, err := os.ReadFile("../../plugins-official/com.kyvro.codex/main.js")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("../../plugins-official/com.kyvro.codex/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	manifest = []byte(strings.Replace(string(manifest), "main.js", "index.js", 1))
	prefix := `let response={status:200,ok:true,body:JSON.stringify({plan_type:'plus',rate_limit:{primary_window:{used_percent:25,reset_at:1800000000,limit_window_seconds:18000},secondary_window:{used_percent:90,limit_window_seconds:604800}},access_token:'response-secret'})};let requests=0;`
	suffix := `const activate=module.exports.activate;module.exports.activate=c=>{
 c.filesystem={readText:p=>{if(p!=='~/.codex/auth.json')throw Error('wrong path');return JSON.stringify({tokens:{access_token:'fixture-secret',account_id:'fixture-account'}});}};
 c.network={request:(url,options)=>{requests++;if(options.followRedirects!==false||url!=='https://chatgpt.com/backend-api/wham/usage'||options.headers.Authorization!=='Bearer fixture-secret'||options.headers['ChatGPT-Account-Id']!=='fixture-account')throw Error('wrong request');return response;}};
 activate(c);
 };const handler=module.exports.onViewEvent;module.exports.onViewEvent=(id,event)=>{if(event==='refresh'){response={status:401,ok:false,body:'fixture-secret'};}handler(id,event);};`
	store := testStore(t)
	rt, err := buildRuntime(t, manifest, prefix+string(source)+suffix, store)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Shutdown()
	rows, err := rt.RunAction(context.Background(), "codex.usage", nil, time.Second)
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %v", rows, err)
	}
	page, err := rt.renderView(context.Background(), "usage", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Root.Children[0].Title != "会话 75% 剩余" || page.Root.Children[0].Value != 75 {
		t.Fatalf("%+v", page)
	}
	cached, _, _ := store.GetNS("plugin:com.kyvro.codex", "usage")
	if strings.Contains(cached, "secret") || !strings.Contains(cached, "plus") {
		t.Fatalf("unsafe cache: %s", cached)
	}
	page, err = rt.renderView(context.Background(), "usage", nil, "refresh")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(page)
	if !strings.Contains(string(data), "登录已过期") || !strings.Contains(string(data), "上次成功") || strings.Contains(string(data), "fixture-secret") {
		t.Fatalf("%s", data)
	}
	after, _, _ := store.GetNS("plugin:com.kyvro.codex", "usage")
	if after != cached {
		t.Fatal("error overwrote cache")
	}
}

func TestCodexResetCountdown(t *testing.T) {
	source, err := os.ReadFile("../../plugins-official/com.kyvro.codex/main.js")
	if err != nil {
		t.Fatal(err)
	}
	rt := newTestRuntime(t, validManifest, string(source)+`
 Date.now=()=>1800000000000;
 module.exports.activate=()=>{};
 module.exports.onAction=(id,args)=>[{id:'reset',title:resetCountdown(Number(args[0])),actions:[{type:'copy',value:'ok'}]}];`)
	for _, tc := range []struct{ reset, want string }{
		{"0", "重置时间未知"},
		{"1799999999", "等待额度重置"},
		{"1800000001", "1 分后重置"},
		{"1800017160", "4 小时 46 分后重置"},
		{"1800507600", "5 天 21 小时后重置"},
	} {
		rows, err := rt.RunAction(context.Background(), "reset", []string{tc.reset}, time.Second)
		if err != nil || len(rows) != 1 || rows[0].Title != tc.want {
			t.Fatalf("reset=%s rows=%v err=%v", tc.reset, rows, err)
		}
	}
}
