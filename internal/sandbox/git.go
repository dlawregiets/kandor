package sandbox

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dlawregiets/kandor/internal/paths"
)

// GitState records what was found and protected in the project's repository.
type GitState struct {
	// Dir is the project's git directory; Common is the shared one (they
	// differ for worktrees).
	Dir, Common string
	// Trusted means the repository is inside the project or was validated.
	Trusted bool
	// AuditCommondir is a commondir path that did not exist before the run;
	// git would follow it if the sandbox created it.
	AuditCommondir string
	// AuditNewRepo asks Audit to warn if the sandbox creates a repository.
	AuditNewRepo bool
}

// DiscoverGit finds the project's repository.  A common git directory outside
// the project is mounted read-write only when it is validated: an enclosing
// repository below HOME, a genuine submodule, or a worktree whose
// back-reference matches.  An untrusted project must not be able to point its
// .git file at an arbitrary host directory.
func DiscoverGit(project string, host paths.Host, p *Plan, warn func(string)) GitState {
	var g GitState
	if _, err := exec.LookPath("git"); err != nil {
		return g
	}
	if _, err := gitProbe(project, "rev-parse", "--is-inside-work-tree"); err != nil {
		return g
	}
	common, err := gitProbe(project, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || common == "" {
		// Older Git versions do not support --path-format.
		common, _ = gitProbe(project, "rev-parse", "--git-common-dir")
		if common != "" && !filepath.IsAbs(common) {
			common = filepath.Join(project, common)
		}
	}
	if common == "" {
		return g
	}
	resolved, err := paths.Resolve(common)
	if err != nil {
		return g
	}
	g.Common = resolved
	switch {
	case paths.Contains(project, resolved):
		g.Trusted = true
	case commonAllowed(project, resolved, host):
		p.RW(resolved, resolved)
		g.Trusted = true
	default:
		warn("refusing to mount an untrusted git directory outside the project: " + resolved)
		warn(fmt.Sprintf("git operations may fail; use --rw '%s' if you trust this repository", resolved))
		return g
	}
	g.Dir = g.Common
	if dir, err := gitProbe(project, "rev-parse", "--absolute-git-dir"); err == nil && dir != "" {
		if resolved, err := paths.Resolve(dir); err == nil {
			g.Dir = resolved
		}
	}
	return g
}

func gitProbe(project string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", project}, args...)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_DIR=") && !strings.HasPrefix(kv, "GIT_WORK_TREE=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func commonAllowed(project, common string, host paths.Host) bool {
	// Without a .git of its own, the project is a subdirectory of an
	// enclosing work tree.  Git found that repository outside the project,
	// so the project cannot have planted it, but never expose a repository
	// rooted at HOME or above: a dotfiles repository would put every tracked
	// file in reach.
	if _, err := os.Lstat(filepath.Join(project, ".git")); err != nil {
		top, ok := strings.CutSuffix(common, "/.git")
		return ok && strings.HasPrefix(project, top+"/") && !host.IsBroad(top)
	}

	// A submodule's git directory lives in <superproject>/.git/modules/, and
	// its core.worktree points back at the submodule.  The project cannot
	// write that file, so it proves the .git file is genuine.  Reading it
	// with --file runs nothing from the repository.
	if top, _, ok := strings.Cut(common, "/.git/modules/"); ok {
		if !strings.HasPrefix(project, top+"/") || host.IsBroad(top) {
			return false
		}
		out, err := exec.Command("git", "config", "--file", filepath.Join(common, "config"), "core.worktree").Output()
		if err != nil {
			return false
		}
		worktree := strings.TrimSpace(string(out))
		if !filepath.IsAbs(worktree) {
			worktree = filepath.Join(common, worktree)
		}
		resolved, err := paths.Resolve(worktree)
		return err == nil && resolved == project
	}

	return validWorktree(project, common)
}

// validWorktree checks a worktree's .git file and the back-reference in the
// per-worktree directory it names.
func validWorktree(project, common string) bool {
	gitFile := filepath.Join(project, ".git")
	line, ok := firstLine(gitFile)
	if !ok {
		return false
	}
	perWT, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return false
	}
	perWT = strings.TrimSpace(perWT)
	if !filepath.IsAbs(perWT) {
		perWT = filepath.Join(project, perWT)
	}
	perWT, err := paths.Resolve(perWT)
	if err != nil || !strings.HasPrefix(perWT, common+"/") {
		return false
	}
	back, ok := firstLine(filepath.Join(perWT, "gitdir"))
	if !ok {
		return false
	}
	if !filepath.IsAbs(back) {
		back = filepath.Join(project, back)
	}
	back, err = paths.Resolve(back)
	if err != nil {
		return false
	}
	self, err := paths.Resolve(gitFile)
	return err == nil && back == self
}

func firstLine(path string) (string, bool) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return "", false
	}
	return strings.TrimSuffix(sc.Text(), "\r"), true
}

// Protect keeps the repository's hooks and config read-only, because git runs
// hooks and commands named in its config (core.fsmonitor, filters, ...) when
// the user later runs git on the host.  Every path git follows to find its
// directories becomes a mount point or a read-only file, because a mount
// point cannot be renamed: otherwise the sandbox could move .git aside, or
// rewrite a .git file or commondir, and swap in a repository of its own.
func (g *GitState) Protect(p *Plan, project string, warn func(string)) {
	dotgit := filepath.Join(project, ".git")
	if isFile(dotgit) {
		p.RO(dotgit, dotgit)
	} else if isDir(dotgit) {
		p.RW(dotgit, dotgit)
	}
	p.RW(g.Common, g.Common)
	if g.Dir != g.Common {
		p.RW(g.Dir, g.Dir)
	}

	hooks := filepath.Join(g.Common, "hooks")
	if !isDir(hooks) {
		if err := os.MkdirAll(hooks, 0o755); err != nil {
			warn(fmt.Sprintf("cannot create %s; git hooks are not protected", hooks))
		}
	}
	if isDir(hooks) {
		p.RO(hooks, hooks)
	}
	if config := filepath.Join(g.Common, "config"); isFile(config) {
		p.RO(config, config)
	}
	if commondir := filepath.Join(g.Dir, "commondir"); g.Dir != g.Common && isFile(commondir) {
		p.RO(commondir, commondir)
	}

	// Git also honours a commondir file in a main git directory.  A missing
	// file cannot be mounted read-only, so Audit checks for it afterwards.
	if commondir := filepath.Join(g.Dir, "commondir"); g.Dir == g.Common && !exists(commondir) {
		g.AuditCommondir = commondir
	}
}

// Audit runs after the sandbox exits.  It moves aside a commondir the sandbox
// created and warns about a repository the sandbox created, since either
// could make host git run commands of the sandbox's choosing.
func Audit(g GitState, project string, warn func(string)) {
	if g.AuditCommondir != "" && exists(g.AuditCommondir) {
		quarantine := fmt.Sprintf("%s.kandor-quarantine.%d", g.AuditCommondir, os.Getpid())
		warn(fmt.Sprintf("the sandbox created %s, which redirects git to another", g.AuditCommondir))
		if os.Rename(g.AuditCommondir, quarantine) == nil {
			warn("repository's config and hooks; it was moved to " + quarantine)
		} else {
			warn("repository's config and hooks; remove it before running git there")
		}
	}
	if g.AuditNewRepo {
		if _, err := os.Lstat(filepath.Join(project, ".git")); err == nil {
			warn(fmt.Sprintf("the sandbox created a git repository in %s; check .git/config", project))
			warn("and .git/hooks before running git there on the host")
		}
	}
}
