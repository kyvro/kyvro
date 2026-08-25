package service

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// makeTestICNS builds a minimal valid .icns: one ic07 element whose
// payload is a 1×1 PNG.
func makeTestICNS(t *testing.T) []byte {
	t.Helper()
	var pngBuf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	payload := pngBuf.Bytes()

	var el bytes.Buffer
	el.WriteString("ic07")
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(payload)+8))
	el.Write(size[:])
	el.Write(payload)

	var file bytes.Buffer
	file.WriteString("icns")
	binary.BigEndian.PutUint32(size[:], uint32(el.Len()+8))
	file.Write(size[:])
	file.Write(el.Bytes())
	return file.Bytes()
}

func TestServeIcon(t *testing.T) {
	dir := t.TempDir()
	icns := filepath.Join(dir, "AppIcon.icns")
	if err := os.WriteFile(icns, makeTestICNS(t), 0o644); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(dir, "Broken.icns")
	if err := os.WriteFile(corrupt, []byte("not-an-icns"), 0o644); err != nil {
		t.Fatal(err)
	}

	// next marks fall-through requests so pass-through is observable.
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	handler := IconMiddleware()(next)

	cases := []struct {
		name   string
		target string
		code   int
	}{
		{"valid icns", "/appicon?path=" + icns, http.StatusOK},
		{"undecodable icns", "/appicon?path=" + corrupt, http.StatusNotFound},
		{"missing file", "/appicon?path=" + filepath.Join(dir, "nope.icns"), http.StatusNotFound},
		{"non-image extension", "/appicon?path=" + filepath.Join(dir, "Info.plist"), http.StatusNotFound},
		{"empty path", "/appicon", http.StatusNotFound},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Errorf("%s: status = %d, want %d", tc.name, rec.Code, tc.code)
		}
	}

	// icns must come back as a decodable PNG with caching headers.
	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/appicon?path="+icns, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	rec := get()
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content type = %q, want image/png", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != iconCacheControl {
		t.Errorf("cache-control = %q, want %q", cc, iconCacheControl)
	}
	if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatalf("body is not a PNG: %v", err)
	}
	// Second hit exercises the decode cache.
	firstLen := rec.Body.Len()
	if rec2 := get(); rec2.Code != http.StatusOK || rec2.Body.Len() != firstLen {
		t.Fatalf("cached fetch mismatch: %d vs %d bytes", rec2.Body.Len(), firstLen)
	}

	// Raw PNG passthrough keeps its own content type.
	pngPath := filepath.Join(dir, "plain.png")
	if err := os.WriteFile(pngPath, rec.Body.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/appicon?path="+pngPath, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("raw png passthrough: code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// Anything else falls through to the default asset handler.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Errorf("non-icon route must fall through, got status %d", rec.Code)
	}
}
