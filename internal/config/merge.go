package config

import (
	"fmt"
	"regexp"
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
	layers, err := resolveLayers(user, project)
	if err != nil {
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
		s.Env = append(s.Env, envSpecs(l.Env)...)
	}

	for _, l := range layers {
		a := l.Agents[agent]
		mounts, err := expandLayer(lists(a.Mounts), home)
		if err != nil {
			return Settings{}, err
		}
		s.Mounts = append(s.Mounts, mounts...)
		s.Env = append(s.Env, envSpecs(a.Env)...)
		if a.Binary != nil {
			s.AgentBinary = *a.Binary
		}
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
// the project file itself.  Each named section applies at most once.
func resolveLayers(user, project *File) ([]*File, error) {
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
		layers = append(layers, sec.layer())
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

func envSpecs(e EnvSection) []EnvSpec {
	out := make([]EnvSpec, 0, len(e.Pass)+len(e.Set))
	for _, name := range e.Pass {
		out = append(out, EnvSpec{Name: name})
	}
	names := make([]string, 0, len(e.Set))
	for name := range e.Set {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, EnvSpec{Name: name, Value: e.Set[name], HasValue: true})
	}
	return out
}
