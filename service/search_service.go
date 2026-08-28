// Package service hosts the Wails-bound services: thin bridges between the
// Vue frontend and the pure-Go launcher core. No business logic lives here.
package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v3/pkg/application"

	"kyvro/internal/core"
	"kyvro/internal/folders"
	"kyvro/internal/indexcache"
	"kyvro/internal/platform"
	kyvroplugin "kyvro/internal/plugin"
	"kyvro/internal/providers/apps"
	"kyvro/internal/providers/calc"
	"kyvro/internal/providers/web"
)

// clipboardWriter abstracts the Wails clipboard so Execute is unit-testable
// without an application instance.
type clipboardWriter interface {
	WriteText(text string) error
}

// wailsClipboard writes through the Wails application clipboard.
type wailsClipboard struct{}

func (wailsClipboard) WriteText(text string) error {
	app := application.Get()
	if app == nil || app.Clipboard == nil {
		return fmt.Errorf("clipboard unavailable")
	}
	if !app.Clipboard.SetText(text) {
		return fmt.Errorf("copy to clipboard failed")
	}
	return nil
}

// SearchService is bound to the frontend by Wails. Search feeds the result
// list; Execute activates a result (or one of its secondary actions) from
// the most recent Search and records usage (frecency); the plugin and
// folder methods back the settings window.
//
// The engine, store and providers are constructed lazily in ServiceStartup:
// the single-instance guard inside application.New must get the chance to
// exit a duplicate process before this service contends on the bbolt lock.
type SearchService struct {
	dataPath   string
	pluginsDir string
	source     platform.AppSource
	launcher   platform.AppLauncher

	initOnce  sync.Once
	store     *core.Store
	engine    *core.Engine
	mgr       *kyvroplugin.Manager
	installer *kyvroplugin.Installer
	initErr   error

	pathOpener platform.PathOpener
	folderCtl  *folders.Controller
	clipboard  clipboardWriter

	mu   sync.Mutex
	last map[string]core.SearchResult

	// TODO(snippets): Text Snippets is temporarily disabled and will be
	// re-opened later. Both fields are intentionally left nil (never assigned
	// in ServiceStartup) so the global keyboard hook (CGEventTap) never starts
	// and every snippet-related bound method below degrades gracefully via its
	// nil guard. Core code in internal/core/snippets.go, template.go and
	// internal/platform/darwin/expander.go is untouched for future use.
	snippets     *core.SnippetsService
	textExpander platform.TextExpander
}

// New creates the search service. dataPath is the bbolt usage database;
// plugins live in the "plugins" directory next to it.
func New(dataPath string, source platform.AppSource, launcher platform.AppLauncher) *SearchService {
	return &SearchService{
		dataPath:   dataPath,
		pluginsDir: filepath.Join(filepath.Dir(dataPath), "plugins"),
		source:     source,
		launcher:   launcher,
		clipboard:  wailsClipboard{},
		last:       make(map[string]core.SearchResult),
	}
}

// ServiceStartup opens the store, wires providers (calc, apps, folders,
// plugins, web — folders before plugins, web strictly last) into the
// engine, seeds providers from the index caches and kicks off the
// background app scan and folder refresh.
func (s *SearchService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.initOnce.Do(func() {
		store, err := core.OpenStore(s.dataPath)
		if err != nil {
			s.initErr = fmt.Errorf("open usage store: %w", err)
			return
		}
		cache, err := indexcache.Open(filepath.Join(filepath.Dir(s.dataPath), "cache"))
		if err != nil {
			log.Printf("index cache: %v; search starts uncached", err)
		}

		mgr := kyvroplugin.NewManager(s.pluginsDir, store, nil)
		mgr.LoadAll() // single-plugin failures are logged, not fatal
		installer := kyvroplugin.NewInstaller(s.pluginsDir)

		// Seed the apps provider from cache/app-index.json; degradation is
		// fine, the background warmup rescans.
		var appSeed []core.AppIndexEntry
		if cache != nil {
			if f, err := cache.LoadAppIndex(); err != nil {
				log.Printf("app index cache: %v", err)
			} else {
				appSeed = f.Entries
			}
		}
		appsProvider := apps.NewWithCache(s.source, appSeed)

		// Folder controller seeds from bbolt + cache/folder-index.json.
		var folderCtl *folders.Controller
		if cache != nil {
			folderCtl = folders.NewController(store, cache)
			if err := folderCtl.LoadAtStartup(); err != nil {
				log.Printf("folders: startup load: %v", err)
			}
		} else {
			// No cache dir: a controller over a nil cache is unusable;
			// folder search simply stays empty until restart.
			folderCtl = folders.NewController(store, nil)
		}

		var folderProvider core.Provider = folderCtl.Provider()
		engine := core.NewEngine(
			[]core.Provider{calc.New(), appsProvider, folderProvider, mgr.Provider(), web.New()},
			store,
			0, // DefaultLimit
		)

		// The cache hook must be registered before Warmup so the very first
		// rescan already persists app-index.json.
		if cache != nil {
			c := cache
			appsProvider.SetCacheHook(func(list []core.AppEntry) {
				if err := c.SaveAppIndex(apps.AppIndexEntries(list)); err != nil {
					log.Printf("app index cache: save: %v", err)
				}
			})
		}
		go appsProvider.Warmup()
		go folderCtl.BackgroundRefresh(ctx)

		// TODO(snippets): Text Snippets is temporarily disabled; restore this
		// block to re-enable global text expansion (snippets service init,
		// expander creation and the startup refresh).
		//
		// Initialize snippets service
		// snippets := core.NewSnippetsService(store)

		s.store = store
		s.mgr = mgr
		s.installer = installer
		s.engine = engine
		s.pathOpener = platform.NewPathOpener()
		s.folderCtl = folderCtl

		// TODO(snippets): disabled together with the feature — see the field
		// comments on SearchService and the block above in ServiceStartup.
		// s.snippets = snippets
		// s.textExpander = platform.NewTextExpander()
		//
		// if enabled, err := s.SnippetsEnabled(); err == nil && enabled {
		// 	if err := s.refreshTextExpander(); err != nil {
		// 		log.Printf("snippets: start expander: %v", err)
		// 	}
		// }
	})
	return s.initErr
}

// ServiceShutdown stops the plugin runtimes first (so in-flight plugin calls
// finish against a live store), then closes the usage store.
func (s *SearchService) ServiceShutdown() error {
	if s.textExpander != nil {
		if err := s.textExpander.Stop(); err != nil {
			log.Printf("snippets: stop expander: %v", err)
		}
	}
	if s.mgr != nil {
		s.mgr.Shutdown()
	}
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

// ready returns the engine, or the startup error if initialisation failed.
func (s *SearchService) ready() (*core.Engine, error) {
	if s.engine == nil {
		if s.initErr != nil {
			return nil, s.initErr
		}
		return nil, fmt.Errorf("search service not started")
	}
	return s.engine, nil
}

// Search runs the engine for query and caches the results for Launch.
func (s *SearchService) Search(query string) ([]core.SearchResult, error) {
	engine, err := s.ready()
	if err != nil {
		return nil, err
	}
	results := engine.Search(context.Background(), query)

	m := make(map[string]core.SearchResult, len(results))
	for _, r := range results {
		m[r.ID] = r
	}
	s.mu.Lock()
	s.last = m
	s.mu.Unlock()
	return results, nil
}

// Execute activates the result with the given ID (from the last Search).
// An empty actionID runs the PrimaryAction; otherwise the ActionItem with
// the matching ID runs. Plugin actions return a secondary result list
// (merged into the session cache for further activation); every terminal
// action returns nil, which the UI treats as "hide the window". Usage is
// recorded against the result ID on success.
func (s *SearchService) Execute(id, actionID string) ([]core.SearchResult, error) {
	s.mu.Lock()
	r, ok := s.last[id]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no search result with id %q", id)
	}

	action := r.PrimaryAction
	if actionID != "" {
		item, ok := findActionItem(r.Actions, actionID)
		if !ok {
			return nil, fmt.Errorf("result %q has no action %q", id, actionID)
		}
		action = item.Action
	}

	var err error
	switch action.Kind {
	case core.ActionLaunchApp:
		if s.launcher == nil {
			err = fmt.Errorf("launcher unavailable")
			break
		}
		err = s.launcher.Launch(core.AppEntry{ID: r.ID, Name: r.Title, Path: action.Arg})
	case core.ActionOpenURL:
		err = platform.OpenURL(s.externalBrowser(), action.Arg)
	case core.ActionCopyText:
		if s.clipboard == nil {
			err = fmt.Errorf("clipboard unavailable")
			break
		}
		err = s.clipboard.WriteText(action.Arg)
	case core.ActionOpenPath:
		if s.pathOpener == nil {
			err = fmt.Errorf("path opener unavailable")
			break
		}
		err = s.pathOpener.OpenPath(action.Arg)
	case core.ActionRevealPath:
		if s.pathOpener == nil {
			err = fmt.Errorf("path opener unavailable")
			break
		}
		err = s.pathOpener.RevealPath(action.Arg)
	case core.ActionPlugin:
		return s.runPluginAction(id, action)
	default:
		err = fmt.Errorf("unknown action kind %d", action.Kind)
	}
	if err != nil {
		return nil, err
	}
	s.recordUsage(id)
	return nil, nil
}

// runPluginAction dispatches to the plugin manager and merges the returned
// secondary rows into the session cache (IDs are plugin-namespaced, so
// they cannot clash with first-level rows).
func (s *SearchService) runPluginAction(id string, action core.Action) ([]core.SearchResult, error) {
	if s.mgr == nil {
		return nil, fmt.Errorf("plugin system unavailable")
	}
	results, err := s.mgr.RunAction(context.Background(), action.PluginID, action.ActionID, action.Args)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	for _, res := range results {
		s.last[res.ID] = res
	}
	s.mu.Unlock()
	s.recordUsage(id)
	return results, nil
}

// findActionItem locates a secondary action by its ID.
func findActionItem(items []core.ActionItem, actionID string) (core.ActionItem, bool) {
	for _, item := range items {
		if item.ID == actionID {
			return item, true
		}
	}
	return core.ActionItem{}, false
}

// recordUsage bumps the frecency counter of the activated result.
func (s *SearchService) recordUsage(id string) {
	if s.store == nil {
		return
	}
	if err := s.store.Record(id, time.Now()); err != nil {
		log.Printf("store: record %q: %v", id, err)
	}
}

// Folder source management (thin bridges to the folders controller).

// FolderSources lists every configured folder source with scan status.
func (s *SearchService) FolderSources() ([]core.FolderSourceInfo, error) {
	if s.folderCtl == nil {
		return nil, fmt.Errorf("search service not started")
	}
	return s.folderCtl.Sources(), nil
}

// AddFolderSource registers and synchronously scans a new root.
func (s *SearchService) AddFolderSource(path string, maxDepth int) (core.FolderSource, error) {
	if s.folderCtl == nil {
		return core.FolderSource{}, fmt.Errorf("search service not started")
	}
	return s.folderCtl.AddSource(context.Background(), path, maxDepth)
}

// RemoveFolderSource deletes a source and its index entries.
func (s *SearchService) RemoveFolderSource(id string) error {
	if s.folderCtl == nil {
		return fmt.Errorf("search service not started")
	}
	return s.folderCtl.RemoveSource(id)
}

// SetFolderSourceEnabled toggles a source without dropping its cache.
func (s *SearchService) SetFolderSourceEnabled(id string, enabled bool) error {
	if s.folderCtl == nil {
		return fmt.Errorf("search service not started")
	}
	return s.folderCtl.SetEnabled(context.Background(), id, enabled)
}

// RefreshFolderSource rescans one source.
func (s *SearchService) RefreshFolderSource(id string) error {
	if s.folderCtl == nil {
		return fmt.Errorf("search service not started")
	}
	return s.folderCtl.RefreshSource(context.Background(), id)
}

// RefreshAllFolderSources rescans every enabled source.
func (s *SearchService) RefreshAllFolderSources() error {
	if s.folderCtl == nil {
		return fmt.Errorf("search service not started")
	}
	return s.folderCtl.RefreshAll(context.Background())
}

// PickFolderSourcePath opens the native directory chooser and returns the
// selected path. An empty string with a nil error means the user cancelled;
// no configuration is written here.
func (s *SearchService) PickFolderSourcePath() (string, error) {
	app := application.Get()
	if app == nil || app.Dialog == nil {
		return "", fmt.Errorf("dialog unavailable")
	}
	return app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(false).
		SetTitle("Choose Folder").
		PromptForSingleSelection()
}

// Plugins returns every loaded plugin (including disabled ones) for the
// settings window.
func (s *SearchService) Plugins() ([]kyvroplugin.PluginInfo, error) {
	if s.mgr == nil {
		return nil, fmt.Errorf("plugin system unavailable")
	}
	return s.mgr.ListPlugins(), nil
}

// SetPluginEnabled enables or disables a plugin and persists the choice.
func (s *SearchService) SetPluginEnabled(id string, enabled bool) error {
	if s.mgr == nil {
		return fmt.Errorf("plugin system unavailable")
	}
	return s.mgr.SetEnabled(id, enabled)
}

// RevealPluginsFolder opens the plugins directory in Finder (created on
// demand), the install/uninstall entry point alongside the settings list.
func (s *SearchService) RevealPluginsFolder() error {
	if err := os.MkdirAll(s.pluginsDir, 0o755); err != nil {
		return fmt.Errorf("create plugins dir: %w", err)
	}
	return browser.OpenFile(s.pluginsDir)
}

// AppVersion is the Kyvro application version shown in the About pane.
const AppVersion = "0.1.0"

// Version returns the Kyvro application version.
func (s *SearchService) Version() string { return AppVersion }

// Settings persistence (bbolt namespace "settings").
const (
	settingsNamespace  = "settings"
	externalBrowserKey = "external-browser" // "" = system default
)

// Browsers lists installed browser names usable as the external browser
// (empty on platforms without browser detection).
func (s *SearchService) Browsers() []string { return platform.InstalledBrowsers() }

// ExternalBrowser returns the configured external browser for open-url
// actions ("" = system default).
func (s *SearchService) ExternalBrowser() (string, error) {
	if s.store == nil {
		return "", fmt.Errorf("search service not started")
	}
	v, _, err := s.store.GetNS(settingsNamespace, externalBrowserKey)
	if err != nil {
		return "", err
	}
	return v, nil // absent key → "" → system default
}

// SetExternalBrowser sets the external browser ("" = system default); the
// name must be one of Browsers().
func (s *SearchService) SetExternalBrowser(app string) error {
	if s.store == nil {
		return fmt.Errorf("search service not started")
	}
	if app != "" && !containsString(platform.InstalledBrowsers(), app) {
		return fmt.Errorf("unknown browser %q", app)
	}
	if app == "" {
		return s.store.DeleteNS(settingsNamespace, externalBrowserKey)
	}
	return s.store.PutNS(settingsNamespace, externalBrowserKey, app)
}

// externalBrowser loads the preference, degrading to the system default on
// read errors.
func (s *SearchService) externalBrowser() string {
	if s.store == nil {
		return ""
	}
	v, ok, err := s.store.GetNS(settingsNamespace, externalBrowserKey)
	if err != nil || !ok {
		if err != nil {
			log.Printf("settings: read external browser: %v", err)
		}
		return ""
	}
	return v
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Remote plugin management methods

// AvailablePlugins fetches the list of plugins available from the official registry.
func (s *SearchService) AvailablePlugins() ([]kyvroplugin.RemotePlugin, error) {
	if s.installer == nil {
		return nil, fmt.Errorf("plugin installer not initialized")
	}
	registry := kyvroplugin.NewRegistryClient()
	return registry.FetchPlugins()
}

// InstallPlugin installs a plugin from the official registry by ID.
func (s *SearchService) InstallPlugin(id string) error {
	if s.installer == nil {
		return fmt.Errorf("plugin installer not initialized")
	}

	// Install the plugin
	if err := s.installer.InstallFromGitHub(id); err != nil {
		return fmt.Errorf("install plugin: %w", err)
	}

	// Reload plugins to pick up the newly installed one
	if s.mgr != nil {
		s.mgr.LoadAll()
	}

	return nil
}

// UninstallPlugin removes an installed plugin by ID.
func (s *SearchService) UninstallPlugin(id string) error {
	if s.installer == nil {
		return fmt.Errorf("plugin installer not initialized")
	}

	// First, ensure the plugin is stopped
	if s.mgr != nil {
		if err := s.mgr.SetEnabled(id, false); err != nil {
			log.Printf("plugin: disable %s before uninstall: %v", id, err)
		}
	}

	// Remove the plugin directory
	if err := s.installer.Uninstall(id); err != nil {
		return fmt.Errorf("uninstall plugin: %w", err)
	}

	// Reload to update the plugin list
	if s.mgr != nil {
		s.mgr.LoadAll()
	}

	return nil
}

// AllPlugins combines installed and available plugins for the settings UI.
func (s *SearchService) AllPlugins() ([]kyvroplugin.PluginInfo, error) {
	if s.mgr == nil || s.installer == nil {
		return nil, fmt.Errorf("plugin system not initialized")
	}

	// Get installed plugins
	installed := make(map[string]kyvroplugin.PluginInfo)
	for _, p := range s.mgr.ListPlugins() {
		installed[p.ID] = p
	}

	// Get available plugins from registry
	registry := kyvroplugin.NewRegistryClient()
	available, err := registry.FetchPlugins()
	if err != nil {
		log.Printf("plugin: fetch available plugins: %v", err)
		// Return only installed plugins on registry fetch failure
		result := make([]kyvroplugin.PluginInfo, 0, len(installed))
		for _, p := range installed {
			result = append(result, p)
		}
		return result, nil
	}

	// Merge installed and available plugins
	pluginMap := make(map[string]kyvroplugin.PluginInfo)

	// Add installed plugins
	for id, p := range installed {
		pluginMap[id] = p
	}

	// Add available plugins that aren't installed
	for _, remote := range available {
		if _, exists := installed[remote.ID]; !exists {
			pluginMap[remote.ID] = kyvroplugin.PluginInfo{
				ID:          remote.ID,
				Name:        remote.Name,
				Version:     remote.Version,
				Description: remote.Description,
				Author:      remote.Author,
				IconURL:     remote.IconURL,
				Status:      kyvroplugin.StatusNotInstalled,
				DownloadURL: remote.DownloadURL,
			}
		}
	}

	// Convert map to slice and sort
	result := make([]kyvroplugin.PluginInfo, 0, len(pluginMap))
	for _, p := range pluginMap {
		result = append(result, p)
	}

	// Sort by status (enabled first, then installed, then not installed) and by name
	sort.Slice(result, func(i, j int) bool {
		if result[i].Status != result[j].Status {
			// Order: Enabled > Installed > NotInstalled
			statusOrder := map[kyvroplugin.PluginStatus]int{
				kyvroplugin.StatusEnabled:      0,
				kyvroplugin.StatusInstalled:    1,
				kyvroplugin.StatusNotInstalled: 2,
			}
			return statusOrder[result[i].Status] < statusOrder[result[j].Status]
		}
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// Snippets methods
//
// TODO(snippets): the Text Snippets feature is temporarily disabled and kept
// for a future release. These bound methods (and their generated frontend
// bindings) remain in place so nothing needs regenerating when the feature
// returns; with s.snippets/s.textExpander left nil, Snippets() returns an
// empty list and the mutation methods report "snippets service not
// initialized". Data in bbolt ("snippets" namespace) is preserved.

// Snippets returns all configured text snippets.
func (s *SearchService) Snippets() ([]core.Snippet, error) {
	if s.snippets == nil {
		return []core.Snippet{}, nil
	}
	return s.snippets.List()
}

// AddSnippet adds a new text snippet.
func (s *SearchService) AddSnippet(trigger, replacement string) error {
	if s.snippets == nil {
		return fmt.Errorf("snippets service not initialized")
	}
	if err := s.snippets.Add(core.Snippet{
		Trigger:     trigger,
		Replacement: replacement,
	}); err != nil {
		return err
	}
	return s.refreshTextExpanderIfEnabled()
}

// RemoveSnippet removes a text snippet by trigger.
func (s *SearchService) RemoveSnippet(trigger string) error {
	if s.snippets == nil {
		return fmt.Errorf("snippets service not initialized")
	}
	if err := s.snippets.Remove(trigger); err != nil {
		return err
	}
	return s.refreshTextExpanderIfEnabled()
}

// SetSnippetEnabled enables or disables a snippet.
func (s *SearchService) SetSnippetEnabled(trigger string, enabled bool) error {
	if s.snippets == nil {
		return fmt.Errorf("snippets service not initialized")
	}
	if err := s.snippets.SetEnabled(trigger, enabled); err != nil {
		return err
	}
	return s.refreshTextExpanderIfEnabled()
}

// SnippetsEnabled checks if text expansion is globally enabled.
func (s *SearchService) SnippetsEnabled() (bool, error) {
	if s.store == nil {
		return false, fmt.Errorf("search service not started")
	}
	v, _, err := s.store.GetNS(settingsNamespace, "snippets-enabled")
	if err != nil {
		return false, err
	}
	// Default to enabled if not set
	if v == "" || v == "true" {
		return true, nil
	}
	return false, nil
}

// SetSnippetsEnabled enables or disables text expansion globally.
func (s *SearchService) SetSnippetsEnabled(enabled bool) error {
	if s.store == nil {
		return fmt.Errorf("search service not started")
	}
	value := "true"
	if !enabled {
		value = "false"
	}

	// Save to store first
	if err := s.store.PutNS(settingsNamespace, "snippets-enabled", value); err != nil {
		return err
	}

	if enabled {
		return s.refreshTextExpander()
	}
	if s.textExpander != nil {
		return s.textExpander.Stop()
	}

	return nil
}

// SnippetAccessibilityGranted reports whether the current Kyvro process has
// macOS Accessibility permission required for global text expansion.
func (s *SearchService) SnippetAccessibilityGranted() (bool, error) {
	if s.textExpander == nil {
		return false, fmt.Errorf("text expander not initialized")
	}
	return s.textExpander.IsEnabled()
}

// RequestSnippetAccessibility asks macOS to grant Accessibility permission to
// the current Kyvro process.
func (s *SearchService) RequestSnippetAccessibility() error {
	if s.textExpander == nil {
		return fmt.Errorf("text expander not initialized")
	}
	return s.textExpander.RequestPermissions()
}

func (s *SearchService) refreshTextExpanderIfEnabled() error {
	enabled, err := s.SnippetsEnabled()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	return s.refreshTextExpander()
}

func (s *SearchService) refreshTextExpander() error {
	if s.textExpander == nil || s.snippets == nil {
		return nil
	}
	snippets, err := s.snippets.GetEnabled()
	if err != nil {
		return err
	}
	enabledMap := make(map[string]string, len(snippets))
	for trigger, sn := range snippets {
		enabledMap[trigger] = sn.Replacement
	}
	if len(enabledMap) == 0 {
		return s.textExpander.Stop()
	}
	return s.textExpander.Start(enabledMap)
}
