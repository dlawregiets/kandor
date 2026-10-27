// Package sandbox plans the bwrap command line: mounts, namespaces and the
// environment.  Planning has no side effects beyond reading the filesystem
// (and creating an empty .git/hooks directory to protect it).
package sandbox

import "os"

// Plan accumulates bwrap arguments (without the leading "bwrap").
type Plan struct {
	features Features
	args     []string
	mounted  map[string]bool
}

func NewPlan(f Features) *Plan {
	return &Plan{features: f, mounted: map[string]bool{}}
}

// RO and RW bind src at dest unless something is already planned at dest.
func (p *Plan) RO(src, dest string) { p.once("--ro-bind", src, dest) }
func (p *Plan) RW(src, dest string) { p.once("--bind", src, dest) }

func (p *Plan) once(kind, src, dest string) {
	if p.mounted[dest] {
		return
	}
	p.mounted[dest] = true
	p.args = append(p.args, kind, src, dest)
}

// Bind always adds the mount, so a later mount of the same path wins.
func (p *Plan) Bind(src, dest string, writable bool) {
	kind := "--ro-bind"
	if writable {
		kind = "--bind"
	}
	p.args = append(p.args, kind, src, dest)
}

// Tmpfs hides dest behind an empty tmpfs.  Missing paths are skipped.
func (p *Plan) Tmpfs(dest, mode string) {
	if _, err := os.Stat(dest); err != nil {
		return
	}
	p.args = append(p.args, "--tmpfs", dest)
	p.chmod(dest, mode)
}

// PrivateDir creates dest and puts an empty tmpfs on it.
func (p *Plan) PrivateDir(dest, mode string) {
	p.args = append(p.args, "--dir", dest, "--tmpfs", dest)
	p.chmod(dest, mode)
}

func (p *Plan) chmod(dest, mode string) {
	if mode != "" && p.features.Chmod {
		p.args = append(p.args, "--chmod", mode, dest)
	}
}

func (p *Plan) Arg(args ...string)       { p.args = append(p.args, args...) }
func (p *Plan) Args() []string           { return p.args }
func (p *Plan) Features() Features       { return p.features }
func (p *Plan) Mounted(dest string) bool { return p.mounted[dest] }
