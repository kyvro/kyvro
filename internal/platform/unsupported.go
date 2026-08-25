//go:build !darwin

package platform

import (
	"github.com/pkg/browser"

	"kyvro/internal/core"
)

// stubSource is the placeholder AppSource for unported platforms.
type stubSource struct{}

// NewAppSource returns a stub source yielding no applications.
func NewAppSource() AppSource { return stubSource{} }

// List returns nothing.
func (stubSource) List() []core.AppEntry { return nil }

// Rescan always reports the platform as unsupported.
func (stubSource) Rescan() error { return ErrUnsupported }

// stubLauncher is the placeholder AppLauncher for unported platforms.
type stubLauncher struct{}

// NewAppLauncher returns a stub launcher.
func NewAppLauncher() AppLauncher { return stubLauncher{} }

// Launch always reports the platform as unsupported.
func (stubLauncher) Launch(core.AppEntry) error { return ErrUnsupported }

// OpenURL opens url in the system default browser; named browsers are not
// supported on unported platforms.
func OpenURL(browserApp, url string) error {
	if browserApp != "" {
		return ErrUnsupported
	}
	return browser.OpenURL(url)
}

// InstalledBrowsers returns no browsers on unported platforms.
func InstalledBrowsers() []string { return nil }
