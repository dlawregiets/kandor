package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dlawregiets/kandor/internal/config"
	"github.com/dlawregiets/kandor/internal/paths"
)

// Spec is the resolved per-agent behavior: identity plus host-state paths.
// Built-in defaults merge with the user's [agents.<name>] section in
// Resolve; Materialize turns the paths into sandbox binds.
type Spec struct {
	Name        string
	Binary      string // "" means Name
	Subcommands []string
	Share       []string
	Copy        []string
	Private     []string
	ReadOnly    []string
	// Cache names agent state kept in a kandor-owned persistent directory,
	// never in the host's real location: derived downloads and caches that
	// must survive runs but must not be written back to the host.
	Cache      []string
	EnvExclude []string
	// NodeCompileCache keeps Node's compile cache in a persistent
	// kandor-owned directory instead of the ephemeral $TMPDIR.
	NodeCompileCache bool
	// PersistentScratch keeps the sandbox's TMPDIR in a persistent
	// kandor-owned directory, so on-disk caches that live in TMPDIR (a
	// module loader's transpiled sources, for one) survive between runs.
	// Stale entries are pruned; see scratchKeep.
	PersistentScratch bool
}

// builtinSpecs are the agents kandor knows without configuration.
var builtinSpecs = []Spec{
	{
		Name:        OpenCode.Name(),
		Subcommands: slices.Clone(opencodeSubcommands),
		EnvExclude:  slices.Clone(opencodeExcluded),
	},
	{
		// Probed against Claude Code 2.1 (claude --help and its binary).
		Name: "claude",
		Subcommands: []string{
			"agents", "attach", "auth", "doctor", "install", "logs",
			"mcp", "plugin", "plugins", "rm", "setup-token",
			"stop", "kill", "update", "upgrade",
		},
		// Settings and login live together; copy both into the profile
		// so the sandbox never reads or writes the host's.
		Copy: []string{"~/.claude", "~/.claude.json"},
		// CLAUDECODE=1 marks child processes; the entrypoint names the
		// parent session.  Neither may flow back in.
		EnvExclude: []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT"},
	},
	{
		// Probed against pi 1.4.0 (pi --help).  Settings and login live
		// under ~/.pi, so that directory is copied into the profile.  The
		// derived trees are not: installed extensions (npm) and pi's own
		// releases (install) are large and are shared with the host, and
		// sessions and downloaded helper binaries (fd) are shared and
		// cached, so a run neither copies them nor downloads them again.
		Name: "pi",
		Subcommands: []string{
			"install", "remove", "uninstall", "update", "list",
			"config", "auth", "mcp",
		},
		Copy: []string{"~/.pi"},
		Share: []string{
			"~/.pi/agent/sessions",
			"~/.pi/agent/npm",
			"~/.pi/agent/install",
		},
		Cache:             []string{"~/.pi/agent/bin"},
		EnvExclude:        []string{"PI_"},
		NodeCompileCache:  true,
		PersistentScratch: true,
	},
	{
		// Probed against Copilot CLI 1.0.101 (copilot --help).  The user
		// settings file is ~/.copilot/settings.json (COPILOT_HOME
		// relocates it, which cannot be expressed here).
		Name: "copilot",
		Subcommands: []string{
			"app", "login", "help", "init", "config", "update", "version",
			"workflow", "sessions", "memories", "plugin", "mcp", "skill",
			"instruction", "lsp", "sandbox", "completion",
		},
		Copy:       []string{"~/.copilot"},
		EnvExclude: []string{"COPILOT_"},
	},
}

// builtin returns the built-in defaults for a known agent.  OpenCode keeps
// its bespoke behavior in its Agent implementation; the spec only carries
// the lists other agents share.
func builtin(name string) (Spec, bool) {
	for _, s := range builtinSpecs {
		if s.Name == name {
			return s, true
		}
	}
	return Spec{}, false
}

// Resolve merges built-in defaults with the user's [agents.<name>] section:
// the configured binary wins, lists concatenate without duplicates.
func Resolve(name string, cfg config.AgentSection) Spec {
	s, _ := builtin(name)
	s.Name = name
	if s.Binary == "" {
		s.Binary = name
	}
	if cfg.Binary != nil {
		s.Binary = *cfg.Binary
	}
	s.Subcommands = appendUnique(s.Subcommands, cfg.Subcommands)
	s.Share = append(s.Share, cfg.Paths.Share...)
	s.Copy = append(s.Copy, cfg.Paths.Copy...)
	s.Private = append(s.Private, cfg.Paths.Private...)
	s.Cache = append(s.Cache, cfg.Paths.Cache...)
	s.ReadOnly = append(s.ReadOnly, cfg.Paths.RO...)
	s.EnvExclude = appendUnique(s.EnvExclude, cfg.Env.Exclude)
	return s
}

func appendUnique(dst, src []string) []string {
	for _, s := range src {
		if !slices.Contains(dst, s) {
			dst = append(dst, s)
		}
	}
	return dst
}

// Materialize turns spec paths into binds.  storeRoot holds the profile's
// private copies (persistent when isolated, temporary when shared); host is
// the host paths for ~ expansion and broad-path checks.  Missing host paths
// are skipped with a warning so a fresh machine does not fail the run,
// except share paths, which are created (share names state directories, not
// files).  With dryRun, the binds are planned but nothing is copied or
// created.  With isolated, share paths are seeded into the profile instead
// of exposed read-write.
func Materialize(spec Spec, storeRoot, cacheRoot string, host paths.Host, dryRun, isolated bool, warn func(string)) []Bind {
	var binds []Bind
	home := host.Home
	bind := func(src, dest string, writable bool) {
		binds = append(binds, Bind{Src: src, Dest: dest, Writable: writable})
	}
	// absolute expands ~ and rejects relative paths: agent state is
	// identity-level, so it must name an exact host location.
	absolute := func(raw string) (string, bool) {
		if raw == "" {
			return "", false
		}
		exp := filepath.Clean(paths.Expand(raw, home))
		if !filepath.IsAbs(exp) {
			warn(fmt.Sprintf("agent path %s must be absolute or start with ~, skipping", raw))
			return "", false
		}
		if host.IsBroad(exp) {
			warn(fmt.Sprintf("agent path %s would expose too much of the host, skipping", raw))
			return "", false
		}
		return exp, true
	}
	// resolvedOK rejects a path whose symlinks lead to a broad host path
	// (for example ~/link-to-root).
	resolvedOK := func(raw, resolved string) bool {
		if host.IsBroad(resolved) {
			warn(fmt.Sprintf("agent path %s resolves to %s, which would expose too much of the host; skipping", raw, resolved))
			return false
		}
		return true
	}
	// existing resolves an absolute path that must already exist.
	existing := func(raw string) (string, bool) {
		exp, ok := absolute(raw)
		if !ok {
			return "", false
		}
		resolved, err := paths.Resolve(exp)
		if err != nil {
			warn(fmt.Sprintf("agent path %s does not exist, skipping", raw))
			return "", false
		}
		if !resolvedOK(raw, resolved) {
			return "", false
		}
		return resolved, true
	}
	// Paths materialized separately must not be copied into a parent copy
	// first: sessions can be large, and tool downloads are recreated on
	// demand, so copying them would only be discarded by the child mount.
	overridden := map[string]bool{}
	for _, list := range [][]string{spec.Share, spec.Cache, spec.Private, spec.ReadOnly} {
		for _, raw := range list {
			if exp, ok := absolute(raw); ok {
				overridden[paths.ResolveMaybe(exp, home)] = true
			}
		}
	}
	copyInto := func(resolved string) {
		store := storeDir(storeRoot, resolved, home, dryRun)
		if _, err := os.Lstat(store); err != nil && !dryRun {
			if err := copyTreeSkip(resolved, store, overridden); err != nil {
				warn(fmt.Sprintf("cannot copy %s into the profile: %v", resolved, err))
				return
			}
		}
		bind(store, resolved, true)
	}
	for _, raw := range spec.Share {
		exp, ok := absolute(raw)
		if !ok {
			continue
		}
		// Share names a state directory.  Create it when the agent has not
		// yet (pi downloads helper binaries and writes sessions there), so
		// the mount is not silently skipped on the first run.
		if _, err := os.Lstat(exp); err != nil {
			if dryRun {
				warn(fmt.Sprintf("dry run: %s does not exist yet; a real run creates it", exp))
			} else if err := os.MkdirAll(exp, 0o700); err != nil {
				warn(fmt.Sprintf("cannot create %s: %v", exp, err))
				continue
			}
		}
		resolved, err := paths.Resolve(exp)
		if err != nil {
			// A dry run may not create the directory it plans to bind.
			resolved = paths.ResolveMaybe(exp, home)
		}
		if !resolvedOK(raw, resolved) {
			continue
		}
		if isolated {
			copyInto(resolved)
			continue
		}
		warn(fmt.Sprintf("%s is mounted read-write into the sandbox", resolved))
		bind(resolved, resolved, true)
	}
	for _, raw := range spec.ReadOnly {
		if resolved, ok := existing(raw); ok {
			bind(resolved, resolved, false)
		}
	}
	for _, raw := range spec.Copy {
		if resolved, ok := existing(raw); ok {
			copyInto(resolved)
		}
	}
	for _, raw := range spec.Private {
		exp, ok := absolute(raw)
		if !ok {
			continue
		}
		// Private state may not exist on the host yet; normalize without
		// requiring it.
		resolved := paths.ResolveMaybe(exp, home)
		if !resolvedOK(raw, resolved) {
			continue
		}
		store := storeDir(storeRoot, resolved, home, dryRun)
		if !dryRun {
			if err := os.MkdirAll(store, 0o755); err != nil {
				warn(fmt.Sprintf("cannot create the profile directory for %s: %v", resolved, err))
				continue
			}
		}
		bind(store, resolved, true)
	}
	for _, raw := range spec.Cache {
		exp, ok := absolute(raw)
		if !ok {
			continue
		}
		resolved := paths.ResolveMaybe(exp, home)
		if !resolvedOK(raw, resolved) {
			continue
		}
		dir := storeDir(cacheRoot, resolved, home, dryRun)
		if !dryRun {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				warn(fmt.Sprintf("cannot create the cache for %s: %v", resolved, err))
				continue
			}
		}
		bind(dir, resolved, true)
	}
	// Mount parents before children, whatever mode produced them: a later
	// child bind must land inside an earlier parent bind, not be hidden by
	// it (for example ~/.pi copied wholesale, with sessions shared beneath).
	slices.SortStableFunc(binds, func(a, b Bind) int {
		return pathDepth(a.Dest) - pathDepth(b.Dest)
	})
	return binds
}

// pathDepth counts the path components of p, so parents sort before children.
func pathDepth(p string) int {
	return strings.Count(filepath.Clean(p), string(filepath.Separator))
}

// storeRel maps a host path into the profile store: paths under home keep
// their relative form, others are flattened with an "abs" prefix.
// The flattening percent-encodes "%" and "/" so it is injective:
// "/etc/foo" and "/etc_foo" must not collide in the store.
func storeRel(resolved, home string) string {
	if rel, err := filepath.Rel(home, resolved); err == nil && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return "abs" + strings.NewReplacer("%", "%25", "/", "%2F").Replace(resolved)
}

// storeDir is the store path for resolved under root, moving a store left
// behind by the pre-percent-encoding name ("abs_etc_foo") to the current
// one ("abs%2Fetc%2Ffoo") on first use, so existing state is kept instead
// of being re-copied from the host.  A dry run plans the path but moves
// nothing.
func storeDir(root, resolved, home string, dryRun bool) string {
	store := filepath.Join(root, storeRel(resolved, home))
	if dryRun {
		return store
	}
	if _, err := os.Lstat(store); err == nil {
		return store
	}
	if legacy := legacyStoreRel(resolved, home); legacy != "" {
		legacyPath := filepath.Join(root, legacy)
		if _, err := os.Lstat(legacyPath); err == nil {
			_ = os.Rename(legacyPath, store)
		}
	}
	return store
}

// legacyStoreRel is the store name used before storeRel percent-encoded
// its flattened form, or "" when the name is unchanged.
func legacyStoreRel(resolved, home string) string {
	if rel, err := filepath.Rel(home, resolved); err == nil && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return "abs" + strings.ReplaceAll(resolved, "/", "_")
}
