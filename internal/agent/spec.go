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
	EnvExclude  []string
}

// builtin returns the built-in defaults for a known agent.  OpenCode keeps
// its bespoke behavior in its Agent implementation; the spec only carries
// the lists other agents share.
func builtin(name string) (Spec, bool) {
	switch name {
	case OpenCode.Name():
		return Spec{
			Name:        OpenCode.Name(),
			Subcommands: slices.Clone(opencodeSubcommands),
			EnvExclude:  slices.Clone(opencodeExcluded),
		}, true
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
// private copies (persistent when isolated, temporary when shared); home is
// the host home for ~ expansion.  Missing host paths are skipped with a
// warning so a fresh machine does not fail the run.
func Materialize(spec Spec, storeRoot, home string, warn func(string)) []Bind {
	var binds []Bind
	bind := func(src, dest string, writable bool) {
		binds = append(binds, Bind{Src: src, Dest: dest, Writable: writable})
	}
	// absolute expands ~ and rejects relative paths: agent state is
	// identity-level, so it must name an exact host location.
	absolute := func(raw string) (string, bool) {
		if raw == "" {
			return "", false
		}
		exp := paths.Expand(raw, home)
		if !filepath.IsAbs(exp) {
			warn(fmt.Sprintf("agent path %s must be absolute or start with ~, skipping", raw))
			return "", false
		}
		return exp, true
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
		return resolved, true
	}
	for _, raw := range spec.Share {
		if resolved, ok := existing(raw); ok {
			warn(fmt.Sprintf("%s is mounted read-write into the sandbox", resolved))
			bind(resolved, resolved, true)
		}
	}
	for _, raw := range spec.ReadOnly {
		if resolved, ok := existing(raw); ok {
			bind(resolved, resolved, false)
		}
	}
	for _, raw := range spec.Copy {
		resolved, ok := existing(raw)
		if !ok {
			continue
		}
		store := filepath.Join(storeRoot, storeRel(resolved, home))
		if _, err := os.Lstat(store); err != nil {
			if err := copyTree(resolved, store); err != nil {
				warn(fmt.Sprintf("cannot copy %s into the profile: %v", resolved, err))
				continue
			}
		}
		bind(store, resolved, true)
	}
	for _, raw := range spec.Private {
		exp, ok := absolute(raw)
		if !ok {
			continue
		}
		// Private state may not exist on the host yet; normalize without
		// requiring it.
		resolved := paths.ResolveMaybe(exp, home)
		store := filepath.Join(storeRoot, storeRel(resolved, home))
		if err := os.MkdirAll(store, 0o755); err != nil {
			warn(fmt.Sprintf("cannot create the profile directory for %s: %v", resolved, err))
			continue
		}
		bind(store, resolved, true)
	}
	return binds
}

// storeRel maps a host path into the profile store: paths under home keep
// their relative form, others are flattened with an "abs" prefix.
func storeRel(resolved, home string) string {
	if rel, err := filepath.Rel(home, resolved); err == nil && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return "abs" + strings.ReplaceAll(resolved, "/", "_")
}
