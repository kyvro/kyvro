package plugin

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	cmds    commandIndex // prefix command index over enabled plugins
}

type loadedPlugin struct {
	manifest *Manifest
	rt       *jsRuntime
	disabled bool
	source   string // install source: SourceMarket or SourceLocal
}

// PluginStatus represents the installation and enablement state of a plugin.
type PluginStatus string

const (
	StatusNotInstalled PluginStatus = "not_installed" // Available in registry but not installed
	StatusInstalled    PluginStatus = "installed"     // Installed but disabled
	StatusEnabled      PluginStatus = "enabled"       // Installed and enabled
)

// PluginInfo describes an installed plugin for the settings UI.
type PluginInfo struct {
	ID           string
	Name         string
	Version      string
	Description  string
	Permissions  []string
	Author       Author
	IconPath     string // absolute manifest-icon path ("" when none)
	Disabled     bool   // user- or auto-disabled
	AutoDisabled bool   // disabled by the 3-strike timeout rule
	Status       PluginStatus
	Source       string // install source: SourceMarket or SourceLocal
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

// LoadAll reconciles the manager state with the plugins root: every
// installable plugin on disk is loaded into a fresh table which then
// replaces the live one, so plugins removed from disk (uninstall) or whose
// load now fails (broken re-import) disappear from the manager immediately.
// Runtimes that were replaced or dropped are shut down outside the lock.
// Individual plugin failures are logged and skipped — one broken plugin
// never blocks the rest or the host.
func (m *Manager) LoadAll() {
	loaded := make(map[string]*loadedPlugin)
	entries, err := os.ReadDir(m.root)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("plugin: read plugins dir %s: %v", m.root, err)
		}
	} else {
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			lp, err := m.load(filepath.Join(m.root, e.Name()))
			if err != nil {
				log.Printf("plugin: skip %s: %v", e.Name(), err)
				continue
			}
			// Honor a persisted user disable: the runtime stays loaded (so
			// it can be re-enabled live) but is kept out of rotation.
			if m.store != nil {
				if _, disabled, _ := m.store.GetNS(stateNamespace, lp.manifest.ID); disabled {
					lp.disabled = true
				}
			}
			loaded[lp.manifest.ID] = lp
			log.Printf("plugin: loaded %s %s", lp.manifest.ID, lp.manifest.Version)
		}
	}

	m.mu.Lock()
	prev := m.plugins
	m.plugins = loaded
	m.rebuildCommandsLocked()
	m.mu.Unlock()

	// Retire runtimes that this pass replaced or dropped. Shutdown is
	// race-safe (in-flight calls fail with "runtime is shutting down"
	// instead of hanging) and idempotent.
	for id, old := range prev {
		if cur := loaded[id]; cur == nil || cur.rt != old.rt {
			old.rt.Shutdown()
		}
	}
}

// rebuildCommandsLocked re-derives the prefix command index from the
// enabled plugins (index.md §4.4): Load / Enable rebuild it; Disable /
// Uninstall / auto-disable drop the plugin's prefixes. Same-prefix conflicts
// resolve deterministically to the lowest plugin id, which logs a shadow
// warning for the loser. Callers must hold m.mu for writing.
func (m *Manager) rebuildCommandsLocked() {
	idx := commandIndex{byPrefix: make(map[string]commandRef, len(m.plugins))}
	ids := make([]string, 0, len(m.plugins))
	for id := range m.plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		lp := m.plugins[id]
		if lp.disabled {
			continue
		}
		for _, c := range lp.manifest.Commands {
			if _, dup := idx.byPrefix[c.Prefix]; dup {
				log.Printf("plugin: prefix %q already indexed; command %s of %s is shadowed", c.Prefix, c.ID, id)
				continue
			}
			idx.byPrefix[c.Prefix] = commandRef{pluginID: id, commandID: c.ID}
			if len(c.Prefix) > idx.maxLen {
				idx.maxLen = len(c.Prefix)
			}
		}
	}
	m.cmds = idx
}

// liveSearch routes a prefix hit to the owning plugin's onAction under the
// live (search) budget; the full query travels as args[0] (plugins.md §3).
// A miss returns (nil, false) without touching any VM. Timeout strikes feed
// the 3-strike auto-disable, which also drops the plugin from the index.
func (m *Manager) liveSearch(ctx context.Context, query string, timeout time.Duration) ([]core.SearchResult, bool) {
	q := strings.ToLower(query)
	m.mu.RLock()
	ref, ok := m.cmds.match(q)
	var lp *loadedPlugin
	if ok {
		lp = m.plugins[ref.pluginID]
		if lp == nil || lp.disabled {
			ok = false
		}
	}
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}

	res, err := lp.rt.RunAction(ctx, ref.commandID, []string{query}, timeout)
	if err != nil {
		if code, ok := CodeOf(err); !ok || code != ErrTimeout {
			log.Printf("plugin %s: live search: %v", ref.pluginID, err)
		}
		if code, ok := CodeOf(err); ok && code == ErrTimeout && lp.rt.Strikes() >= disableStrikes {
			m.disable(ref.pluginID)
		}
		return nil, true
	}
	return res, true
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
	// Install source: the version pin records how the active version
	// arrived (market install vs offline import); a missing pin means a
	// manual directory copy, read as local.
	pin, _ := ReadCurrentPin(filepath.Dir(dir))
	source := pin.Source
	if source != SourceMarket {
		source = SourceLocal
	}
	return &loadedPlugin{manifest: manifest, rt: rt, source: source}, nil
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
		author := Author{}
		if lp.manifest.Author != nil {
			author = *lp.manifest.Author
		}
		info := PluginInfo{
			ID:           lp.manifest.ID,
			Name:         lp.manifest.DisplayName(),
			Version:      lp.manifest.Version,
			Description:  lp.manifest.Description,
			Permissions:  lp.manifest.Permissions,
			Author:       author,
			IconPath:     lp.rt.iconPath,
			Disabled:     lp.disabled,
			AutoDisabled: lp.disabled && lp.rt.Strikes() >= disableStrikes,
			Source:       lp.source,
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
// counter. Auto-disabled plugins can be re-enabled the same way. The
// command index follows: disabling removes the plugin's prefixes, enabling
// re-adds them.
func (m *Manager) SetEnabled(pluginID string, enabled bool) error {
	m.mu.Lock()
	lp, ok := m.plugins[pluginID]
	if ok {
		lp.disabled = !enabled
		m.rebuildCommandsLocked()
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

// ClearDisabledState drops the persisted user-disable flag for pluginID.
// The uninstall path calls it so reinstalling the same plugin later (from
// the market or an offline import) loads enabled instead of inheriting the
// stale disabled flag of the removed install.
func (m *Manager) ClearDisabledState(pluginID string) {
	if m.store == nil {
		return
	}
	if err := m.store.DeleteNS(stateNamespace, pluginID); err != nil {
		log.Printf("plugin: clear persisted state for %s: %v", pluginID, err)
	}
}

// disable removes a plugin from the rotation (3 consecutive search
// timeouts), including from the command index. Log-only here; re-enabling
// arrives with the settings UI.
func (m *Manager) disable(pluginID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lp, ok := m.plugins[pluginID]; ok && !lp.disabled {
		lp.disabled = true
		m.rebuildCommandsLocked()
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
