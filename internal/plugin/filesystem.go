package plugin

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/dop251/goja"
)

const fileReadPrefix = "filesystem:read:"

// Exact files only: no directory grants, wildcards or environment expansion.
func fileReadPath(name string) (string, bool) {
	if !strings.HasPrefix(name, fileReadPrefix) {
		return "", false
	}
	path := strings.TrimPrefix(name, fileReadPrefix)
	if strings.ContainsAny(path, "*?\x00") || path == "" {
		return "", false
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) == string(filepath.Separator) {
		return "", false
	}
	return filepath.Clean(path), true
}
func validFileReadPermission(name string) bool { _, ok := fileReadPath(name); return ok }

func (r *jsRuntime) readTextFile(path string) (string, error) {
	target, ok := fileReadPath(fileReadPrefix + path)
	if !ok {
		return "", Errorf(r.pluginID, ErrInvalidArgument, "readText requires an absolute or ~/ file path")
	}
	allowed := false
	for permission, granted := range r.perms.granted {
		scope, valid := fileReadPath(permission)
		if granted && valid && scope == target {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", Errorf(r.pluginID, ErrPermissionDenied, "file read not granted")
	}
	// OpenRoot confines resolution to the approved parent directory, including races.
	root, err := os.OpenRoot(filepath.Dir(target))
	if err != nil {
		return "", Errorf(r.pluginID, ErrHostAPIError, "cannot open granted file directory")
	}
	defer root.Close()
	name := filepath.Base(target)
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return "", Errorf(r.pluginID, ErrHostAPIError, "granted file is unavailable or not a regular file (symlinks unsupported)")
	}
	file, err := root.Open(name)
	if err != nil {
		return "", Errorf(r.pluginID, ErrHostAPIError, "cannot open granted file")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return "", Errorf(r.pluginID, ErrHostAPIError, "granted file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes || !utf8.Valid(data) {
		return "", Errorf(r.pluginID, ErrHostAPIError, "file must be UTF-8 text no larger than 2MiB")
	}
	return string(data), nil
}
func (r *jsRuntime) installFilesystem(vm *goja.Runtime, ctx *goja.Object) {
	for permission, granted := range r.perms.granted {
		if !granted || !validFileReadPermission(permission) {
			continue
		}
		files := vm.NewObject()
		files.Set("readText", func(path string) string {
			text, err := r.readTextFile(path)
			if err != nil {
				panic(vm.NewGoError(err))
			}
			return text
		})
		ctx.Set("filesystem", files)
		return
	}
}
