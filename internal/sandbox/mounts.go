package sandbox

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dlawregiets/kandor/internal/agent"
	"github.com/dlawregiets/kandor/internal/config"
	"github.com/dlawregiets/kandor/internal/paths"
)

// Input is everything the mount planner needs, already resolved.
type Input struct {
	Settings   config.Settings
	Host       paths.Host
	Features   Features
	AgentBinds []agent.Bind
	AgentMasks []agent.Mask
	// Binary is the resolved agent (or debug shell) executable.
	Binary    string
	XDGConfig string
	Getenv    func(string) string
	Warn      func(string)
}

// Result carries what later steps need from the mount planner.
type Result struct {
	Git GitState
	// GitConfigGlobal is set inside the sandbox before any --env, so an
	// explicit --env GIT_CONFIG_GLOBAL still wins.
	GitConfigGlobal string
	// SSHAuthSock is the resolved agent socket, "" when not forwarded.
	SSHAuthSock string
}

// Files named by these variables are mounted read-only so providers can use
// them.
var credentialFileVars = []string{
	"GOOGLE_APPLICATION_CREDENTIALS", "NODE_EXTRA_CA_CERTS", "SSL_CERT_FILE",
	"CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "AWS_SHARED_CREDENTIALS_FILE",
	"AWS_CONFIG_FILE", "AWS_CA_BUNDLE", "GIT_SSL_CAINFO", "GIT_SSL_CAPATH",
}

// BuildMounts plans the filesystem: a read-only host, a hidden home, private
// scratch and runtime directories, the project, the agent's state, tools,
// opt-in credentials and the configured extra mounts, in that order.
func BuildMounts(p *Plan, in Input) (Result, error) {
	var r Result
	s, h := in.Settings, in.Host
	home, project := h.Home, s.ProjectDir

	p.Bind("/", "/", false)
	p.Arg("--dev", "/dev", "--proc", "/proc")

	// Hide the home directory.  bwrap resolves mount sources before applying
	// mounts, so binding paths below HOME afterwards still works.
	homeParent := home
	for _, parent := range []string{"/home", "/tmp", "/var/tmp", "/mnt", "/media", "/srv"} {
		if strings.HasPrefix(home, parent+"/") {
			homeParent = parent
		}
	}
	if home == "/root" {
		homeParent = "/root"
	}
	mode := ""
	if homeParent == "/tmp" || homeParent == "/var/tmp" {
		mode = "1777"
	}
	p.Tmpfs(homeParent, mode)
	p.Arg("--dir", home)

	for _, d := range []struct{ path, mode string }{
		{"/root", ""}, {"/tmp", "1777"}, {"/var/tmp", "1777"}, {"/mnt", ""}, {"/media", ""}, {"/srv", ""},
	} {
		// Never hide a directory that contains HOME.
		if !paths.Contains(d.path, home) {
			p.Tmpfs(d.path, d.mode)
		}
	}

	// Hide host runtime sockets (system DBus, journal, keyrings, ...) but keep
	// the resolver configuration /etc/resolv.conf points at, or DNS breaks on
	// systemd-resolved systems.
	p.Tmpfs("/run", "")
	// In case HOME lives under /run, recreate it after the tmpfs.
	p.Arg("--dir", home)
	if target, err := paths.Resolve("/etc/resolv.conf"); err == nil && strings.HasPrefix(target, "/run/") {
		// Recreate only the resolver file, not the rest of its directory
		// (journal, varlink and other host sockets).
		p.PrivateDir(filepath.Dir(target), "")
		p.RO(target, target)
	}

	// A private runtime directory: host DBus, keyring and agent sockets stay
	// out unless explicitly requested below.
	p.PrivateDir(h.RuntimeDir, "0700")

	p.RW(project, project)

	r.Git = DiscoverGit(project, h, p, in.Warn)
	if s.ProtectGit && r.Git.Trusted {
		r.Git.Protect(p, project, in.Warn)
	}
	if s.ProtectGit {
		if _, err := os.Lstat(filepath.Join(project, ".git")); err != nil {
			r.Git.AuditNewRepo = true
		}
	}

	for _, b := range in.AgentBinds {
		if b.Writable {
			p.RW(b.Src, b.Dest)
		} else {
			p.RO(b.Src, b.Dest)
		}
		if b.FollowSymlinks {
			roSymlinkTargets(p, in, b.Src)
		}
	}

	if err := mountBinaryDir(p, in, homeParent); err != nil {
		return Result{}, err
	}
	if isDir(filepath.Join(home, ".bun")) {
		p.RO(filepath.Join(home, ".bun"), filepath.Join(home, ".bun"))
	}
	for _, d := range []string{".local/bin", "bin", ".opencode/bin", "go/bin", ".cargo/bin"} {
		d = filepath.Join(home, d)
		if isDir(d) {
			p.RO(d, d)
			roSymlinkTargets(p, in, d)
		}
	}

	// Git configuration.  Without it, commits inside the sandbox have no
	// author.
	if s.MountGitConfig {
		for _, f := range []string{".gitconfig", ".gitignore_global"} {
			if f = filepath.Join(home, f); isFile(f) {
				p.RO(f, f)
			}
		}
		xdgGit := filepath.Join(in.XDGConfig, "git")
		if g := in.Getenv("GIT_CONFIG_GLOBAL"); g != "" && exists(g) {
			if resolved, err := paths.Resolve(g); err == nil {
				p.RO(resolved, resolved)
				r.GitConfigGlobal = resolved
			}
		} else if in.XDGConfig != "" && isDir(xdgGit) {
			// Keep the host path: Git follows symlinks inside this directory,
			// and their targets live at their original host paths.
			p.RO(xdgGit, xdgGit)
			roSymlinkTargets(p, in, xdgGit)
			if isFile(filepath.Join(xdgGit, "config")) {
				r.GitConfigGlobal = filepath.Join(xdgGit, "config")
			}
		}
	}

	// Opt-in credentials.
	if s.SSH {
		if ssh := filepath.Join(home, ".ssh"); isDir(ssh) {
			p.RO(ssh, ssh)
			roSymlinkTargets(p, in, ssh)
		} else {
			in.Warn(fmt.Sprintf("--ssh was requested but %s does not exist", ssh))
		}
	}
	if s.SSHAgent {
		sock := in.Getenv("SSH_AUTH_SOCK")
		resolved, err := paths.Resolve(sock)
		if st, statErr := os.Stat(sock); sock != "" && statErr == nil && st.Mode()&fs.ModeSocket != 0 && err == nil {
			p.RO(resolved, resolved)
			r.SSHAuthSock = resolved
		} else {
			in.Warn("--ssh-agent was requested but SSH_AUTH_SOCK is not a socket")
		}
	}
	for _, c := range []struct {
		on   bool
		file string
		flag string
	}{
		{s.GitCredentials, ".git-credentials", "--git-credentials"},
		{s.Netrc, ".netrc", "--netrc"},
		{s.Npmrc, ".npmrc", "--npmrc"},
	} {
		if !c.on {
			continue
		}
		if f := filepath.Join(home, c.file); isFile(f) {
			p.RO(f, f)
		} else {
			in.Warn(fmt.Sprintf("%s was requested but %s does not exist", c.flag, f))
		}
	}

	// Credential and CA files named by forwarded environment variables.
	for _, name := range credentialFileVars {
		v := in.Getenv(name)
		if v == "" {
			continue
		}
		if resolved, err := paths.Resolve(v); err == nil {
			p.RO(resolved, resolved)
		} else {
			in.Warn(fmt.Sprintf("%s points to a path that does not exist: %s", name, v))
		}
	}
	if d := in.Getenv("SSL_CERT_DIR"); d != "" && isDir(d) {
		if resolved, err := paths.Resolve(d); err == nil {
			p.RO(resolved, resolved)
		}
	}

	// Explicit extra mounts, in layer order so a later one wins.
	for _, m := range s.Mounts {
		if err := extraMount(p, h, m); err != nil {
			return Result{}, err
		}
	}

	// Last, so no mount above can uncover them.
	for _, m := range in.AgentMasks {
		if err := mask(p, m); err != nil {
			return Result{}, err
		}
	}
	return r, nil
}

// mask covers m.Dest with m.Src wherever the planned mounts make it visible:
// at its own path and at its resolved path, which is where extra mounts put
// a symlinked directory.
func mask(p *Plan, m agent.Mask) error {
	dests := []string{m.Dest}
	if dir, err := paths.Resolve(filepath.Dir(m.Dest)); err == nil {
		if resolved := filepath.Join(dir, filepath.Base(m.Dest)); resolved != m.Dest {
			dests = append(dests, resolved)
		}
	}
	for _, dest := range dests {
		visible, writable := p.Visible(dest)
		if !visible {
			continue
		}
		if _, err := os.Lstat(dest); err != nil {
			if !writable {
				continue
			}
			f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err == nil {
				_, err = f.WriteString(m.Placeholder)
				if cerr := f.Close(); err == nil {
					err = cerr
				}
			}
			if err != nil {
				return fmt.Errorf("cannot create %s to keep the sandbox from writing it: %v", dest, err)
			}
		}
		p.Bind(m.Src, dest, true)
	}
	return nil
}

// mountBinaryDir mounts the directory holding the agent or shell binary.
// Mounting a hidden directory, HOME (or an ancestor) or a parent of the
// project would undo the isolation above; other top-level directories such as
// /bin are already visible through the read-only root.
func mountBinaryDir(p *Plan, in Input, homeParent string) error {
	dir := filepath.Dir(in.Binary)
	refuse := fmt.Errorf("refusing to mount the binary's directory '%s'; move the binary into its own directory (or point KANDOR_BIN at one)", dir)
	switch dir {
	case "/tmp", "/var/tmp", "/run", "/mnt", "/media", "/srv", "/root", homeParent:
		return refuse
	}
	if in.Host.IsBroad(dir) || paths.Contains(dir, in.Settings.ProjectDir) {
		if isTopLevel(dir) && dir != in.Host.Home {
			return nil
		}
		return refuse
	}
	p.RO(dir, dir)
	return nil
}

func extraMount(p *Plan, h paths.Host, m config.MountSpec) error {
	kind := "ro"
	if m.Writable {
		kind = "rw"
	}
	if m.Path == "" {
		return nil
	}
	if !exists(m.Path) {
		return fmt.Errorf("%s path does not exist: %s", kind, m.Path)
	}
	resolved, err := paths.Resolve(m.Path)
	if err != nil {
		return fmt.Errorf("%s path cannot be resolved: %s", kind, m.Path)
	}
	// Refuse the obvious ways to recreate the host inside the sandbox.
	// Deliberate subdirectory and file mounts are still possible.
	if isTopLevel(resolved) && resolved != h.Home {
		return fmt.Errorf("refusing to mount top-level directory '%s'; mount a subdirectory or file instead", resolved)
	}
	if h.IsBroad(resolved) {
		return fmt.Errorf("refusing to mount '%s'; mount a subdirectory or file instead", resolved)
	}
	p.Bind(resolved, resolved, m.Writable)
	return nil
}

// roSymlinkTargets mounts the resolved targets of symlinks inside dir at their
// own paths, so the links resolve inside the sandbox.  Targets that are broad
// host paths or parents of the project are skipped with a warning.
func roSymlinkTargets(p *Plan, in Input, dir string) {
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		target, err := paths.Resolve(path)
		if err != nil {
			return nil
		}
		switch {
		case in.Host.IsBroad(target):
			in.Warn(fmt.Sprintf("not following symlink to broad host path: %s -> %s", path, target))
		case paths.Contains(target, in.Settings.ProjectDir):
			in.Warn(fmt.Sprintf("not following symlink to a parent of the project: %s -> %s", path, target))
		default:
			p.RO(target, target)
		}
		return nil
	})
}

func isTopLevel(p string) bool {
	return len(p) > 1 && p[0] == '/' && !strings.Contains(p[1:], "/")
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }
func isDir(p string) bool  { st, err := os.Stat(p); return err == nil && st.IsDir() }
func isFile(p string) bool { st, err := os.Stat(p); return err == nil && st.Mode().IsRegular() }
