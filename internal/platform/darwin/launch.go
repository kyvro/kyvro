//go:build darwin

package darwin

import (
	"fmt"
	"os/exec"
	"strings"

	"kyvro/internal/core"
)

// AppLauncher starts applications via open(1). Without -n, open reuses an
// already-running instance, which matches launcher expectations.
type AppLauncher struct{}

// NewAppLauncher creates a launcher.
func NewAppLauncher() *AppLauncher { return &AppLauncher{} }

// Launch opens the app located at app.Path.
func (l *AppLauncher) Launch(app core.AppEntry) error {
	if app.Path == "" {
		return fmt.Errorf("launch %q: empty path", app.Name)
	}
	out, err := exec.Command("open", "-a", app.Path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("open -a %s: %w: %s", app.Path, err, strings.TrimSpace(string(out)))
	}
	return nil
}
