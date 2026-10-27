package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dlawregiets/kandor/internal/agent"
	"github.com/dlawregiets/kandor/internal/config"
	"github.com/dlawregiets/kandor/internal/paths"
)

// indexSeq returns the position of seq in args, or -1.
func indexSeq(args []string, seq ...string) int {
	for i := 0; i+len(seq) <= len(args); i++ {
		if slices.Equal(args[i:i+len(seq)], seq) {
			return i
		}
	}
	return -1
}

func hasSeq(args []string, seq ...string) bool { return indexSeq(args, seq...) >= 0 }

type env struct {
	home, project, runtime string
	in                     Input
	warns                  []string
	vars                   map[string]string
}

// newEnv builds a fake HOME and project under t.TempDir().  The temporary
// directory is resolved so it matches what Resolve returns.
func newEnv(t *testing.T) *env {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	e := &env{
		home:    filepath.Join(root, "home"),
		runtime: filepath.Join(root, "run"),
		vars:    map[string]string{},
	}
	e.project = filepath.Join(e.home, "code", "proj")
	for _, d := range []string{e.project, e.runtime, filepath.Join(root, "bin")} {
		os.MkdirAll(d, 0o755)
	}
	e.in = Input{
		Settings: config.Settings{ProjectDir: e.project, Network: true, ProtectGit: false, MountGitConfig: true},
		Host:     paths.Host{Home: e.home, RuntimeDir: e.runtime, UID: os.Getuid()},
		Features: Features{Chmod: true},
		Binary:   filepath.Join(root, "bin", "opencode"),
		Getenv:   func(k string) string { return e.vars[k] },
		Warn:     func(s string) { e.warns = append(e.warns, s) },
	}
	return e
}

func (e *env) build(t *testing.T) ([]string, Result) {
	t.Helper()
	p := NewPlan(e.in.Features)
	r, err := BuildMounts(p, e.in)
	if err != nil {
		t.Fatalf("BuildMounts error: %v", err)
	}
	return p.Args(), r
}

func (e *env) buildErr(t *testing.T) error {
	t.Helper()
	_, err := BuildMounts(NewPlan(e.in.Features), e.in)
	if err == nil {
		t.Fatal("BuildMounts succeeded, want error")
	}
	return err
}

func TestParseFeatures(t *testing.T) {
	f := ParseFeatures("usage: bwrap [OPTIONS...]\n    --chmod OCTAL PATH\n    --disable-userns  Forbid...\n")
	if f != (Features{Chmod: true, DisableUserns: true}) {
		t.Errorf("ParseFeatures = %+v", f)
	}
	all := ParseFeatures("--unshare-user-try --disable-userns --chmod --unshare-cgroup-try --new-session --die-with-parent --cap-drop")
	if all != (Features{true, true, true, true, true, true, true}) {
		t.Errorf("ParseFeatures(all) = %+v", all)
	}
}

func TestPlanDedupesByDestination(t *testing.T) {
	p := NewPlan(Features{})
	p.RW("/a", "/d")
	p.RO("/b", "/d")
	p.Bind("/c", "/c", false)
	p.Bind("/c", "/c", true)
	want := []string{"--bind", "/a", "/d", "--ro-bind", "/c", "/c", "--bind", "/c", "/c"}
	if !slices.Equal(p.Args(), want) {
		t.Errorf("Args = %q, want %q", p.Args(), want)
	}
}

func TestPlanTmpfsChmod(t *testing.T) {
	dir := t.TempDir()
	p := NewPlan(Features{Chmod: true})
	p.Tmpfs(dir, "1777")
	p.Tmpfs(filepath.Join(dir, "missing"), "")
	p.PrivateDir("/x", "0700")
	want := []string{"--tmpfs", dir, "--chmod", "1777", dir, "--dir", "/x", "--tmpfs", "/x", "--chmod", "0700", "/x"}
	if !slices.Equal(p.Args(), want) {
		t.Errorf("Args = %q", p.Args())
	}
	p = NewPlan(Features{})
	p.PrivateDir("/x", "0700")
	if hasSeq(p.Args(), "--chmod") {
		t.Error("--chmod used without the feature")
	}
}

func TestMountsBaseLayout(t *testing.T) {
	e := newEnv(t)
	args, _ := e.build(t)
	for _, seq := range [][]string{
		{"--ro-bind", "/", "/"}, {"--dev", "/dev"}, {"--proc", "/proc"},
		{"--tmpfs", "/tmp", "--chmod", "1777", "/tmp"}, {"--dir", e.home},
		{"--dir", e.runtime, "--tmpfs", e.runtime, "--chmod", "0700", e.runtime},
		{"--bind", e.project, e.project},
	} {
		if !hasSeq(args, seq...) {
			t.Errorf("args missing %q:\n%q", seq, args)
		}
	}
	if indexSeq(args, "--tmpfs", "/tmp") > indexSeq(args, "--bind", e.project, e.project) {
		t.Error("home is hidden after the project is mounted")
	}
	if hasSeq(args, "--tmpfs", "/var/tmp") == false && dirExists("/var/tmp") {
		t.Error("/var/tmp not hidden")
	}
}

func dirExists(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

func TestMountsAgentBinds(t *testing.T) {
	e := newEnv(t)
	rw, ro := filepath.Join(e.home, "state"), filepath.Join(e.home, "bin-cache")
	os.MkdirAll(rw, 0o755)
	os.MkdirAll(ro, 0o755)
	e.in.AgentBinds = []agent.Bind{{Src: rw, Dest: rw, Writable: true}, {Src: ro, Dest: "/dest", FollowSymlinks: true}}
	args, _ := e.build(t)
	if !hasSeq(args, "--bind", rw, rw) || !hasSeq(args, "--ro-bind", ro, "/dest") {
		t.Errorf("agent binds missing: %q", args)
	}
}

func TestMountsSettingsOrder(t *testing.T) {
	e := newEnv(t)
	x := filepath.Join(e.home, "go")
	os.MkdirAll(x, 0o755)
	e.in.Settings.Mounts = []config.MountSpec{{Path: x}, {Path: x, Writable: true}}
	args, _ := e.build(t)
	ro, rw := indexSeq(args, "--ro-bind", x, x), indexSeq(args, "--bind", x, x)
	if ro < 0 || rw < 0 || ro > rw {
		t.Errorf("want --ro-bind then --bind of %s: %q", x, args)
	}
}

func TestMountsMissingPath(t *testing.T) {
	e := newEnv(t)
	missing := filepath.Join(e.home, "missing")
	e.in.Settings.Mounts = []config.MountSpec{{Path: missing}}
	if err := e.buildErr(t); err.Error() != "ro path does not exist: "+missing {
		t.Errorf("err = %q", err)
	}
	e.in.Settings.Mounts = []config.MountSpec{{Path: missing, Writable: true}}
	if err := e.buildErr(t); err.Error() != "rw path does not exist: "+missing {
		t.Errorf("err = %q", err)
	}
}

func TestMountsRefusesBroad(t *testing.T) {
	e := newEnv(t)
	e.in.Settings.Mounts = []config.MountSpec{{Path: e.home, Writable: true}}
	if err := e.buildErr(t); !strings.Contains(err.Error(), "refusing to mount") {
		t.Errorf("err = %q", err)
	}
	e.in.Settings.Mounts = []config.MountSpec{{Path: "/usr"}}
	if err := e.buildErr(t); !strings.Contains(err.Error(), "refusing to mount top-level directory '/usr'") {
		t.Errorf("err = %q", err)
	}
}

func TestMountsSymlinkToProjectParentSkipped(t *testing.T) {
	e := newEnv(t)
	bin := filepath.Join(e.home, ".local", "bin")
	os.MkdirAll(bin, 0o755)
	os.Symlink(filepath.Join(e.home, "code"), filepath.Join(bin, "code"))
	tool := filepath.Join(e.home, "tools", "t")
	os.MkdirAll(filepath.Dir(tool), 0o755)
	os.WriteFile(tool, nil, 0o755)
	os.Symlink(tool, filepath.Join(bin, "t"))
	args, _ := e.build(t)
	if hasSeq(args, "--ro-bind", filepath.Join(e.home, "code"), filepath.Join(e.home, "code")) {
		t.Error("symlink target containing the project was mounted")
	}
	if !hasSeq(args, "--ro-bind", tool, tool) {
		t.Error("ordinary symlink target not mounted")
	}
	if !slices.ContainsFunc(e.warns, func(w string) bool { return strings.Contains(w, "parent of the project") }) {
		t.Errorf("warnings = %q", e.warns)
	}
}

func TestMountsBinaryInHomeRefused(t *testing.T) {
	e := newEnv(t)
	e.in.Binary = filepath.Join(e.home, "opencode")
	if err := e.buildErr(t); !strings.Contains(err.Error(), "refusing to mount the binary's directory") {
		t.Errorf("err = %q", err)
	}
	e = newEnv(t)
	e.in.Binary = "/usr/bin/opencode"
	args, _ := e.build(t)
	if !hasSeq(args, "--ro-bind", "/usr/bin", "/usr/bin") {
		t.Errorf("binary directory not mounted: %q", args)
	}
	e = newEnv(t)
	e.in.Binary = "/bin/sh"
	if args, _ := e.build(t); hasSeq(args, "--ro-bind", "/bin", "/bin") {
		t.Error("top-level binary directory was mounted again")
	}
}

func TestMountsCredentialsOptIn(t *testing.T) {
	e := newEnv(t)
	ssh := filepath.Join(e.home, ".ssh")
	os.MkdirAll(ssh, 0o700)
	os.WriteFile(filepath.Join(e.home, ".netrc"), nil, 0o600)
	args, _ := e.build(t)
	if hasSeq(args, "--ro-bind", ssh, ssh) || slices.Contains(args, filepath.Join(e.home, ".netrc")) {
		t.Error("credentials mounted without opting in")
	}
	e.in.Settings.SSH, e.in.Settings.Netrc, e.in.Settings.Npmrc = true, true, true
	args, _ = e.build(t)
	if !hasSeq(args, "--ro-bind", ssh, ssh) || !hasSeq(args, "--ro-bind", e.home+"/.netrc", e.home+"/.netrc") {
		t.Errorf("opted-in credentials missing: %q", args)
	}
	if !slices.ContainsFunc(e.warns, func(w string) bool { return strings.Contains(w, "--npmrc was requested") }) {
		t.Errorf("no warning for a missing ~/.npmrc: %q", e.warns)
	}
}

func TestMountsCredentialEnvFiles(t *testing.T) {
	e := newEnv(t)
	cert := filepath.Join(e.home, "ca.pem")
	os.WriteFile(cert, nil, 0o644)
	e.vars["SSL_CERT_FILE"] = cert
	e.vars["AWS_CONFIG_FILE"] = filepath.Join(e.home, "nope")
	args, _ := e.build(t)
	if !hasSeq(args, "--ro-bind", cert, cert) {
		t.Errorf("SSL_CERT_FILE not mounted: %q", args)
	}
	if !slices.ContainsFunc(e.warns, func(w string) bool {
		return strings.HasPrefix(w, "AWS_CONFIG_FILE points to a path that does not exist")
	}) {
		t.Errorf("warnings = %q", e.warns)
	}
}

func TestMountsGitConfig(t *testing.T) {
	e := newEnv(t)
	gitconfig := filepath.Join(e.home, ".gitconfig")
	os.WriteFile(gitconfig, nil, 0o644)
	xdgGit := filepath.Join(e.home, ".config", "git")
	os.MkdirAll(xdgGit, 0o755)
	os.WriteFile(filepath.Join(xdgGit, "config"), nil, 0o644)
	e.in.XDGConfig = filepath.Join(e.home, ".config")
	args, r := e.build(t)
	if !hasSeq(args, "--ro-bind", gitconfig, gitconfig) || !hasSeq(args, "--ro-bind", xdgGit, xdgGit) {
		t.Errorf("git config not mounted: %q", args)
	}
	if r.GitConfigGlobal != filepath.Join(xdgGit, "config") {
		t.Errorf("GitConfigGlobal = %q", r.GitConfigGlobal)
	}
	e.in.Settings.MountGitConfig = false
	if args, _ := e.build(t); slices.Contains(args, gitconfig) {
		t.Error("--no-gitconfig still mounted ~/.gitconfig")
	}
}

func TestMountsSSHAgent(t *testing.T) {
	e := newEnv(t)
	e.in.Settings.SSHAgent = true
	e.vars["SSH_AUTH_SOCK"] = filepath.Join(e.home, "not-a-socket")
	_, r := e.build(t)
	if r.SSHAuthSock != "" || !slices.ContainsFunc(e.warns, func(w string) bool { return strings.Contains(w, "SSH_AUTH_SOCK is not a socket") }) {
		t.Errorf("SSHAuthSock = %q, warnings = %q", r.SSHAuthSock, e.warns)
	}
}
