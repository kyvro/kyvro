package plugin

import (
	"path/filepath"
	"testing"

	"kyvro/internal/core"
)

func TestPluginStorageIsolationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")

	store, err := core.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	a := NewPluginStorage(store, "com.example.a")
	b := NewPluginStorage(store, "com.example.b")

	if err := a.Set("key", "from-a"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := b.Get("key"); err != nil || ok || v != "" {
		t.Fatalf("plugin B must not see plugin A keys, got %q ok=%v err=%v", v, ok, err)
	}
	if v, ok, err := a.Get("key"); err != nil || !ok || v != "from-a" {
		t.Fatalf("plugin A lost its own key: %q %v %v", v, ok, err)
	}

	// Close and reopen: plugin data survives reloads and version switches.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store2, err := core.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	a2 := NewPluginStorage(store2, "com.example.a")
	if v, ok, err := a2.Get("key"); err != nil || !ok || v != "from-a" {
		t.Fatalf("storage must persist across reloads: %q %v %v", v, ok, err)
	}
	if err := a2.Delete("key"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := a2.Get("key"); ok {
		t.Fatal("delete did not remove the key")
	}
}
