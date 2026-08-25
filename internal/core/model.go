// Package core implements the launcher-core: pure Go, no Wails dependency.
// It defines the data model shared between providers, the search engine and
// the platform layer.
package core

// ActionKind describes what to do when a result is activated.
type ActionKind int

const (
	// ActionLaunchApp launches a macOS application by path.
	ActionLaunchApp ActionKind = iota
	// ActionOpenURL opens a URL in the default browser.
	ActionOpenURL
	// ActionCopyText copies Arg to the clipboard (calculator results).
	ActionCopyText
	// ActionPlugin dispatches to a plugin (commands and callback actions).
	// Executed via the plugin manager, never directly by the host.
	ActionPlugin
)

// Action is the executable payload attached to a search result.
type Action struct {
	Kind ActionKind
	// Arg is the app path (ActionLaunchApp) or URL (ActionOpenURL).
	Arg string
	// PluginID identifies the target plugin (ActionPlugin only).
	PluginID string
	// ActionID is the plugin-defined command or callback id
	// (ActionPlugin only).
	ActionID string
	// Args are opaque arguments passed through to the plugin
	// (ActionPlugin only).
	Args []string
}

// AppEntry describes a locally installed application.
type AppEntry struct {
	// ID is a stable identifier (bundle ID, falling back to path).
	ID string
	// Name is the display name, e.g. "Safari".
	Name string
	// Path is the absolute path of the .app bundle.
	Path string
	// BundleID is the macOS bundle identifier, empty when unavailable.
	BundleID string
	// IconPath is the absolute path of the bundle icon file (usually
	// Contents/Resources/AppIcon.icns), empty when unavailable.
	IconPath string
	// AltNames are additional search keys: the un-localized plist name
	// and the bundle filename base (e.g. BaiduNetdisk_mac), deduped
	// against Name, so queries in either language match the app.
	AltNames []string
}

// SearchResult is a single row in the launcher UI.
type SearchResult struct {
	// ID uniquely identifies the result within a search session
	// (app ID for apps, "web:<query>" for the fallback entry).
	ID string
	// Title is the primary label.
	Title string
	// Subtitle is a secondary, dimmed label (e.g. app path).
	Subtitle string
	// Action is executed when the user activates the result.
	Action Action
	// Score drives ordering; higher sorts first. Populated by the engine.
	Score float64
	// IconPath is the icon file path rendered by the UI (apps only);
	// empty results fall back to a monogram.
	IconPath string
}
