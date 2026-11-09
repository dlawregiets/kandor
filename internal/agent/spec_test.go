package agent

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dlawregiets/kandor/internal/config"
)

func ptr[T any](v T) *T { return &v }

func TestResolveBuiltinDefaults(t *testing.T) {
	s := Resolve("opencode", config.AgentSection{})
	if s.Name != "opencode" || s.Binary != "opencode" {
		t.Errorf("identity = %+v", s)
	}
	if !slices.Contains(s.Subcommands, "run") || !slices.Contains(s.EnvExclude, "OPENCODE_SESSION_ID") {
		t.Errorf("builtin lists missing: %+v", s)
	}
}

func TestResolveMergesUserSection(t *testing.T) {
	cfg := config.AgentSection{
		Binary:      ptr("/x/claude"),
		Subcommands: []string{"extra", "mcp"},
		Paths: config.AgentPaths{
			Share: []string{"~/share"},
			Copy:  []string{"~/copy"},
		},
		Env: config.AgentEnv{Exclude: []string{"CLAUDECODE", "PI_"}},
	}
	s := Resolve("pi", cfg)
	if s.Binary != "/x/claude" {
		t.Errorf("Binary = %q", s.Binary)
	}
	// Built-in subcommands stay, new ones append, duplicates drop.
	want := append([]string{"install", "remove", "uninstall", "update", "list", "config", "auth", "mcp"}, "extra")
	if !slices.Equal(s.Subcommands, want) {
		t.Errorf("Subcommands = %q, want %q", s.Subcommands, want)
	}
	// Built-in copies stay, user paths append.
	if !slices.Equal(s.Share, []string{"~/share"}) ||
		!slices.Equal(s.Copy, []string{"~/.pi", "~/copy"}) {
		t.Errorf("paths = %+v", s)
	}
	if !slices.Equal(s.EnvExclude, []string{"PI_", "CLAUDECODE"}) {
		t.Errorf("EnvExclude = %q", s.EnvExclude)
	}
}

func testHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{".claude", ".config/ghostty", "work/.pi"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestMaterializeEmpty(t *testing.T) {
	var warnings []string
	if binds := Materialize(Spec{}, t.TempDir(), t.TempDir(), func(w string) { warnings = append(warnings, w) }); len(binds) != 0 {
		t.Errorf("empty spec produced %d binds", len(binds))
	}
	if len(warnings) != 0 {
		t.Errorf("empty spec warned: %v", warnings)
	}
}

func TestMaterializeModes(t *testing.T) {
	home := testHome(t)
	store := t.TempDir()
	var warnings []string
	warn := func(w string) { warnings = append(warnings, w) }
	spec := Spec{
		Share:    []string{"~/.config/ghostty", "~/missing", "relative/path"},
		ReadOnly: []string{"~/work/.pi"},
		Copy:     []string{"~/.claude", "~/.claude.json"},
		Private:  []string{"~/.pi", "/nowhere/private-dir"},
	}
	binds := Materialize(spec, store, home, warn)
	byDest := map[string]Bind{}
	for _, b := range binds {
		byDest[b.Dest] = b
	}
	mustBind := func(dest, src string, writable bool) {
		t.Helper()
		b, ok := byDest[dest]
		if !ok {
			t.Errorf("no bind for %s (binds: %+v)", dest, binds)
			return
		}
		if b.Src != src || b.Writable != writable {
			t.Errorf("bind for %s = %+v, want src %s writable %v", dest, b, src, writable)
		}
	}
	mustBind(filepath.Join(home, ".config/ghostty"), filepath.Join(home, ".config/ghostty"), true)
	mustBind(filepath.Join(home, "work/.pi"), filepath.Join(home, "work/.pi"), false)
	mustBind(filepath.Join(home, ".claude"), filepath.Join(store, ".claude"), true)
	mustBind(filepath.Join(home, ".claude.json"), filepath.Join(store, ".claude.json"), true)
	mustBind(filepath.Join(home, ".pi"), filepath.Join(store, ".pi"), true)
	mustBind("/nowhere/private-dir", filepath.Join(store, "abs_nowhere_private-dir"), true)
	if len(warnings) != 3 {
		t.Errorf("warnings = %v, want share notice + 2 skips", warnings)
	}
	// The copies actually landed in the store.
	for _, p := range []string{".claude", ".claude.json"} {
		if _, err := os.Lstat(filepath.Join(store, p)); err != nil {
			t.Errorf("store is missing %s: %v", p, err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(store, ".pi")); len(entries) != 0 {
		t.Errorf("private store is not empty: %v", entries)
	}
}

func TestStoreRel(t *testing.T) {
	if got := storeRel("/h/.claude", "/h"); got != ".claude" {
		t.Errorf("storeRel = %q", got)
	}
	if got := storeRel("/opt/x", "/h"); got != "abs_opt_x" {
		t.Errorf("storeRel = %q", got)
	}
	if got := storeRel("/h", "/h"); got != "." {
		t.Errorf("storeRel = %q", got)
	}
}

func TestBuiltinClaude(t *testing.T) {
	s := Resolve("claude", config.AgentSection{})
	if s.Name != "claude" || s.Binary != "claude" {
		t.Errorf("identity = %+v", s)
	}
	if !slices.Contains(s.Subcommands, "mcp") || !slices.Contains(s.Subcommands, "doctor") {
		t.Errorf("Subcommands = %v", s.Subcommands)
	}
	if !slices.Equal(s.Copy, []string{"~/.claude", "~/.claude.json"}) {
		t.Errorf("Copy = %v", s.Copy)
	}
	if !slices.Equal(s.EnvExclude, []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT"}) {
		t.Errorf("EnvExclude = %v", s.EnvExclude)
	}
	// User configuration wins over the built-in defaults.
	s = Resolve("claude", config.AgentSection{Binary: ptr("/opt/claude")})
	if s.Binary != "/opt/claude" || len(s.Copy) != 2 {
		t.Errorf("override = %+v", s)
	}
}

func TestBuiltinPiAndCopilot(t *testing.T) {
	pi := Resolve("pi", config.AgentSection{})
	if pi.Binary != "pi" || !slices.Equal(pi.Copy, []string{"~/.pi"}) ||
		!slices.Equal(pi.EnvExclude, []string{"PI_"}) || !slices.Contains(pi.Subcommands, "auth") {
		t.Errorf("pi = %+v", pi)
	}
	copilot := Resolve("copilot", config.AgentSection{})
	if copilot.Binary != "copilot" || !slices.Equal(copilot.Copy, []string{"~/.copilot"}) ||
		!slices.Equal(copilot.EnvExclude, []string{"COPILOT_"}) || !slices.Contains(copilot.Subcommands, "login") {
		t.Errorf("copilot = %+v", copilot)
	}
	// User configuration still overrides a built-in.
	copilot = Resolve("copilot", config.AgentSection{Binary: ptr("/opt/copilot")})
	if copilot.Binary != "/opt/copilot" || len(copilot.Copy) != 1 {
		t.Errorf("override = %+v", copilot)
	}
}

func TestBuiltinExcludePrefixes(t *testing.T) {
	ag := For("pi", config.AgentSection{})
	for _, name := range []string{"PI_OFFLINE", "PI_", "PI"} {
		want := name != "PI"
		if got := ag.ExcludeEnv(name); got != want {
			t.Errorf("pi.ExcludeEnv(%q) = %v, want %v", name, got, want)
		}
	}
	if ag.ExcludeEnv("ANTHROPIC_API_KEY") {
		t.Error("pi blocked a provider key")
	}
}
