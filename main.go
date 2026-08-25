// Kyvro — a cross-platform launcher (v0.1: macOS first).
//
// launcher-app: owns the Wails application lifecycle — the summon window,
// the global ⌥Space hotkey, the systray menu and single-instance guard —
// and wires the pure-Go core (engine + providers + store) into the UI.
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"kyvro/internal/core"
	"kyvro/internal/platform"
	"kyvro/service"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/darwin/tray-template.png
var trayTemplateIcon []byte

// Window geometry: Alfred-like width, centred horizontally, upper quarter
// of the primary screen.
const (
	windowWidth  = 680
	windowHeight = 440
	// Settings window: a regular centered utility window.
	settingsWindowWidth  = 760
	settingsWindowHeight = 640
)

func main() {
	dataPath, err := core.DefaultDataPath()
	if err != nil {
		log.Fatalf("resolve data path: %v", err)
	}

	// summon is assigned once the window exists; a second instance launch
	// uses it to reveal the running instance instead.
	var summon func()

	// The store/engine are opened inside the service's startup hook so the
	// single-instance guard below runs before anything contends on bbolt.
	searchService := service.New(dataPath, platform.NewAppSource(), platform.NewAppLauncher())

	app := application.New(application.Options{
		Name:        "Kyvro",
		Description: "Fast, keyboard-first launcher",
		Services: []application.Service{
			application.NewService(searchService),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: service.IconMiddleware(),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.kyvro.launcher",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if summon != nil {
					summon()
				}
			},
		},
		Mac: application.MacOptions{
			// Accessory: no Dock icon, no menu bar — a hotkey launcher.
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "main",
		Title:           "Kyvro",
		Width:           windowWidth,
		Height:          windowHeight,
		Frameless:       true,
		DisableResize:   true,
		AlwaysOnTop:     true,
		Hidden:          true, // summoned via ⌥Space
		HideOnFocusLost: true,
		BackgroundType:  application.BackgroundTypeTransparent,
		InitialPosition: application.WindowCentered,
		URL:             "/",
		Mac: application.MacWindow{
			Backdrop:   application.MacBackdropTranslucent,
			Appearance: application.NSAppearanceNameDarkAqua,
		},
	})

	toggle := func() {
		if win.IsVisible() {
			win.Hide()
			return
		}
		centreUpper(app, win)
		win.Show()
		win.Focus()
		app.Event.Emit("kyvro:shown", nil)
	}
	summon = toggle

	// Settings lives in its own regular (titled, non-overlay) window; the
	// SPA routes on the URL hash. Created lazily on first open (no preload,
	// no startup cost); clicking again while open just focuses it, and a
	// closed window (beta.12 cannot intercept close) is recreated on demand.
	// The handler itself is trivial, so any first-open wait is the
	// framework's webview creation, not host logic.
	openSettings := func() {
		if w, ok := app.Window.GetByName("settings"); ok {
			w.Show()
			w.Focus()
			return
		}
		// Focus also activates the app (activateIgnoringOtherApps) — without
		// it the accessory app's new window layers behind the active app
		// (e.g. a terminal).
		app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:            "settings",
			Title:           "Kyvro Settings",
			Width:           settingsWindowWidth,
			Height:          settingsWindowHeight,
			InitialPosition: application.WindowCentered,
			URL:             "/#settings",
			Mac: application.MacWindow{
				Appearance: application.NSAppearanceNameDarkAqua,
			},
		}).Focus()
	}

	// Global hotkey. Fall back to ⌥⌘Space when ⌥Space is taken.
	if err := app.GlobalShortcut.Register("Alt+Space", toggle); err != nil {
		log.Printf("hotkey: Alt+Space unavailable (%v); trying Alt+Cmd+Space", err)
		if err2 := app.GlobalShortcut.Register("Alt+Cmd+Space", toggle); err2 != nil {
			log.Printf("hotkey: Alt+Cmd+Space also unavailable: %v", err2)
		}
	}

	tray := app.SystemTray.New()
	tray.SetTemplateIcon(trayTemplateIcon)
	tray.SetTooltip("Kyvro launcher — ⌥Space")
	menu := application.NewMenu()
	menu.Add("Show Kyvro").OnClick(func(*application.Context) { toggle() })
	menu.Add("Settings…").OnClick(func(*application.Context) { openSettings() })
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(*application.Context) { app.Quit() })
	// No click handlers: with a menu and no attached window, both left and
	// right clicks open the menu via native tracking (proper highlight).
	// The summon window stays bound to the hotkey and "Show Kyvro".
	tray.SetMenu(menu)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// windowTopMargin is the fixed distance between the top of the screen
// and the window — high launcher position, clear of the menu bar/notch.
const windowTopMargin = 140

// centreUpper positions the window horizontally centred on the primary
// screen with its top a fixed margin below the top edge, so it sits at
// the same comfortable height on laptop and large external displays.
func centreUpper(app *application.App, win application.Window) {
	screen := app.Screen.GetPrimary()
	if screen == nil {
		return
	}
	x := screen.X + (screen.Size.Width-windowWidth)/2
	y := screen.Y + windowTopMargin
	if x < screen.X {
		x = screen.X
	}
	if y < screen.Y {
		y = screen.Y
	}
	win.SetPosition(x, y)
}
