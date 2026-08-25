package plugin

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"kyvro/internal/core"
)

// ActionTimeout bounds command/callback action execution (Enter path); the
// soft search budget is SearchTimeout (spec §15).
const (
	ActionTimeout = 5 * time.Second
	SearchTimeout = 150 * time.Millisecond
	// disableStrikes is the consecutive-timeout count that removes a plugin
	// from the search rotation.
	disableStrikes = 3
)

// Manager discovers, loads and supervises plugins installed under a root
// directory (spec §11 layout: <root>/<id>/<version>/plugin.json).
// A single Manager is created at service startup; individual plugin
// failures never abort the load pass.
type Manager struct {
	root  string
	store *core.Store
	grant GrantDecision

	mu      sync.RWMutex
	plugins map[string]*loadedPlugin
}

type loadedPlugin struct {
	manifest *Manifest
	rt       *jsRuntime
	disabled bool
}

// PluginStatus represents the installation and enablement state of a plugin.
type PluginStatus string

const (
	StatusNotInstalled PluginStatus = "not_installed" // Available in registry but not installed
	StatusInstalled    PluginStatus = "installed"    // Installed but disabled
	StatusEnabled      PluginStatus = "enabled"      // Installed and enabled
)

// PluginInfo describes a plugin for the settings UI, combining local and remote metadata.
type PluginInfo struct {
	ID           string
	Name         string
	Version      string
	Description  string
	Permissions  []string
	Author       string
	IconPath     string // absolute manifest-icon path ("" when none)
	IconURL      string // remote icon URL for registry plugins
	Disabled     bool   // user- or auto-disabled
	AutoDisabled bool   // disabled by the 3-strike timeout rule
	Status       PluginStatus
	DownloadURL  string // URL for downloading from registry
}

// stateNamespace is the bbolt namespace persisting user choices ("disabled"
// per plugin id) so the disabled state survives restarts. Auto-disable (3
// consecutive timeouts) is deliberately NOT persisted: a restart gives the
// plugin a fresh chance.
const stateNamespace = "plugins-state"

// NewManager creates a manager over root. store may be nil (plugins then get
// no storage, storage permission or not); grant may be nil (V1 default
// policy: storage-only).
func NewManager(root string, store *core.Store, grant GrantDecision) *Manager {
	return &Manager{
		root:    root,
		store:   store,
		grant:   grant,
		plugins: make(map[string]*loadedPlugin),
	}
}

// LoadAll scans the plugins root and activates every installable plugin.
// Failures are logged and skipped — one broken plugin never blocks the rest
// or the host.
func (m *Manager) LoadAll() {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("plugin: read plugins dir %s: %v", m.root, err)
		}
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if lp, err := m.load(filepath.Join(m.root, e.Name())); err != nil {
			log.Printf("plugin: skip %s: %v", e.Name(), err)
		} else {
			// Honor a persisted user disable: the runtime stays loaded (so
			// it can be re-enabled live) but is kept out of rotation.
			if m.store != nil {
				if _, disabled, _ := m.store.GetNS(stateNamespace, lp.manifest.ID); disabled {
					lp.disabled = true
				}
			}
			m.mu.Lock()
			m.plugins[lp.manifest.ID] = lp
			m.mu.Unlock()
			log.Printf("plugin: loaded %s %s", lp.manifest.ID, lp.manifest.Version)
		}
	}
}

// load resolves the version directory, validates the manifest and starts
// the runtime for one plugin install dir.
func (m *Manager) load(pluginDir string) (*loadedPlugin, error) {
	dir, err := ResolveVersionDir(pluginDir)
	if err != nil {
		return nil, err
	}
	manifest, err := LoadManifestFile(dir)
	if err != nil {
		return nil, err
	}
	perms := ParsePermissions(manifest.ID, manifest.Permissions, m.grant)
	var storage *PluginStorage
	if m.store != nil && perms.Granted("storage") {
		storage = NewPluginStorage(m.store, manifest.ID)
	}
	rt, err := newRuntime(manifest, dir, storage)
	if err != nil {
		return nil, err
	}
	return &loadedPlugin{manifest: manifest, rt: rt}, nil
}

// Shutdown stops every plugin runtime.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	plugins := make([]*loadedPlugin, 0, len(m.plugins))
	for _, lp := range m.plugins {
		plugins = append(plugins, lp)
	}
	m.mu.Unlock()
	for _, lp := range plugins {
		lp.rt.Shutdown()
	}
}

// RunAction executes a command or callback action in the target plugin and
// returns its result list (for the secondary view).
func (m *Manager) RunAction(ctx context.Context, pluginID, actionID string, args []string) ([]core.SearchResult, error) {
	m.mu.RLock()
	lp, ok := m.plugins[pluginID]
	disabled := ok && lp.disabled
	m.mu.RUnlock()
	if !ok {
		return nil, Errorf(pluginID, ErrInvalidArgument, "unknown plugin")
	}
	if disabled {
		return nil, Errorf(pluginID, ErrPluginException, "plugin is disabled")
	}
	return lp.rt.RunAction(ctx, actionID, args, ActionTimeout)
}

// ListPlugins reports every loaded plugin (including disabled ones) for
// the settings UI, ordered by id.
func (m *Manager) ListPlugins() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]PluginInfo, 0, len(m.plugins))
	for _, lp := range m.plugins {
		authorName := ""
		if lp.manifest.Author != nil {
			authorName = lp.manifest.Author.Name
		}
		info := PluginInfo{
			ID:           lp.manifest.ID,
			Name:         lp.manifest.DisplayName(),
			Version:      lp.manifest.Version,
			Description:  lp.manifest.Description,
			Permissions:  lp.manifest.Permissions,
			Author:       authorName,
			IconPath:     lp.rt.iconPath,
			Disabled:     lp.disabled,
			AutoDisabled: lp.disabled && lp.rt.Strikes() >= disableStrikes,
		}
		if info.Permissions == nil {
			info.Permissions = []string{}
		}
		// Set status based on disabled state
		if info.Disabled {
			info.Status = StatusInstalled
		} else {
			info.Status = StatusEnabled
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SetEnabled enables or disables a plugin (user action). Disabling is
// persisted; enabling clears both the persisted state and any strike
// counter. Auto-disabled plugins can be re-enabled the same way.
func (m *Manager) SetEnabled(pluginID string, enabled bool) error {
	m.mu.Lock()
	lp, ok := m.plugins[pluginID]
	if ok {
		lp.disabled = !enabled
	}
	m.mu.Unlock()
	if !ok {
		return Errorf(pluginID, ErrInvalidArgument, "unknown plugin")
	}
	if enabled {
		lp.rt.resetStrikes()
	}
	if m.store != nil {
		if enabled {
			return m.store.DeleteNS(stateNamespace, pluginID)
		}
		return m.store.PutNS(stateNamespace, pluginID, "disabled")
	}
	return nil
}

// disable removes a plugin from the rotation (3 consecutive search
// timeouts). Log-only here; re-enabling arrives with the settings UI.
func (m *Manager) disable(pluginID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lp, ok := m.plugins[pluginID]; ok && !lp.disabled {
		lp.disabled = true
		log.Printf("plugin: disabled %s after %d consecutive timeouts", pluginID, disableStrikes)
	}
}

// snapshot returns the active (non-disabled) plugins ordered by id for
// deterministic result merging.
func (m *Manager) snapshot() []*loadedPlugin {
	m.mu.RLock()
	out := make([]*loadedPlugin, 0, len(m.plugins))
	for _, lp := range m.plugins {
		if !lp.disabled {
			out = append(out, lp)
		}
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].manifest.ID < out[j].manifest.ID })
	return out
}
