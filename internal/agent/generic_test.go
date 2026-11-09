package agent

import (
	"slices"
	"testing"

	"github.com/dlawregiets/kandor/internal/config"
)

func TestForOpenCode(t *testing.T) {
	if For("opencode", config.AgentSection{}) != OpenCode {
		t.Error("For(opencode) is not the OpenCode singleton")
	}
}

func TestGenericAgent(t *testing.T) {
	cfg := config.AgentSection{
		Binary:      ptr("/opt/pi"),
		Subcommands: []string{"mcp"},
		Env:         config.AgentEnv{Exclude: []string{"PI_", "PI_KEY"}},
	}
	ag := For("pi", cfg)
	if ag.Name() != "pi" || ag.Executable() != "/opt/pi" {
		t.Errorf("identity = %s %s", ag.Name(), ag.Executable())
	}
	if !ag.IsSubcommand("mcp") || ag.IsSubcommand("run") {
		t.Error("subcommands wrong")
	}
	for _, blocked := range []string{"PI_", "PI_KEY", "PI_STATE"} {
		if !ag.ExcludeEnv(blocked) {
			t.Errorf("ExcludeEnv(%q) = false", blocked)
		}
	}
	if ag.ExcludeEnv("ANTHROPIC_API_KEY") {
		t.Error("ExcludeEnv blocked an unrelated variable")
	}
	st, err := ag.Prepare(Layout{}, func(string) {})
	if err != nil || len(st.Binds) != 0 {
		t.Errorf("Prepare = %+v, %v", st, err)
	}
	cmd, err := ag.Command("/x/pi", []string{"a"})
	if err != nil || !slices.Equal(cmd, []string{"/x/pi", "a"}) {
		t.Errorf("Command = %v, %v", cmd, err)
	}
}

func TestGenericDefaultsToName(t *testing.T) {
	ag := For("copilot", config.AgentSection{})
	if ag.Executable() != "copilot" {
		t.Errorf("Executable = %q", ag.Executable())
	}
}

func TestSubcommandsUnion(t *testing.T) {
	subs := Subcommands("opencode", config.AgentSection{Subcommands: []string{"mcp"}})
	if !slices.Contains(subs, "run") || !slices.Contains(subs, "mcp") {
		t.Errorf("Subcommands = %v", subs)
	}
}

func TestKnownAgents(t *testing.T) {
	known := Known(map[string]config.AgentSection{"pi": {}, "copilot": {}})
	if !slices.Equal(known, []string{"claude", "copilot", "opencode", "pi"}) {
		t.Errorf("Known = %v", known)
	}
}

func TestProvisionalIsSubcommand(t *testing.T) {
	if !ProvisionalIsSubcommand("run") {
		t.Error("run not recognised")
	}
	if ProvisionalIsSubcommand("definitely-not-a-subcommand") {
		t.Error("unknown token recognised")
	}
}

func TestIsSubcommandFor(t *testing.T) {
	isSub := IsSubcommandFor("pi", config.AgentSection{Subcommands: []string{"mcp"}})
	if !isSub("mcp") || isSub("run") {
		t.Error("IsSubcommandFor wrong")
	}
}
