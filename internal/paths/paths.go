// Package paths holds the path helpers shared by the configuration loader and
// the sandbox planner.
package paths

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Expand replaces a leading "~" or "$HOME" with home.  Nothing else is
// expanded: "~user" and variables other than $HOME are left alone.
func Expand(p, home string) string {
	switch {
	case p == "~" || p == "$HOME":
		return home
	case strings.HasPrefix(p, "~/"):
		return home + p[1:]
	case strings.HasPrefix(p, "$HOME/"):
		return home + p[len("$HOME"):]
	}
	return p
}

// Resolve returns the absolute path of p with every symlink resolved.  The
// path must exist.
func Resolve(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// ResolveMaybe behaves like "realpath -m": relative paths are joined to cwd,
// the longest existing prefix has its symlinks resolved, and the missing
// remainder is appended unchanged.
func ResolveMaybe(p, cwd string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	p = filepath.Clean(p)
	rest := ""
	for prefix := p; ; prefix = filepath.Dir(prefix) {
		if resolved, err := filepath.EvalSymlinks(prefix); err == nil {
			return filepath.Join(resolved, rest)
		}
		if prefix == "/" {
			return p
		}
		rest = filepath.Join(filepath.Base(prefix), rest)
	}
}

// Host describes the host paths that must never be mounted wholesale.
type Host struct {
	Home       string
	RuntimeDir string
	UID        int
}

// IsBroad reports whether mounting p would expose too much of the host: /,
// HOME and its ancestors, the user runtime directory and top-level
// directories.
func (h Host) IsBroad(p string) bool {
	switch p {
	case "/", h.Home, h.RuntimeDir, "/run/user", fmt.Sprintf("/run/user/%d", h.UID):
		return true
	}
	if strings.HasPrefix(h.Home, p+"/") {
		return true
	}
	return isTopLevel(p)
}

func isTopLevel(p string) bool {
	return len(p) > 1 && p[0] == '/' && !strings.Contains(p[1:], "/")
}

// Contains reports whether p is parent or lies below it.
func Contains(parent, p string) bool {
	return p == parent || strings.HasPrefix(p, parent+"/")
}
