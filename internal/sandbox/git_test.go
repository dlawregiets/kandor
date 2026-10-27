package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid",
		"-c", "protocol.file.allow=always"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %q: %v\n%s", args, err, out)
	}
}

// discover runs DiscoverGit and Protect the way BuildMounts does.
func (e *env) discover(t *testing.T, protect bool) ([]string, GitState) {
	t.Helper()
	p := NewPlan(e.in.Features)
	g := DiscoverGit(e.project, e.in.Host, p, e.in.Warn)
	if protect && g.Trusted {
		g.Protect(p, e.project, e.in.Warn)
	}
	return p.Args(), g
}

func (e *env) warned(substr string) bool {
	return slices.ContainsFunc(e.warns, func(w string) bool { return strings.Contains(w, substr) })
}

func TestGitPlainRepoProtected(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	git(t, e.project, "init", "-q")
	dotgit := filepath.Join(e.project, ".git")
	os.RemoveAll(filepath.Join(dotgit, "hooks"))
	args, g := e.discover(t, true)
	if !g.Trusted || g.Common != dotgit || g.Dir != dotgit {
		t.Fatalf("GitState = %+v", g)
	}
	for _, seq := range [][]string{
		{"--bind", dotgit, dotgit},
		{"--ro-bind", dotgit + "/hooks", dotgit + "/hooks"},
		{"--ro-bind", dotgit + "/config", dotgit + "/config"},
	} {
		if !hasSeq(args, seq...) {
			t.Errorf("args missing %q: %q", seq, args)
		}
	}
	if indexSeq(args, "--bind", dotgit, dotgit) > indexSeq(args, "--ro-bind", dotgit+"/hooks", dotgit+"/hooks") {
		t.Error(".git is bound after its hooks, which would hide the read-only mount")
	}
	if !isDir(filepath.Join(dotgit, "hooks")) {
		t.Error("missing hooks directory was not created")
	}
	if g.AuditCommondir != filepath.Join(dotgit, "commondir") {
		t.Errorf("AuditCommondir = %q", g.AuditCommondir)
	}
}

func TestGitNotARepo(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	args, g := e.discover(t, true)
	if g.Trusted || slices.Contains(args, "--bind") {
		t.Errorf("non-repository: %+v %q", g, args)
	}
}

func TestGitDotfilesRepoRefused(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	git(t, e.home, "init", "-q")
	os.WriteFile(filepath.Join(e.project, ".git"), []byte("gitdir: "+e.home+"/.git\n"), 0o644)
	args, g := e.discover(t, true)
	if g.Trusted || slices.Contains(args, e.home+"/.git") {
		t.Errorf("dotfiles repository exposed: %+v %q", g, args)
	}
	if !e.warned("refusing to mount an untrusted git directory") {
		t.Errorf("warnings = %q", e.warns)
	}
}

func TestGitSubdirOfHomeRepoRefused(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	git(t, e.home, "init", "-q")
	args, g := e.discover(t, true)
	if g.Trusted || slices.Contains(args, e.home+"/.git") {
		t.Errorf("HOME repository exposed: %+v %q", g, args)
	}
}

func TestGitSubdirOfProjectRepoAccepted(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	repo := filepath.Join(e.home, "code")
	git(t, repo, "init", "-q")
	args, g := e.discover(t, true)
	if !g.Trusted || !hasSeq(args, "--bind", repo+"/.git", repo+"/.git") {
		t.Errorf("enclosing repository not mounted: %+v %q", g, args)
	}
}

func submodule(t *testing.T, e *env) (super, sub, modDir string) {
	super = filepath.Join(e.home, "code", "super")
	src := filepath.Join(e.home, "src")
	os.MkdirAll(super, 0o755)
	os.MkdirAll(src, 0o755)
	git(t, src, "init", "-q")
	git(t, src, "commit", "-q", "--allow-empty", "-m", "init")
	git(t, super, "init", "-q")
	git(t, super, "submodule", "add", "-q", src, "sub")
	return super, filepath.Join(super, "sub"), filepath.Join(super, ".git", "modules", "sub")
}

func TestGitSubmoduleAccepted(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	_, sub, modDir := submodule(t, e)
	e.project, e.in.Settings.ProjectDir = sub, sub
	args, g := e.discover(t, true)
	if !g.Trusted || !hasSeq(args, "--bind", modDir, modDir) {
		t.Errorf("submodule git directory not mounted: %+v %q", g, args)
	}
	if !hasSeq(args, "--ro-bind", sub+"/.git", sub+"/.git") {
		t.Errorf("submodule .git file not read-only: %q", args)
	}
}

func TestGitSubmoduleWrongWorktree(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	_, sub, modDir := submodule(t, e)
	git(t, modDir, "config", "core.worktree", "../../../elsewhere")
	e.project, e.in.Settings.ProjectDir = sub, sub
	args, g := e.discover(t, true)
	if g.Trusted || slices.Contains(args, modDir) {
		t.Errorf("submodule with a foreign worktree accepted: %+v %q", g, args)
	}
}

func TestGitWorktreeAccepted(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	main := filepath.Join(e.home, "main")
	os.MkdirAll(main, 0o755)
	git(t, main, "init", "-q")
	git(t, main, "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(e.home, "code", "wt")
	git(t, main, "worktree", "add", "-q", wt, "-b", "b")
	e.project, e.in.Settings.ProjectDir = wt, wt
	args, g := e.discover(t, true)
	common, perWT := main+"/.git", main+"/.git/worktrees/wt"
	if !g.Trusted || g.Common != common || g.Dir != perWT {
		t.Fatalf("GitState = %+v", g)
	}
	for _, seq := range [][]string{
		{"--bind", common, common},
		{"--ro-bind", wt + "/.git", wt + "/.git"},
		{"--bind", perWT, perWT},
		{"--ro-bind", perWT + "/commondir", perWT + "/commondir"},
		{"--ro-bind", common + "/config", common + "/config"},
	} {
		if !hasSeq(args, seq...) {
			t.Errorf("args missing %q: %q", seq, args)
		}
	}
	if g.AuditCommondir != "" {
		t.Errorf("AuditCommondir = %q for a worktree", g.AuditCommondir)
	}
}

func TestGitForgedWorktreeRefused(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	other := filepath.Join(e.home, "other")
	os.MkdirAll(other, 0o755)
	git(t, other, "init", "-q")
	os.MkdirAll(filepath.Join(other, ".git", "worktrees", "x"), 0o755)
	os.WriteFile(filepath.Join(e.project, ".git"), []byte("gitdir: "+other+"/.git/worktrees/x\n"), 0o644)
	os.WriteFile(filepath.Join(other, ".git", "worktrees", "x", "commondir"), []byte("../..\n"), 0o644)
	os.WriteFile(filepath.Join(other, ".git", "worktrees", "x", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	os.WriteFile(filepath.Join(other, ".git", "worktrees", "x", "gitdir"), []byte("/somewhere/else/.git\n"), 0o644)
	args, g := e.discover(t, true)
	if g.Trusted || slices.Contains(args, other+"/.git") {
		t.Errorf("forged worktree accepted: %+v %q", g, args)
	}
}

func TestGitNoProtect(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	git(t, e.project, "init", "-q")
	args, g := e.discover(t, false)
	if !g.Trusted || slices.Contains(args, e.project+"/.git/config") {
		t.Errorf("unprotected repository: %+v %q", g, args)
	}
}

func TestBuildMountsProtectsGit(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	git(t, e.project, "init", "-q")
	e.in.Settings.ProtectGit = true
	args, r := e.build(t)
	if !hasSeq(args, "--ro-bind", e.project+"/.git/config", e.project+"/.git/config") || r.Git.AuditNewRepo {
		t.Errorf("BuildMounts: %+v %q", r.Git, args)
	}
	e.in.Settings.ProtectGit = false
	if args, _ := e.build(t); slices.Contains(args, e.project+"/.git/config") {
		t.Error("ProtectGit false still protected")
	}
}

func TestAuditQuarantinesCommondir(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	git(t, e.project, "init", "-q")
	_, g := e.discover(t, true)
	os.WriteFile(g.AuditCommondir, []byte("../evil\n"), 0o644)
	Audit(g, e.project, e.in.Warn)
	if exists(g.AuditCommondir) {
		t.Error("commondir not moved aside")
	}
	matches, _ := filepath.Glob(g.AuditCommondir + ".kandor-quarantine.*")
	if len(matches) != 1 || !e.warned("redirects git to another") {
		t.Errorf("quarantine = %q, warnings = %q", matches, e.warns)
	}
}

func TestAuditNewRepoWarns(t *testing.T) {
	e := newEnv(t)
	Audit(GitState{AuditNewRepo: true}, e.project, e.in.Warn)
	if len(e.warns) != 0 {
		t.Errorf("warned without a new repository: %q", e.warns)
	}
	os.Mkdir(filepath.Join(e.project, ".git"), 0o755)
	Audit(GitState{AuditNewRepo: true}, e.project, e.in.Warn)
	if !e.warned("the sandbox created a git repository") {
		t.Errorf("warnings = %q", e.warns)
	}
}
