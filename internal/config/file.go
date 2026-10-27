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
	Binary *string    `toml:"binary,omitempty"`
	Mounts MountLists `toml:"mounts,omitempty"`
	Env    EnvSection `toml:"env,omitempty"`
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
