// Package platform defines the native bridge between the pure-Go launcher
// core and OS-specific implementations (app discovery, launching, and later
// hotkeys/clipboard/spotlight). The core never imports a platform package
// directly; main wires concrete implementations into providers.
package platform

import (
	"errors"

	"kyvro/internal/core"
)

// ErrUnsupported is returned by stub implementations on platforms that have
// not been ported yet (Windows/Linux are planned post-v0.1).
var ErrUnsupported = errors.New("kyvro: this platform is not supported yet")

// AppSource enumerates installed applications.
type AppSource interface {
	// List returns the cached application list. It must not block on I/O;
	// populate the cache via Rescan (typically at startup and throttled
	// in the background afterwards).
	List() []core.AppEntry
	// Rescan performs a full on-disk scan and replaces the cache.
	Rescan() error
}

// AppLauncher starts an application.
type AppLauncher interface {
	Launch(app core.AppEntry) error
}
