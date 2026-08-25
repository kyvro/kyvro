// Package service hosts the Wails-bound services: thin bridges between the
// Vue frontend and the pure-Go launcher core. No business logic lives here.
package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v3/pkg/application"

	"kyvro/internal/core"
	"kyvro/internal/platform"
	kyvroplugin "kyvro/internal/plugin"
	"kyvro/internal/providers/apps"
	"kyvro/internal/providers/calc"
	"kyvro/internal/providers/web"
)

// SearchService is bound to the frontend by Wails. Search feeds the result
// list; Launch activates a result from the most recent Search and records
// usage (frecency); the plugin methods back the settings window.
//
// The engine, store and providers are constructed lazily in ServiceStartup:
// the single-instance guard inside application.New must get the chance to
// exit a duplicate process before this service contends on the bbolt lock.
type SearchService struct {
	dataPath   string
	pluginsDir string
	source     platform.AppSource
	launcher   platform.AppLauncher

	initOnce sync.Once
	store    *core.Store
	engine   *core.Engine
	mgr      *kyvroplugin.Manager
	initErr  error

	mu   sync.Mutex
	last map[string]core.SearchResult
}

// New creates the search service. dataPath is the bbolt usage database;
// plugins live in the "plugins" directory next to it.
func New(dataPath string, source platform.AppSource, launcher platform.AppLauncher) *SearchService {
	return &SearchService{
		dataPath:   dataPath,
		pluginsDir: filepath.Join(filepath.Dir(dataPath), "plugins"),
		source:     source,
		launcher:   launcher,
		last:       make(map[string]core.SearchResult),
	}
}

// ServiceStartup opens the store, wires providers (calc, apps, plugins, web
// — plugins strictly between apps and the web fallback) into the engine and
// kicks off the background app scan.
func (s *SearchService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	s.initOnce.Do(func() {
		store, err := core.OpenStore(s.dataPath)
		if err != nil {
			s.initErr = fmt.Errorf("open usage store: %w", err)
			return
		}
		mgr := kyvroplugin.NewManager(s.pluginsDir, store, nil)
		mgr.LoadAll() // single-plugin failures are logged, not fatal
		appsProvider := apps.New(s.source)
		engine := core.NewEngine(
			[]core.Provider{calc.New(), appsProvider, mgr.Provider(), web.New()},
			store,
			0, // DefaultLimit
		)
		go appsProvider.Warmup()

		s.store = store
		s.mgr = mgr
		s.engine = engine
	})
	return s.initErr
}

// ServiceShutdown stops the plugin runtimes first (so in-flight plugin calls
// finish against a live store), then closes the usage store.
func (s *SearchService) ServiceShutdown() error {
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

// Launch activates the result with the given ID (from the last Search),
// records usage and hides the summon window.
func (s *SearchService) Launch(id string) error {
	s.mu.Lock()
	r, ok := s.last[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("no search result with id %q", id)
	}

	var err error
	switch r.Action.Kind {
	case core.ActionLaunchApp:
		err = s.launcher.Launch(core.AppEntry{ID: r.ID, Name: r.Title, Path: r.Action.Arg})
	case core.ActionOpenURL:
		err = platform.OpenURL(s.externalBrowser(), r.Action.Arg)
	case core.ActionCopyText:
		if !application.Get().Clipboard.SetText(r.Action.Arg) {
			err = fmt.Errorf("copy to clipboard failed")
		}
	case core.ActionPlugin:
		err = fmt.Errorf("plugin actions must go through RunAction")
	default:
		err = fmt.Errorf("unknown action kind %d", r.Action.Kind)
	}
	if err != nil {
		return err
	}

	if s.store != nil {
		if rerr := s.store.Record(id, time.Now()); rerr != nil {
			log.Printf("store: record %q: %v", id, rerr)
		}
	}
	return nil
}

// RunAction dispatches a plugin row (command or callback) from the last
// Search to its plugin and returns the secondary result list. Rows whose
// actions are open-url/copy keep flowing through Launch: the returned rows
// are merged into the same session cache, so activating them works with no
// extra plumbing. An empty list means "nothing to show" (the frontend hides
// the window).
func (s *SearchService) RunAction(id string) ([]core.SearchResult, error) {
	s.mu.Lock()
	r, ok := s.last[id]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no search result with id %q", id)
	}
	if r.Action.Kind != core.ActionPlugin {
		return nil, fmt.Errorf("result %q is not a plugin action", id)
	}
	if s.mgr == nil {
		return nil, fmt.Errorf("plugin system unavailable")
	}

	results, err := s.mgr.RunAction(context.Background(), r.Action.PluginID, r.Action.ActionID, r.Action.Args)
	if err != nil {
		return nil, err
	}

	// IDs are plugin-namespaced, so merging cannot clash with first-level
	// rows or other plugins' rows.
	s.mu.Lock()
	for _, res := range results {
		s.last[res.ID] = res
	}
	s.mu.Unlock()

	if s.store != nil {
		if rerr := s.store.Record(id, time.Now()); rerr != nil {
			log.Printf("store: record %q: %v", id, rerr)
		}
	}
	return results, nil
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
