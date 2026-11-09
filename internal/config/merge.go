package config

import (
	"fmt"
	"regexp"
	"slices"
	"sort"

	"github.com/dlawregiets/kandor/internal/paths"
)

var profileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ResolveProjectDir applies the -C/positional project directory override to
// the current directory.
func ResolveProjectDir(o Overrides, cwd string) string {
	if o.ProjectDir != "" {
		return o.ProjectDir
	}
	return cwd
}

// Merge applies built-in defaults, then the user file's top-level keys, its
// "default" section, its included sections, the project file's included
// sections and own keys, then the agent sections, then the command line.
// Later layers win: a later mount of the same path and a later variable of
// the same name override earlier ones.
func Merge(user, project *File, o Overrides, agent, home, cwd string, getenv func(string) string) (Settings, error) {
	if user == nil {
		user = &File{}
	}
	if project == nil {
		project = &File{}
	}
	if len(project.Sections) > 0 {
		return Settings{}, fmt.Errorf("sections must be defined in the user configuration file, not the project one")
	}
	if project.DefaultAgent != "" {
		return Settings{}, fmt.Errorf("default_agent may only be set in the user configuration file, not the project one")
	}
	layers, err := resolveLayers(user, project, agent)
	if err != nil {
		return Settings{}, err
	}
	if err := checkAgentPaths(layers, home); err != nil {
		return Settings{}, err
	}

	s := Settings{
		ProjectDir:     ResolveProjectDir(o, cwd),
		Network:        true,
		MountGitConfig: true,
		ProtectGit:     true,
		Seed:           true,
		Profile:        "default",
	}

	for _, l := range layers {
		set(&s.Network, l.Sandbox.Network)
		set(&s.AllowUserns, l.Sandbox.AllowUserns)
		set(&s.NewSession, l.Sandbox.NewSession)
		set(&s.SSH, l.Credentials.SSH)
		set(&s.SSHAgent, l.Credentials.SSHAgent)
		set(&s.GitCredentials, l.Credentials.GitCredentials)
		set(&s.Netrc, l.Credentials.Netrc)
		set(&s.Npmrc, l.Credentials.Npmrc)
		set(&s.MountGitConfig, l.Git.MountConfig)
		set(&s.ProtectGit, l.Git.Protect)
		set(&s.Seed, l.Profile.Seed)

		// A profile name implies an isolated profile, like --profile,
		// unless the same layer also says explicitly whether to isolate.
		if l.Profile.Name != nil {
			s.Profile = *l.Profile.Name
			s.Isolated = true
		}
		set(&s.Isolated, l.Profile.Isolated)

		mounts, err := expandLayer(lists(l.Mounts), home)
		if err != nil {
			return Settings{}, err
		}
		s.Mounts = append(s.Mounts, mounts...)
		s.Env = append(s.Env, envSpecs(l.Env.Pass, l.Env.Set)...)
	}

	for _, l := range layers {
		a := l.Agents[agent]
		mounts, err := expandLayer(lists(a.Mounts), home)
		if err != nil {
			return Settings{}, err
		}
		s.Mounts = append(s.Mounts, mounts...)
		s.Env = append(s.Env, envSpecs(a.Env.Pass, a.Env.Set)...)
		// An empty binary selects nothing; only a real path overrides.
		if a.Binary != nil && *a.Binary != "" {
			s.AgentBinary = *a.Binary
			s.Agent.Binary = a.Binary
		}
		s.Agent.Subcommands = appendUnique(s.Agent.Subcommands, a.Subcommands)
		s.Agent.Paths.Share = append(s.Agent.Paths.Share, a.Paths.Share...)
		s.Agent.Paths.Copy = append(s.Agent.Paths.Copy, a.Paths.Copy...)
		s.Agent.Paths.Private = append(s.Agent.Paths.Private, a.Paths.Private...)
		s.Agent.Paths.RO = append(s.Agent.Paths.RO, a.Paths.RO...)
		s.Agent.Paths.Cache = append(s.Agent.Paths.Cache, a.Paths.Cache...)
		s.Agent.Env.Exclude = appendUnique(s.Agent.Env.Exclude, a.Env.Exclude)
	}

	set(&s.Network, o.Network)
	set(&s.AllowUserns, o.AllowUserns)
	set(&s.NewSession, o.NewSession)
	set(&s.SSH, o.SSH)
	set(&s.SSHAgent, o.SSHAgent)
	set(&s.GitCredentials, o.GitCredentials)
	set(&s.Netrc, o.Netrc)
	set(&s.Npmrc, o.Npmrc)
	set(&s.MountGitConfig, o.MountGitConfig)
	set(&s.ProtectGit, o.ProtectGit)
	set(&s.Seed, o.Seed)
	set(&s.Isolated, o.Isolated)
	if o.Profile != "" {
		s.Profile = o.Profile
	}
	if !profileName.MatchString(s.Profile) {
		return Settings{}, fmt.Errorf("invalid profile name '%s' (use letters, digits, '.', '_' and '-')", s.Profile)
	}

	cliMounts, err := expandLayer(o.Mounts, home)
	if err != nil {
		return Settings{}, err
	}
	s.Mounts = append(s.Mounts, cliMounts...)
	s.Env = append(s.Env, o.Env...)

	if v := getenv("KANDOR_BIN"); v != "" {
		s.AgentBinary = v
	}
	return s, nil
}

// resolveLayers orders the file layers: the user file, its "default"
// section, its included sections, the project file's included sections and
// the project file itself.  Each named section applies at most once.  A
// section's own Paths apply to the agent being run, folded into its agent
// entry for that layer.
func resolveLayers(user, project *File, agent string) ([]*File, error) {
	layers := []*File{user}
	applied := map[string]bool{}
	apply := func(name, where string) error {
		if applied[name] {
			return nil
		}
		sec, ok := user.Sections[name]
		if !ok {
			return fmt.Errorf("unknown section %q in the %s include list", name, where)
		}
		applied[name] = true
		layer := sec.layer()
		if p := sec.Paths; len(p.Share)+len(p.Copy)+len(p.Private)+len(p.RO)+len(p.Cache) > 0 {
			if layer.Agents == nil {
				layer.Agents = map[string]AgentSection{}
			}
			a := layer.Agents[agent]
			a.Paths.Share = append(a.Paths.Share, p.Share...)
			a.Paths.Copy = append(a.Paths.Copy, p.Copy...)
			a.Paths.Private = append(a.Paths.Private, p.Private...)
			a.Paths.RO = append(a.Paths.RO, p.RO...)
			a.Paths.Cache = append(a.Paths.Cache, p.Cache...)
			layer.Agents[agent] = a
		}
		layers = append(layers, layer)
		return nil
	}
	if _, ok := user.Sections["default"]; ok {
		if err := apply("default", "user file's"); err != nil {
			return nil, err
		}
	}
	for _, name := range user.Include {
		if err := apply(name, "user file's"); err != nil {
			return nil, err
		}
	}
	for _, name := range project.Include {
		if err := apply(name, "project file's"); err != nil {
			return nil, err
		}
	}
	return append(layers, project), nil
}

// set applies the file value, then the command-line value, when present.
func set[T any](dst *T, values ...*T) {
	for _, v := range values {
		if v != nil {
			*dst = *v
		}
	}
}

// appendUnique appends src elements missing from dst, keeping layer order.
func appendUnique(dst, src []string) []string {
	for _, s := range src {
		if !slices.Contains(dst, s) {
			dst = append(dst, s)
		}
	}
	return dst
}

func lists(m MountLists) []MountSpec {
	var out []MountSpec
	for _, p := range m.ReadOnly {
		out = append(out, MountSpec{Path: p})
	}
	for _, p := range m.ReadWrite {
		out = append(out, MountSpec{Path: p, Writable: true})
	}
	return out
}

// expandLayer expands paths and rejects a path that one layer asks for both
// read-only and read-write.
func expandLayer(layer []MountSpec, home string) ([]MountSpec, error) {
	seen := map[string]bool{}
	out := make([]MountSpec, 0, len(layer))
	for _, m := range layer {
		m.Path = paths.Expand(m.Path, home)
		if w, ok := seen[m.Path]; ok && w != m.Writable {
			return nil, fmt.Errorf("mounts: %q is listed as both read_only and read_write", m.Path)
		}
		seen[m.Path] = m.Writable
		out = append(out, m)
	}
	return out, nil
}

func envSpecs(pass []string, set map[string]string) []EnvSpec {
	out := make([]EnvSpec, 0, len(pass)+len(set))
	for _, name := range pass {
		out = append(out, EnvSpec{Name: name})
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, EnvSpec{Name: name, Value: set[name], HasValue: true})
	}
	return out
}

// checkAgentPaths rejects empty paths and paths listed under two state modes
// of the same agent, after home expansion.
func checkAgentPaths(layers []*File, home string) error {
	for _, l := range layers {
		names := make([]string, 0, len(l.Agents))
		for name := range l.Agents {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			p := l.Agents[name].Paths
			modes := []struct {
				mode string
				list []string
			}{
				{"share", p.Share}, {"copy", p.Copy},
				{"private", p.Private}, {"ro", p.RO}, {"cache", p.Cache},
			}
			seen := map[string]string{}
			for _, m := range modes {
				for _, raw := range m.list {
					if raw == "" {
						return fmt.Errorf("agents.%s: empty path in %s", name, m.mode)
					}
					exp := paths.Expand(raw, home)
					if prev, ok := seen[exp]; ok {
						return fmt.Errorf("agents.%s: %q is listed in both %s and %s", name, exp, prev, m.mode)
					}
					seen[exp] = m.mode
				}
			}
		}
	}
	return nil
}
