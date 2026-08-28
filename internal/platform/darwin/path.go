//go:build darwin

package darwin

import (
	"fmt"
	"os/exec"
	"strings"
)

// PathOpenerImpl opens and reveals filesystem paths via open(1).
type PathOpenerImpl struct{}

// NewPathOpener creates a path opener.
func NewPathOpener() *PathOpenerImpl { return &PathOpenerImpl{} }

// OpenPath opens path with the default handler (folders open in Finder).
func (p *PathOpenerImpl) OpenPath(path string) error {
	if path == "" {
		return fmt.Errorf("open: empty path")
	}
	if out, err := exec.Command("open", path).CombinedOutput(); err != nil {
		return fmt.Errorf("open %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RevealPath selects path in a Finder window.
func (p *PathOpenerImpl) RevealPath(path string) error {
	if path == "" {
		return fmt.Errorf("open -R: empty path")
	}
	if out, err := exec.Command("open", "-R", path).CombinedOutput(); err != nil {
		return fmt.Errorf("open -R %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}
