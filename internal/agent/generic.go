package agent

import (
	"slices"
	"strings"

	"github.com/dlawregiets/kandor/internal/config"
)

// generic is a config-defined agent: no bespoke behavior, just the declared
// identity, subcommands, exclusions and state paths.
type generic struct {
	spec Spec
}

func (g generic) Name() string       { return g.spec.Name }
func (g generic) Executable() string { return g.spec.Binary }

func (g generic) IsSubcommand(arg string) bool {
	return slices.Contains(g.spec.Subcommands, arg)
}

func (g generic) ExcludeEnv(name string) bool {
	for _, e := range g.spec.EnvExclude {
		if e != "" && (name == e || (strings.HasSuffix(e, "_") && strings.HasPrefix(name, e))) {
			return true
		}
	}
	return false
}

// Prepare binds nothing itself: host state arrives through Materialize,
// which app applies from the merged spec after Prepare.
func (g generic) Prepare(Layout, func(string)) (State, error) { return State{}, nil }

// Command passes arguments through unchanged.
func (g generic) Command(bin string, args []string) ([]string, error) {
	return append([]string{bin}, args...), nil
}

// For returns the runtime agent: OpenCode keeps its bespoke behavior,
// everything else is generic over the merged section.
func For(name string, cfg config.AgentSection) Agent {
	if name == OpenCode.Name() {
		return OpenCode
	}
	return generic{spec: Resolve(name, cfg)}
}

// Subcommands returns the known subcommands for name from built-in defaults
// plus the user file's top-level section: enough to tell a project directory
// apart from a subcommand while parsing, before layers merge.
func Subcommands(name string, cfg config.AgentSection) []string {
	s, _ := builtin(name)
	return appendUnique(slices.Clone(s.Subcommands), cfg.Subcommands)
}

// Known lists the selectable agents: built-ins plus user-defined sections,
// sorted.
func Known(user map[string]config.AgentSection) []string {
	var seen []string
	for _, s := range builtinSpecs {
		seen = append(seen, s.Name)
	}
	for name := range user {
		if !slices.Contains(seen, name) {
			seen = append(seen, name)
		}
	}
	slices.Sort(seen)
	return seen
}

// ProvisionalIsSubcommand matches any built-in subcommand.  It backs the
// pre-parse before the agent is resolved; the full parse uses the resolved
// agent's list instead.
func ProvisionalIsSubcommand(arg string) bool {
	for _, s := range builtinSpecs {
		if slices.Contains(s.Subcommands, arg) {
			return true
		}
	}
	return false
}

// IsSubcommandFor reports whether arg is a subcommand of the named agent.
func IsSubcommandFor(name string, cfg config.AgentSection) func(string) bool {
	subs := Subcommands(name, cfg)
	return func(arg string) bool {
		return slices.Contains(subs, arg)
	}
}
