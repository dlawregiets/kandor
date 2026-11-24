package agent

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

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
	known = Known(map[string]config.AgentSection{"pi": {}}, map[string]config.AgentSection{"aider": {}})
	if !slices.Equal(known, []string{"aider", "claude", "copilot", "opencode", "pi"}) {
		t.Errorf("Known with project agents = %v", known)
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

func TestGenericPrepareNodeCompileCache(t *testing.T) {
	layout := Layout{HostCache: t.TempDir()}
	ag := For("pi", config.AgentSection{})
	state, err := ag.Prepare(layout, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(layout.HostCache, "kandor", "pi", "node-compile-cache")
	if !hasBind(state.Binds, dir) || !hasEnv(state.Env, "NODE_COMPILE_CACHE", dir) {
		t.Errorf("state = %+v", state)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Errorf("cache directory was not created: %v", err)
	}
	// An agent without the opt-in gets nothing.
	state, _ = For("copilot", config.AgentSection{}).Prepare(layout, func(string) {})
	if len(state.Binds) != 0 || len(state.Env) != 0 {
		t.Errorf("copilot got a compile cache: %+v", state)
	}
}

func TestGenericPrepareScratchDir(t *testing.T) {
	layout := Layout{HostCache: t.TempDir()}
	dir := filepath.Join(layout.HostCache, "kandor", "pi", "tmp")
	// A leftover from an old run goes; a fresh one stays.
	stale := filepath.Join(dir, "stale-entry")
	fresh := filepath.Join(dir, "fresh-entry")
	for _, p := range []string{stale, fresh} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * scratchKeep)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	// An old directory whose contents were rewritten recently stays:
	// rewriting a file does not update the directory's mod-time.
	rewritten := filepath.Join(dir, "rewritten-entry")
	if err := os.MkdirAll(rewritten, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rewritten, "cache"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(rewritten, old, old); err != nil {
		t.Fatal(err)
	}
	// An old directory with only old contents goes.
	oldOnly := filepath.Join(dir, "old-only-entry")
	if err := os.MkdirAll(oldOnly, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldOnly, "cache"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(oldOnly, "cache"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldOnly, old, old); err != nil {
		t.Fatal(err)
	}

	state, err := For("pi", config.AgentSection{}).Prepare(layout, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if !hasBind(state.Binds, dir) || !hasEnv(state.Env, "TMPDIR", dir) {
		t.Errorf("state = %+v", state)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale scratch entry survived: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh scratch entry was removed: %v", err)
	}
	if _, err := os.Stat(rewritten); err != nil {
		t.Errorf("recently rewritten scratch entry was removed: %v", err)
	}
	if _, err := os.Stat(oldOnly); !os.IsNotExist(err) {
		t.Errorf("old scratch entry with old contents survived: %v", err)
	}

	// A dry run plans the same directory without creating it.
	dry := Layout{HostCache: t.TempDir(), DryRun: true}
	state, err = For("pi", config.AgentSection{}).Prepare(dry, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	planned := filepath.Join(dry.HostCache, "kandor", "pi", "tmp")
	if !hasBind(state.Binds, planned) || !hasEnv(state.Env, "TMPDIR", planned) {
		t.Errorf("dry-run state = %+v", state)
	}
	if _, err := os.Stat(filepath.Join(dry.HostCache, "kandor")); !os.IsNotExist(err) {
		t.Errorf("dry run created %s", filepath.Join(dry.HostCache, "kandor"))
	}
}

func hasBind(binds []Bind, dir string) bool {
	return slices.ContainsFunc(binds, func(b Bind) bool { return b.Src == dir && b.Dest == dir && b.Writable })
}

func hasEnv(vars []EnvVar, name, value string) bool {
	return slices.ContainsFunc(vars, func(v EnvVar) bool { return v.Name == name && v.Value == value })
}
