package config

// MountSpec is one extra mount requested by the configuration file or the
// command line.
type MountSpec struct {
	Path     string
	Writable bool
}

// EnvSpec is one environment variable to pass into the sandbox.  HasValue
// false means "copy the host value", like --env NAME.
type EnvSpec struct {
	Name     string
	Value    string
	HasValue bool
}

// Overrides is the command-line layer.  Nil pointers and empty strings mean
// "not given on the command line".
type Overrides struct {
	ProjectDir string
	Mounts     []MountSpec
	Env        []EnvSpec

	Network, AllowUserns                        *bool
	SSH, SSHAgent, GitCredentials, Netrc, Npmrc *bool
	MountGitConfig, ProtectGit                  *bool
	Isolated, Seed                              *bool
	NewSession                                  *NewSessionMode
	Profile                                     string
}

// Settings is the fully merged configuration for one run.
type Settings struct {
	ProjectDir string
	// Mounts are in layer order; a later mount of the same path wins.
	Mounts []MountSpec
	// Env is in layer order; a later variable of the same name wins.
	Env []EnvSpec

	Network     bool
	AllowUserns bool
	NewSession  NewSessionMode

	SSH, SSHAgent, GitCredentials, Netrc, Npmrc bool

	MountGitConfig bool
	ProtectGit     bool

	Isolated bool
	Profile  string
	Seed     bool

	AgentBinary string
}
