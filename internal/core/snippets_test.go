package core

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnippetValidate(t *testing.T) {
	tests := []struct {
		name    string
		snippet Snippet
		wantErr bool
	}{
		{
			name: "valid snippet",
			snippet: Snippet{
				Trigger:     "dd",
				Replacement: "20260825",
			},
			wantErr: false,
		},
		{
			name: "empty trigger",
			snippet: Snippet{
				Trigger:     "",
				Replacement: "20260825",
			},
			wantErr: true,
		},
		{
			name: "empty replacement",
			snippet: Snippet{
				Trigger:     "dd",
				Replacement: "",
			},
			wantErr: true,
		},
		{
			name: "trigger with newline",
			snippet: Snippet{
				Trigger:     "d\nd",
				Replacement: "20260825",
			},
			wantErr: true,
		},
		{
			name: "trigger too long",
			snippet: Snippet{
				Trigger:     string(make([]byte, 101)),
				Replacement: "20260825",
			},
			wantErr: true,
		},
		{
			name: "replacement too long",
			snippet: Snippet{
				Trigger:     "dd",
				Replacement: string(make([]byte, 10001)),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.snippet.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Snippet.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSnippetsService(t *testing.T) {
	// Create a temporary file for the test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	service := NewSnippetsService(store)

	t.Run("add and list", func(t *testing.T) {
		snippet := Snippet{
			Trigger:     "dd",
			Replacement: "20260825",
		}

		err := service.Add(snippet)
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}

		list, err := service.List()
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		if len(list) != 1 {
			t.Fatalf("List() returned %d snippets, want 1", len(list))
		}

		if list[0].Trigger != "dd" {
			t.Errorf("List()[0].Trigger = %s, want dd", list[0].Trigger)
		}
		if list[0].Replacement != "20260825" {
			t.Errorf("List()[0].Replacement = %s, want 20260825", list[0].Replacement)
		}
		if !list[0].Enabled {
			t.Errorf("List()[0].Enabled = false, want true")
		}
	})

	t.Run("replace existing", func(t *testing.T) {
		// Add initial snippet
		err := service.Add(Snippet{Trigger: "dd", Replacement: "old"})
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}

		// Replace with same trigger
		err = service.Add(Snippet{Trigger: "dd", Replacement: "new"})
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}

		list, err := service.List()
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		if len(list) != 1 {
			t.Fatalf("List() returned %d snippets, want 1", len(list))
		}
		if list[0].Replacement != "new" {
			t.Errorf("List()[0].Replacement = %s, want new", list[0].Replacement)
		}
	})

	t.Run("remove", func(t *testing.T) {
		err := service.Add(Snippet{Trigger: "test", Replacement: "value"})
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}

		err = service.Remove("test")
		if err != nil {
			t.Fatalf("Remove() error = %v", err)
		}

		list, err := service.List()
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		// Should only have "dd" snippet left
		if len(list) != 1 {
			t.Fatalf("List() returned %d snippets, want 1", len(list))
		}
		if list[0].Trigger != "dd" {
			t.Errorf("List()[0].Trigger = %s, want dd", list[0].Trigger)
		}
	})

	t.Run("remove non-existent", func(t *testing.T) {
		err := service.Remove("nonexistent")
		if err == nil {
			t.Error("Remove() should return error for non-existent snippet")
		}
	})

	t.Run("set enabled", func(t *testing.T) {
		err := service.Add(Snippet{Trigger: "toggle", Replacement: "value"})
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}

		// Disable
		err = service.SetEnabled("toggle", false)
		if err != nil {
			t.Fatalf("SetEnabled() error = %v", err)
		}

		list, err := service.List()
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		for _, sn := range list {
			if sn.Trigger == "toggle" && sn.Enabled {
				t.Error("SetEnabled(false) did not disable snippet")
			}
		}

		// Enable
		err = service.SetEnabled("toggle", true)
		if err != nil {
			t.Fatalf("SetEnabled() error = %v", err)
		}

		list, err = service.List()
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		for _, sn := range list {
			if sn.Trigger == "toggle" && !sn.Enabled {
				t.Error("SetEnabled(true) did not enable snippet")
			}
		}
	})

	t.Run("get enabled only", func(t *testing.T) {
		err := service.Add(Snippet{Trigger: "enabled1", Replacement: "val1"})
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}

		err = service.Add(Snippet{Trigger: "disabled1", Replacement: "val2"})
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}

		err = service.SetEnabled("disabled1", false)
		if err != nil {
			t.Fatalf("SetEnabled() error = %v", err)
		}

		enabled, err := service.GetEnabled()
		if err != nil {
			t.Fatalf("GetEnabled() error = %v", err)
		}

		// Should not contain disabled snippet
		if _, ok := enabled["disabled1"]; ok {
			t.Error("GetEnabled() returned disabled snippet")
		}

		// Should contain enabled snippets
		if _, ok := enabled["enabled1"]; !ok {
			t.Error("GetEnabled() did not return enabled snippet")
		}
	})
}

func TestSnippetCreatedTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "timestamp_test.db")

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	service := NewSnippetsService(store)

	beforeAdd := time.Now()
	err = service.Add(Snippet{Trigger: "timestamp_test", Replacement: "value"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	list, err := service.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Find the snippet we just added
	var found *Snippet
	for i := range list {
		if list[i].Trigger == "timestamp_test" {
			found = &list[i]
			break
		}
	}

	if found == nil {
		t.Fatal("timestamp_test snippet not found in list")
	}

	if found.CreatedAt.IsZero() {
		t.Error("Snippet.CreatedAt was not set")
	}

	if found.CreatedAt.Before(beforeAdd) {
		t.Error("Snippet.CreatedAt is before Add() was called")
	}
}

func TestRenderSnippetReplacement(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 7, 6, 0, time.Local)

	tests := []struct {
		name        string
		replacement string
		want        string
	}{
		{
			name:        "static text unchanged",
			replacement: "xxxxx",
			want:        "xxxxx",
		},
		{
			name:        "date expression unchanged (function not registered)",
			replacement: `${date("YYMMDD")}`,
			want:        `${date("YYMMDD")}`,
		},
		{
			name:        "data expression unchanged (function not registered)",
			replacement: `${data("YYMMDD")}`,
			want:        `${data("YYMMDD")}`,
		},
		{
			name:        "mixed static and dynamic (functions not registered)",
			replacement: `Today is ${date("YYYY-MM-DD HH:mm:ss")}`,
			want:        `Today is ${date("YYYY-MM-DD HH:mm:ss")}`,
		},
		{
			name:        "unknown expression unchanged",
			replacement: `${upper("abc")}`,
			want:        `${upper("abc")}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderSnippetReplacement(tt.replacement, now); got != tt.want {
				t.Fatalf("RenderSnippetReplacement() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderSnippetReplacementWithRegisteredFunc(t *testing.T) {
	// Register a test date function
	originalFunc := GetTemplateFunc("date")
	defer func() {
		if originalFunc != nil {
			RegisterTemplateFunc("date", originalFunc)
		} else {
			UnregisterTemplateFunc("date")
		}
	}()

	RegisterTemplateFunc("date", func(args ...string) (string, error) {
		if len(args) == 0 {
			return "2026-08-25", nil
		}
		format := args[0]
		now := time.Date(2026, 8, 25, 9, 7, 6, 0, time.UTC)
		year := now.Year()
		month := fmt.Sprintf("%02d", now.Month())
		day := fmt.Sprintf("%02d", now.Day())
		hour := fmt.Sprintf("%02d", now.Hour())
		minute := fmt.Sprintf("%02d", now.Minute())
		second := fmt.Sprintf("%02d", now.Second())

		return strings.NewReplacer(
			"YYYY", fmt.Sprintf("%d", year),
			"YY", fmt.Sprintf("%02d", year%100),
			"MM", month,
			"DD", day,
			"HH", hour,
			"mm", minute,
			"ss", second,
		).Replace(format), nil
	})

	now := time.Date(2026, 8, 25, 9, 7, 6, 0, time.UTC)
	tests := []struct {
		name        string
		replacement string
		want        string
	}{
		{
			name:        "date short format",
			replacement: `${date("YYMMDD")}`,
			want:        "260825",
		},
		{
			name:        "date long format",
			replacement: `${date("YYYY-MM-DD")}`,
			want:        "2026-08-25",
		},
		{
			name:        "mixed static and dynamic",
			replacement: `Hello ${date("YYMMDD")}`,
			want:        "Hello 260825",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderSnippetReplacement(tt.replacement, now); got != tt.want {
				t.Fatalf("RenderSnippetReplacement() = %q, want %q", got, tt.want)
			}
		})
	}
}
