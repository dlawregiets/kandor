package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func command(t *testing.T, args ...string) []string {
	t.Helper()
	cmd, err := OpenCode.Command("/bin/oc", args)
	if err != nil {
		t.Fatalf("Command(%q) error: %v", args, err)
	}
	return cmd
}

func wantCmd(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("command = %q, want %q", got, want)
	}
}

func TestCommandTUI(t *testing.T) {
	wantCmd(t, command(t), "/bin/oc", "--standalone")
	wantCmd(t, command(t, "--continue"), "/bin/oc", "--standalone", "--continue")
}

func TestCommandRun(t *testing.T) {
	wantCmd(t, command(t, "run", "-x"), "/bin/oc", "run", "--standalone", "-x")
}

func TestCommandLeadingStandalone(t *testing.T) {
	wantCmd(t, command(t, "--standalone", "run"), "/bin/oc", "run", "--standalone")
}

func TestCommandNested(t *testing.T) {
	wantCmd(t, command(t, "auth", "login"), "/bin/oc", "auth", "login", "--standalone")
	wantCmd(t, command(t, "auth"), "/bin/oc", "auth")
	wantCmd(t, command(t, "session", "-q", "export", "id"), "/bin/oc", "session", "export", "--standalone", "-q", "id")
}

func TestCommandHelpUntouched(t *testing.T) {
	wantCmd(t, command(t, "run", "--help"), "/bin/oc", "run", "--help")
	wantCmd(t, command(t, "run", "--", "--help"), "/bin/oc", "run", "--standalone", "--", "--help")
	wantCmd(t, command(t, "serve"), "/bin/oc", "serve")
}

func TestCommandRefusals(t *testing.T) {
	cases := map[string][]string{
		"refusing to run 'opencode service'": {"service"},
		"refusing to run 'opencode upgrade'": {"upgrade"},
		"refusing to run 'opencode pair'":    {"pair"},
		"refusing '--server=x'":              {"run", "--server=x"},
		"refusing '--service'":               {"--service"},
		"kandor auth login":                  {"acp", "--login"},
	}
	for want, args := range cases {
		_, err := OpenCode.Command("/bin/oc", args)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Command(%q) error = %v, want %q", args, err, want)
		}
	}
}

func TestSubcommandsAndExclusions(t *testing.T) {
	if !OpenCode.IsSubcommand("run") || !OpenCode.IsSubcommand("completions") || OpenCode.IsSubcommand("src") {
		t.Error("IsSubcommand wrong")
	}
	if !OpenCode.ExcludeEnv("OPENCODE_SERVER_PASSWORD") || !OpenCode.ExcludeEnv("AI_AGENT") || OpenCode.ExcludeEnv("ANTHROPIC_API_KEY") {
		t.Error("ExcludeEnv wrong")
	}
	if a, ok := Lookup("opencode"); !ok || a.Executable() != "opencode" || a.Name() != "opencode" {
		t.Error("Lookup(opencode) failed")
	}
	if _, ok := Lookup("pi"); ok {
		t.Error("Lookup(pi) succeeded")
	}
}

type fixture struct {
	layout Layout
	warns  []string
}

func newFixture(t *testing.T, isolated bool) *fixture {
	t.Helper()
	root := t.TempDir()
	l := Layout{
		Config: filepath.Join(root, "sb/config"), Data: filepath.Join(root, "sb/data"),
		State: filepath.Join(root, "sb/state"), Cache: filepath.Join(root, "sb/cache"),
		HostConfig: filepath.Join(root, "host/config"), HostData: filepath.Join(root, "host/data"),
		HostCache: filepath.Join(root, "host/cache"), Isolated: isolated, Seed: true,
	}
	hostOC := filepath.Join(l.HostConfig, "opencode")
	os.MkdirAll(filepath.Join(hostOC, "plugins"), 0o755)
	os.WriteFile(filepath.Join(hostOC, "opencode.json"), []byte(`{"v":1}`), 0o644)
	os.WriteFile(filepath.Join(hostOC, "service.json"), []byte(`{"password":"host"}`), 0o600)
	os.WriteFile(filepath.Join(hostOC, "plugins", "p.js"), []byte("x"), 0o755)
	return &fixture{layout: l}
}

func (f *fixture) prepare(t *testing.T) State {
	t.Helper()
	st, err := OpenCode.Prepare(f.layout, func(s string) { f.warns = append(f.warns, s) })
	if err != nil {
		t.Fatalf("Prepare error: %v", err)
	}
	return st
}

func TestPrepareIsolatedSeedsOnce(t *testing.T) {
	f := newFixture(t, true)
	f.prepare(t)
	cfg := filepath.Join(f.layout.Config, "opencode")
	if data, _ := os.ReadFile(filepath.Join(cfg, "opencode.json")); string(data) != `{"v":1}` {
		t.Errorf("opencode.json not seeded: %q", data)
	}
	if st, err := os.Stat(filepath.Join(cfg, "plugins", "p.js")); err != nil || st.Mode().Perm() != 0o755 {
		t.Errorf("plugins not copied with modes: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(cfg, "service.json")); strings.Contains(string(data), "host") {
		t.Error("host service.json was copied")
	}
	os.WriteFile(filepath.Join(f.layout.HostConfig, "opencode", "opencode.json"), []byte(`{"v":2}`), 0o644)
	f.prepare(t)
	if data, _ := os.ReadFile(filepath.Join(cfg, "opencode.json")); string(data) != `{"v":1}` {
		t.Error("isolated profile was seeded twice")
	}
}

func TestPrepareNoSeed(t *testing.T) {
	f := newFixture(t, true)
	f.layout.Seed = false
	f.prepare(t)
	if _, err := os.Stat(filepath.Join(f.layout.Config, "opencode", "opencode.json")); err == nil {
		t.Error("seed = false still copied the host config")
	}
}

func TestPrepareSharedCopiesEveryRun(t *testing.T) {
	f := newFixture(t, false)
	f.prepare(t)
	os.WriteFile(filepath.Join(f.layout.HostConfig, "opencode", "opencode.json"), []byte(`{"v":2}`), 0o644)
	f.prepare(t)
	if data, _ := os.ReadFile(filepath.Join(f.layout.Config, "opencode", "opencode.json")); string(data) != `{"v":2}` {
		t.Errorf("shared mode did not copy the current host config: %q", data)
	}
	if len(f.warns) == 0 || !strings.Contains(f.warns[0], "sharing the host OpenCode data directory") {
		t.Errorf("warnings = %q", f.warns)
	}
}

func TestPrepareWritesServiceJSON(t *testing.T) {
	f := newFixture(t, true)
	f.prepare(t)
	path := filepath.Join(f.layout.Config, "opencode", "service.json")
	data, err := os.ReadFile(path)
	var v struct{ Port int }
	if err != nil || json.Unmarshal(data, &v) != nil || v.Port <= 0 {
		t.Errorf("config service.json = %q, %v", data, err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("service.json mode = %v", st.Mode().Perm())
	}
	if data, _ := os.ReadFile(filepath.Join(f.layout.State, "opencode", "service.json")); string(data) != "{}\n" {
		t.Errorf("state service.json = %q", data)
	}
}

func TestPrepareBinds(t *testing.T) {
	f := newFixture(t, false)
	os.MkdirAll(filepath.Join(f.layout.HostCache, "opencode", "bin"), 0o755)
	st := f.prepare(t)
	for _, d := range []string{f.layout.Config, f.layout.Data, f.layout.State, f.layout.Cache} {
		p := filepath.Join(d, "opencode")
		if !slices.Contains(st.Binds, Bind{Src: p, Dest: p, Writable: true}) {
			t.Errorf("no writable bind for %s in %+v", p, st.Binds)
		}
	}
	bin := Bind{Src: filepath.Join(f.layout.HostCache, "opencode", "bin"), Dest: filepath.Join(f.layout.Cache, "opencode", "bin"), FollowSymlinks: true}
	if !slices.Contains(st.Binds, bin) {
		t.Errorf("no read-only cache/bin bind in %+v", st.Binds)
	}
	if !slices.Contains(st.Env, EnvVar{"OPENCODE_DISABLE_AUTOUPDATE", "1"}) {
		t.Errorf("Env = %+v", st.Env)
	}

	iso := newFixture(t, true)
	os.MkdirAll(filepath.Join(iso.layout.HostCache, "opencode", "bin"), 0o755)
	if st := iso.prepare(t); len(st.Binds) != 4 {
		t.Errorf("isolated binds = %+v, want only the four state directories", st.Binds)
	}
}
