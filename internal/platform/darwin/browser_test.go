package darwin

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInstalledBrowsersOnlyReportsExistingApps guards the contract with
// `open -a`: every reported name must resolve to an installed .app bundle.
func TestInstalledBrowsersOnlyReportsExistingApps(t *testing.T) {
	roots := []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")}
	for _, name := range InstalledBrowsers() {
		found := false
		for _, root := range roots {
			if _, err := os.Stat(filepath.Join(root, name+".app")); err == nil {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("reported browser %q has no .app bundle", name)
		}
	}
}
