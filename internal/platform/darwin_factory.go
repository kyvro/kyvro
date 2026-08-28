//go:build darwin

package platform

import "kyvro/internal/platform/darwin"

// NewAppSource returns the macOS application source.
func NewAppSource() AppSource { return darwin.NewAppSource() }

// NewAppSourceWithIconCache returns the macOS application source configured
// to render asset-catalog-only app icons into cacheDir via NSWorkspace.
func NewAppSourceWithIconCache(cacheDir string) AppSource {
	return darwin.NewAppSourceWithIconCache(cacheDir)
}

// NewAppLauncher returns the macOS application launcher.
func NewAppLauncher() AppLauncher { return darwin.NewAppLauncher() }

// NewPathOpener returns the macOS open/reveal implementation.
func NewPathOpener() PathOpener { return darwin.NewPathOpener() }

// OpenURL opens url in browserApp ("" = system default browser).
func OpenURL(browserApp, url string) error { return darwin.OpenURL(browserApp, url) }

// InstalledBrowsers lists browser app names usable with OpenURL.
func InstalledBrowsers() []string { return darwin.InstalledBrowsers() }

// NewTextExpander returns the macOS text expander.
func NewTextExpander() TextExpander { return darwin.NewTextExpander() }

// DecodeImageFilePNG rasterises an image file to a square PNG of size
// points via AppKit (size 0 = shared renderedIconSize); used for .icns
// entries with JPEG2000 payloads the pure-Go decoder cannot read.
func DecodeImageFilePNG(path string, size int) ([]byte, error) {
	return darwin.DecodeImageFilePNG(path, size)
}
