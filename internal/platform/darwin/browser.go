package darwin

import (
	"os"
	"os/exec"
	"path/filepath"
)

// browserCandidates are common macOS browsers, reported by InstalledBrowsers
// when their .app bundle exists in a standard root. Names are exactly what
// `open -a` expects.
var browserCandidates = []string{
	"Safari",
	"Google Chrome",
	"Microsoft Edge",
	"Arc",
	"Firefox",
	"Brave Browser",
	"Chromium",
	"Opera",
	"Vivaldi",
	"DuckDuckGo",
}

// InstalledBrowsers lists installed browser app names, candidate order.
func InstalledBrowsers() []string {
	roots := []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")}
	var out []string
	for _, name := range browserCandidates {
		for _, root := range roots {
			if _, err := os.Stat(filepath.Join(root, name+".app")); err == nil {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

// OpenURL opens url in the named browser app; browserApp == "" uses the
// system default browser. Run (not Start) surfaces "unknown browser" errors
// while still returning quickly — `open` hands off without waiting for the
// browser to exit.
func OpenURL(browserApp, url string) error {
	if browserApp == "" {
		return exec.Command("open", url).Run()
	}
	return exec.Command("open", "-a", browserApp, url).Run()
}
