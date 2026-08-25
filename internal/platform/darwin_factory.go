//go:build darwin

package platform

import "kyvro/internal/platform/darwin"

// NewAppSource returns the macOS application source.
func NewAppSource() AppSource { return darwin.NewAppSource() }

// NewAppLauncher returns the macOS application launcher.
func NewAppLauncher() AppLauncher { return darwin.NewAppLauncher() }

// OpenURL opens url in browserApp ("" = system default browser).
func OpenURL(browserApp, url string) error { return darwin.OpenURL(browserApp, url) }

// InstalledBrowsers lists browser app names usable with OpenURL.
func InstalledBrowsers() []string { return darwin.InstalledBrowsers() }
