package service

import (
	"bytes"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jackmordaunt/icns"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// iconCacheControl lets the webview cache icons for a day so repeated
// searches don't re-read the bundle.
const iconCacheControl = "private, max-age=86400"

// iconCacheMaxBytes bounds the in-memory cache of decoded icons; when
// exceeded the cache is dropped wholesale and re-filled lazily.
const iconCacheMaxBytes = 64 << 20

// iconContentTypes maps the icon file extensions Kyvro serves to their
// MIME types. WebKit's <img> cannot decode .icns, so those files are
// converted to PNG on the fly (see iconPNG); the other formats are
// streamed raw.
var iconContentTypes = map[string]string{
	".icns": "image/icns", // never sent raw; marker for the decode path
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".tiff": "image/tiff",
	".tif":  "image/tiff",
	".bmp":  "image/bmp",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
}

// iconPNGCache memoises decoded icons keyed by file path.
var iconPNGCache struct {
	mu    sync.Mutex
	data  map[string][]byte
	bytes int
}

// IconMiddleware serves GET /appicon?path=<icon file> from disk so the
// webview can render real app icons. Every other request falls through
// to the default asset handler (the embedded frontend).
func IconMiddleware() application.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/appicon" {
				serveIcon(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// serveIcon responds with the requested icon. Only known image
// extensions are served; anything else (or a missing/undecodable file)
// is a 404 and the frontend falls back to its monogram.
func serveIcon(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	ct, ok := iconContentTypes[strings.ToLower(filepath.Ext(path))]
	if path == "" || !ok || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", iconCacheControl)
	if ct == "image/icns" {
		pngBytes, err := iconPNG(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", ct)
	_, _ = io.Copy(w, f)
}

// iconPNG decodes an .icns file (largest embedded representation) and
// re-encodes it as PNG, memoising the result.
func iconPNG(path string) ([]byte, error) {
	iconPNGCache.mu.Lock()
	if iconPNGCache.data == nil {
		iconPNGCache.data = make(map[string][]byte)
	}
	if b, ok := iconPNGCache.data[path]; ok {
		iconPNGCache.mu.Unlock()
		return b, nil
	}
	iconPNGCache.mu.Unlock()

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, err := icns.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	b := buf.Bytes()

	iconPNGCache.mu.Lock()
	if iconPNGCache.bytes+len(b) > iconCacheMaxBytes {
		iconPNGCache.data = make(map[string][]byte)
		iconPNGCache.bytes = 0
	}
	iconPNGCache.data[path] = b
	iconPNGCache.bytes += len(b)
	iconPNGCache.mu.Unlock()
	return b, nil
}
