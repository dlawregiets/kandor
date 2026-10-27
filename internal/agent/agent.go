// Package agent describes the coding agents kandor can run.  Everything
// agent-specific (binary, state directories, command-line rewriting) lives
// behind the Agent interface so the sandbox planner stays agent-neutral.
package agent

// Bind is a mount the agent needs.
type Bind struct {
	Src, Dest string
	Writable  bool
	// FollowSymlinks also mounts the resolved targets of symlinks inside Src,
	// at their own paths, so the links resolve inside the sandbox.
	FollowSymlinks bool
}

// Mask hides a host file behind Src wherever a mount makes Dest visible in
// the sandbox.  If Dest is missing in a writable directory, Placeholder is
// written there first, so the sandbox cannot create the file itself.
type Mask struct {
	Src, Dest   string
	Placeholder string
}

// EnvVar is one variable set inside the sandbox.
type EnvVar struct {
	Name, Value string
}

// Layout is where the sandbox's XDG directories live, plus the host's.
type Layout struct {
	Config, Data, State, Cache                 string
	HostConfig, HostData, HostState, HostCache string
	Isolated, Seed                             bool
	// ProfileRoot is the isolated profile's directory ("" in shared mode).
	ProfileRoot string
	// Writable reports whether a host path is explicitly mounted read-write
	// (nil means none is).
	Writable func(path string) bool
}

// State is what an agent needs from the sandbox after Prepare.
type State struct {
	Binds []Bind
	// Env is set after the XDG variables, so it can override them.
	Env   []EnvVar
	Masks []Mask
}

// Agent is a coding agent kandor can run.
type Agent interface {
	Name() string
	// Executable is the program name searched for in PATH.
	Executable() string
	// IsSubcommand reports whether arg is one of the agent's subcommands, so
	// a directory with that name is not mistaken for the project.
	IsSubcommand(arg string) bool
	// ExcludeEnv reports whether a host variable must never be forwarded.
	ExcludeEnv(name string) bool
	// Prepare creates and fills the agent's state directories.
	Prepare(l Layout, warn func(string)) (State, error)
	// Command builds the agent's command line, or refuses it.
	Command(bin string, args []string) ([]string, error)
}

// Lookup returns the agent with the given name.
func Lookup(name string) (Agent, bool) {
	if name == OpenCode.Name() {
		return OpenCode, true
	}
	return nil, false
}
