package config

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/dlawregiets/kandor/internal/paths"
)

var profileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Merge applies built-in defaults, then the file's top-level sections, then
// the file's section for agent, then the command line.
func Merge(f *File, o Overrides, agent, home, cwd string, getenv func(string) string) (Settings, error) {
	s := Settings{
		ProjectDir:     cwd,
		Network:        true,
		MountGitConfig: true,
		ProtectGit:     true,
		Seed:           true,
		Profile:        "default",
	}
	a := f.Agents[agent]

	set(&s.Network, f.Sandbox.Network, o.Network)
	set(&s.AllowUserns, f.Sandbox.AllowUserns, o.AllowUserns)
	set(&s.NewSession, f.Sandbox.NewSession, o.NewSession)
	set(&s.SSH, f.Credentials.SSH, o.SSH)
	set(&s.SSHAgent, f.Credentials.SSHAgent, o.SSHAgent)
	set(&s.GitCredentials, f.Credentials.GitCredentials, o.GitCredentials)
	set(&s.Netrc, f.Credentials.Netrc, o.Netrc)
	set(&s.Npmrc, f.Credentials.Npmrc, o.Npmrc)
	set(&s.MountGitConfig, f.Git.MountConfig, o.MountGitConfig)
	set(&s.ProtectGit, f.Git.Protect, o.ProtectGit)
	set(&s.Seed, f.Profile.Seed, o.Seed)

	// A profile name implies an isolated profile, like --profile, unless the
	// file also says explicitly whether to isolate.
	if f.Profile.Name != nil {
		s.Profile = *f.Profile.Name
		s.Isolated = true
	}
	set(&s.Isolated, f.Profile.Isolated, o.Isolated)
	if o.Profile != "" {
		s.Profile = o.Profile
	}
	if !profileName.MatchString(s.Profile) {
		return Settings{}, fmt.Errorf("invalid profile name '%s' (use letters, digits, '.', '_' and '-')", s.Profile)
	}

	if o.ProjectDir != "" {
		s.ProjectDir = o.ProjectDir
	}

	layers := [][]MountSpec{lists(f.Mounts), lists(a.Mounts), o.Mounts}
	for _, layer := range layers {
		mounts, err := expandLayer(layer, home)
		if err != nil {
			return Settings{}, err
		}
		s.Mounts = append(s.Mounts, mounts...)
	}

	s.Env = append(s.Env, envSpecs(f.Env)...)
	s.Env = append(s.Env, envSpecs(a.Env)...)
	s.Env = append(s.Env, o.Env...)

	s.AgentBinary = getenv("KANDOR_BIN")
	if s.AgentBinary == "" && a.Binary != nil {
		s.AgentBinary = *a.Binary
	}
	return s, nil
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
