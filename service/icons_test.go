package service

import (
	"bytes"
	"encoding/binary"
	"errors"
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

// Regression: icns files that open with legacy elements (s8mk/is32 masks,
// e.g. XMind ZEN) must still decode — unknown element types have to consume
// their size+payload so parsing stays aligned on later PNG entries.
// jackmordaunt/icns v1 misparsed these as "no icons found"; /v2 handles them.
func TestServeIconLegacyElementsBeforePNG(t *testing.T) {
	var pngBuf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}

	elem := func(typ string, payload []byte) []byte {
		out := []byte(typ)
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(payload)+8))
		out = append(out, size[:]...)
		return append(out, payload...)
	}

	var els bytes.Buffer
	els.Write(elem("s8mk", make([]byte, 64))) // 8-bit mask
	els.Write(elem("is32", make([]byte, 128)))
	els.Write(elem("ic07", pngBuf.Bytes()))

	var file bytes.Buffer
	file.WriteString("icns")
	var total [4]byte
	binary.BigEndian.PutUint32(total[:], uint32(file.Len()+els.Len()+8))
	file.Write(total[:])
	file.Write(els.Bytes())

	path := filepath.Join(t.TempDir(), "Legacy.icns")
	if err := os.WriteFile(path, file.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := IconMiddleware()(http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodGet, "/appicon?path="+path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("legacy-prefixed icns: code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatalf("body is not a PNG: %v", err)
	}
}

func TestServeIcon(t *testing.T) {
	// Keep the undecodable-file case away from the real AppKit fallback.
	orig := appKitDecodePNG
	appKitDecodePNG = func(string, int) ([]byte, error) { return nil, errors.New("no appkit in tests") }
	t.Cleanup(func() { appKitDecodePNG = orig })

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

// Regression: .icns files whose only rendition is JPEG2000-encoded (e.g.
// OpenVPN Connect's electron.icns — a single ic09 JP2 element) are skipped
// by jackmordaunt/icns ("no icons found"); the AppKit decoder must be
// consulted instead and its result served as image/png + memoised.
func TestServeIconJPEG2000FallsBackToAppKit(t *testing.T) {
	var pngBuf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	fakePNG := pngBuf.Bytes()

	var calls int
	var gotPath string
	orig := appKitDecodePNG
	appKitDecodePNG = func(p string, _ int) ([]byte, error) {
		calls++
		gotPath = p
		return fakePNG, nil
	}
	t.Cleanup(func() { appKitDecodePNG = orig })

	// ic09 element with a JPEG2000 payload — exactly what the real file holds.
	payload := append([]byte{0x00, 0x00, 0x00, 0x0c, 0x6a, 0x50, 0x20, 0x20}, make([]byte, 100)...)
	var el bytes.Buffer
	el.WriteString("ic09")
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(payload)+8))
	el.Write(size[:])
	el.Write(payload)
	var file bytes.Buffer
	file.WriteString("icns")
	binary.BigEndian.PutUint32(size[:], uint32(el.Len()+8))
	file.Write(size[:])
	file.Write(el.Bytes())

	path := filepath.Join(t.TempDir(), "JP2.icns")
	if err := os.WriteFile(path, file.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := IconMiddleware()(http.NotFoundHandler())
	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/appicon?path="+path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := get()
	if gotPath != path {
		t.Errorf("fallback got path %q, want %q", gotPath, path)
	}
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), fakePNG) {
		t.Fatalf("jp2 fallback: code=%d ct=%q len=%d", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	if calls != 1 {
		t.Fatalf("fallback called %d times after first request", calls)
	}

	// Second hit is served from the memoised conversion.
	if rec := get(); rec.Code != http.StatusOK || rec.Body.Len() != len(fakePNG) {
		t.Fatalf("cached jp2 fetch: code=%d len=%d", rec.Code, rec.Body.Len())
	}
	if calls != 1 {
		t.Fatalf("memoisation broken: fallback called %d times", calls)
	}
}
