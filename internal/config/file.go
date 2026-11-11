// Package config loads kandor's TOML configuration file and merges it with
// the command line into resolved settings.
package config

import "fmt"

// File is the decoded configuration file.  Scalars are pointers so a key
// that is absent can be told apart from one set to its zero value.
type File struct {
	Mounts      MountLists              `toml:"mounts,omitempty"`
	Env         EnvSection              `toml:"env,omitempty"`
	Sandbox     SandboxSection          `toml:"sandbox,omitempty"`
	Credentials CredentialsSection      `toml:"credentials,omitempty"`
	Git         GitSection              `toml:"git,omitempty"`
	Profile     ProfileSection          `toml:"profile,omitempty"`
	Agents      map[string]AgentSection `toml:"agents,omitempty"`
	// DefaultAgent selects the agent to run: --agent and KANDOR_AGENT win
	// over it, a project file overrides the user file for that project, and
	// it falls back to "opencode".
	DefaultAgent string `toml:"default_agent,omitempty"`
	// Sections is a library of named, reusable stacks of settings (such as
	// "go" or "python").  They apply only when included: the "default"
	// section is always included, and a project file opts into more with
	// "include".  Sections may only be defined in the user configuration
	// file, not in a project one.
	Sections map[string]Section `toml:"sections,omitempty"`
	// Include names sections from Sections to apply on top of the file's
	// own keys.  In a project file it opts into the user's stacks.
	Include []string `toml:"include,omitempty"`
}

// Section is one named stack of settings.  It holds the same settable
// settings as the top level of File (including Paths, which apply to the
// agent being run), but cannot nest further sections or carry its own
// include list, so resolution stays a single flat pass.
type Section struct {
	Mounts      MountLists              `toml:"mounts,omitempty"`
	Env         EnvSection              `toml:"env,omitempty"`
	Sandbox     SandboxSection          `toml:"sandbox,omitempty"`
	Credentials CredentialsSection      `toml:"credentials,omitempty"`
	Git         GitSection              `toml:"git,omitempty"`
	Profile     ProfileSection          `toml:"profile,omitempty"`
	Agents      map[string]AgentSection `toml:"agents,omitempty"`
	// Paths applies to whichever agent is running when the section is
	// included.
	Paths AgentPaths `toml:"paths,omitempty"`
}

// layer returns the section as a File so it merges like any other layer.
func (s *Section) layer() *File {
	return &File{
		Mounts:      s.Mounts,
		Env:         s.Env,
		Sandbox:     s.Sandbox,
		Credentials: s.Credentials,
		Git:         s.Git,
		Profile:     s.Profile,
		Agents:      s.Agents,
	}
}

type MountLists struct {
	ReadOnly  []string `toml:"read_only,omitempty"`
	ReadWrite []string `toml:"read_write,omitempty"`
}

type EnvSection struct {
	Pass []string          `toml:"pass,omitempty"`
	Set  map[string]string `toml:"set,omitempty"`
}

type SandboxSection struct {
	Network     *bool           `toml:"network,omitempty"`
	AllowUserns *bool           `toml:"allow_userns,omitempty"`
	NewSession  *NewSessionMode `toml:"new_session,omitempty"`
}

type CredentialsSection struct {
	SSH            *bool `toml:"ssh,omitempty"`
	SSHAgent       *bool `toml:"ssh_agent,omitempty"`
	GitCredentials *bool `toml:"git_credentials,omitempty"`
	Netrc          *bool `toml:"netrc,omitempty"`
	Npmrc          *bool `toml:"npmrc,omitempty"`
}

type GitSection struct {
	MountConfig *bool `toml:"mount_config,omitempty"`
	Protect     *bool `toml:"protect,omitempty"`
}

type ProfileSection struct {
	Isolated *bool   `toml:"isolated,omitempty"`
	Name     *string `toml:"name,omitempty"`
	Seed     *bool   `toml:"seed,omitempty"`
}

// AgentSection holds the settings that apply only when one agent runs.
type AgentSection struct {
	Binary *string `toml:"binary,omitempty"`
	// Subcommands extends the agent's known subcommands (used to tell a
	// project directory apart from a subcommand on the command line).
	Subcommands []string `toml:"subcommands,omitempty"`
	// Paths declares host state for the agent: share mounts it read-write
	// (warned), copy seeds it into the profile once and mounts the copy
	// read-write, private gives it a fresh profile directory while the
	// host path stays hidden, and ro mounts it read-only.
	Paths  AgentPaths `toml:"paths,omitempty"`
	Mounts MountLists `toml:"mounts,omitempty"`
	Env    AgentEnv   `toml:"env,omitempty"`
}

// AgentPaths holds the host-state modes for one agent.
type AgentPaths struct {
	Share   []string `toml:"share,omitempty"`
	Copy    []string `toml:"copy,omitempty"`
	Private []string `toml:"private,omitempty"`
	RO      []string `toml:"ro,omitempty"`
	// Cache is kandor-owned persistent state, never the host's path.
	Cache []string `toml:"cache,omitempty"`
}

// AgentEnv is EnvSection plus the host variables one agent must never see.
type AgentEnv struct {
	Pass    []string          `toml:"pass,omitempty"`
	Set     map[string]string `toml:"set,omitempty"`
	Exclude []string          `toml:"exclude,omitempty"`
}

// NewSessionMode is sandbox.new_session: "auto", true or false.
type NewSessionMode int

const (
	SessionAuto NewSessionMode = iota
	SessionOn
	SessionOff
)

func (m *NewSessionMode) UnmarshalTOML(v any) error {
	switch v {
	case "auto":
		*m = SessionAuto
	case true:
		*m = SessionOn
	case false:
		*m = SessionOff
	default:
		return fmt.Errorf("new_session must be \"auto\", true or false, not %v", v)
	}
	return nil
}

func (m NewSessionMode) MarshalTOML() ([]byte, error) {
	switch m {
	case SessionOn:
		return []byte("true"), nil
	case SessionOff:
		return []byte("false"), nil
	}
	return []byte(`"auto"`), nil
}
