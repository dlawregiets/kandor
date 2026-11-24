package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dlawregiets/kandor/internal/config"
)

func isSub(arg string) bool { return arg == "run" || arg == "auth" }

func parse(t *testing.T, args ...string) Request {
	t.Helper()
	r, err := Parse(args, isSub)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", args, err)
	}
	return r
}

func TestParseDefaults(t *testing.T) {
	if r := parse(t); !reflect.DeepEqual(r, Request{}) {
		t.Errorf("Parse() = %+v, want zero Request", r)
	}
}

func TestParseProjectDir(t *testing.T) {
	d := t.TempDir()
	if r := parse(t, "-C", d); r.Overrides.ProjectDir != d {
		t.Errorf("-C: ProjectDir = %q", r.Overrides.ProjectDir)
	}
	if r := parse(t, "--dir", d); r.Overrides.ProjectDir != d {
		t.Errorf("--dir: ProjectDir = %q", r.Overrides.ProjectDir)
	}
	r := parse(t, d, "--continue")
	if r.Overrides.ProjectDir != d || !slices.Equal(r.AgentArgs, []string{"--continue"}) {
		t.Errorf("positional dir: %+v", r)
	}
}

func TestParseSubcommandIsNotProject(t *testing.T) {
	r := parse(t, "run", "-x")
	if r.Overrides.ProjectDir != "" || !slices.Equal(r.AgentArgs, []string{"run", "-x"}) {
		t.Errorf("Parse(run -x) = %+v", r)
	}
	r = parse(t, "not-a-dir", "x")
	if !slices.Equal(r.AgentArgs, []string{"not-a-dir", "x"}) {
		t.Errorf("non-directory positional: AgentArgs = %q", r.AgentArgs)
	}
}

func TestParseUnknownOptionPassesThrough(t *testing.T) {
	r := parse(t, "--frobnicator", "plan", "--no-net")
	if !slices.Equal(r.AgentArgs, []string{"--frobnicator", "plan", "--no-net"}) || r.Overrides.Network != nil {
		t.Errorf("Parse(--frobnicator plan --no-net) = %+v", r)
	}
}

func TestParseDoubleDash(t *testing.T) {
	r := parse(t, "--no-net", "--", "--version")
	if r.Overrides.Network == nil || *r.Overrides.Network || !slices.Equal(r.AgentArgs, []string{"--version"}) || r.Version {
		t.Errorf("Parse(--no-net -- --version) = %+v", r)
	}
}

func TestParseHelpStops(t *testing.T) {
	r := parse(t, "--help", "--no-net")
	if !r.Help || r.Overrides.Network != nil {
		t.Errorf("Parse(--help --no-net) = %+v", r)
	}
	if r := parse(t, "-V", "--no-net"); !r.Version || r.Overrides.Network != nil {
		t.Errorf("Parse(-V --no-net) = %+v", r)
	}
}

func TestParseMountsInOrder(t *testing.T) {
	r := parse(t, "--ro", "a", "--rw", "b")
	want := []config.MountSpec{{Path: "a"}, {Path: "b", Writable: true}}
	if !reflect.DeepEqual(r.Overrides.Mounts, want) {
		t.Errorf("Mounts = %+v", r.Overrides.Mounts)
	}
}

func TestParseEnv(t *testing.T) {
	r := parse(t, "--env", "A", "--env", "B=1", "--env", "C=")
	want := []config.EnvSpec{{Name: "A"}, {Name: "B", Value: "1", HasValue: true}, {Name: "C", HasValue: true}}
	if !reflect.DeepEqual(r.Overrides.Env, want) {
		t.Errorf("Env = %+v", r.Overrides.Env)
	}
}

func TestParseProfileThenShared(t *testing.T) {
	r := parse(t, "--profile", "w", "--shared")
	if r.Overrides.Profile != "w" || r.Overrides.Isolated == nil || *r.Overrides.Isolated {
		t.Errorf("Parse(--profile w --shared) = %+v", r.Overrides)
	}
	r = parse(t, "--shared", "--profile", "w")
	if !*r.Overrides.Isolated {
		t.Error("--profile after --shared did not isolate")
	}
}

func TestParseFlags(t *testing.T) {
	r := parse(t, "--ssh", "--ssh-agent", "--git-credentials", "--netrc", "--npmrc", "--no-gitconfig",
		"--no-protect-git", "--allow-userns", "--isolated", "--no-seed", "--shell", "--dry-run", "--verbose", "--init-config")
	o := r.Overrides
	if !*o.SSH || !*o.SSHAgent || !*o.GitCredentials || !*o.Netrc || !*o.Npmrc || *o.MountGitConfig ||
		*o.ProtectGit || !*o.AllowUserns || !*o.Isolated || *o.Seed {
		t.Errorf("flags not applied: %+v", o)
	}
	if !r.Shell || !r.DryRun || !r.Verbose || !r.InitConfig {
		t.Errorf("actions not applied: %+v", r)
	}
	if r := parse(t, "--init-project"); !r.InitProject {
		t.Error("--init-project not recognised")
	}
	if r := parse(t, "--print-command"); !r.DryRun {
		t.Error("--print-command is not an alias of --dry-run")
	}
}

func TestParseNewSession(t *testing.T) {
	if r := parse(t, "--no-new-session"); r.Overrides.NewSession == nil || *r.Overrides.NewSession != config.SessionOff {
		t.Errorf("--no-new-session: %v", r.Overrides.NewSession)
	}
	if r := parse(t, "--new-session"); *r.Overrides.NewSession != config.SessionOn {
		t.Error("--new-session did not set SessionOn")
	}
}

func TestParseMissingArgument(t *testing.T) {
	cases := map[string]string{
		"--ro":      "--ro requires a path",
		"--rw":      "--rw requires a path",
		"--env":     "--env requires NAME or NAME=VALUE",
		"--profile": "--profile requires a name",
		"-C":        "-C requires a directory argument",
	}
	for opt, want := range cases {
		_, err := Parse([]string{opt}, isSub)
		if err == nil || err.Error() != want {
			t.Errorf("Parse(%s) error = %v, want %q", opt, err, want)
		}
	}
}

func TestOptionKeysAreClassified(t *testing.T) {
	keys := config.Keys()
	for opt, key := range OptionKeys() {
		if key != "" && !slices.Contains(keys, key) {
			t.Errorf("option %s maps to %q, which is not a setting", opt, key)
		}
	}
	for _, opt := range []string{"--ro", "--rw", "--env", "--no-net", "--ssh", "--profile", "--no-protect-git"} {
		if OptionKeys()[opt] == "" {
			t.Errorf("setting option %s has no key", opt)
		}
	}
}

func TestUsage(t *testing.T) {
	u := Usage()
	for _, want := range []string{"config.toml", "--init-config", "--init-project", "--no-protect-git", "Run a coding agent inside a bubblewrap sandbox."} {
		if !strings.Contains(u, want) {
			t.Errorf("Usage() is missing %q", want)
		}
	}
}

func TestParseAgent(t *testing.T) {
	if r := parse(t, "--agent", "pi", "run", "x"); r.Agent != "pi" || !slices.Equal(r.AgentArgs, []string{"run", "x"}) {
		t.Errorf("--agent: %+v", r)
	}
}

func TestAgentArg(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--agent", "pi", "run"}, "pi"},
		{[]string{"-C", dir, "--agent", "pi"}, "pi"},
		{[]string{"--ro", "/x", "--agent", "pi"}, "pi"},
		{[]string{dir, "--agent", "pi"}, "pi"},
		{[]string{"--", "--agent", "pi"}, ""},
		{[]string{"--unknown", "--agent", "pi"}, ""},
		{[]string{"not-a-dir", "--agent", "pi"}, ""},
		{[]string{"--agent"}, ""},
		{[]string{}, ""},
	} {
		if got := AgentArg(c.args); got != c.want {
			t.Errorf("AgentArg(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestParseExec(t *testing.T) {
	r := parse(t, "exec", "--", "ls", "-la")
	if !r.Exec || !slices.Equal(r.AgentArgs, []string{"ls", "-la"}) {
		t.Errorf("exec: %+v", r)
	}
	r = parse(t, "--dry-run", "exec", "ls")
	if !r.Exec || !r.DryRun || !slices.Equal(r.AgentArgs, []string{"ls"}) {
		t.Errorf("exec with leading flags: %+v", r)
	}
	r = parse(t, "exec", "--dry-run")
	if !r.Exec || r.DryRun || !slices.Equal(r.AgentArgs, []string{"--dry-run"}) {
		t.Errorf("flags after exec are the command: %+v", r)
	}
	dir := t.TempDir()
	r = parse(t, "-C", dir, "exec", "--", "true")
	if !r.Exec || r.Overrides.ProjectDir != dir || !slices.Equal(r.AgentArgs, []string{"true"}) {
		t.Errorf("exec with -C: %+v", r)
	}
	if _, err := Parse([]string{"exec"}, isSub); err == nil || err.Error() != "exec requires a command" {
		t.Errorf("bare exec err = %v", err)
	}
	// A directory named exec keeps working with -C or ./exec; only a
	// leading bare "exec" switches modes.
	execDir := filepath.Join(dir, "exec")
	if err := os.Mkdir(execDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r = parse(t, "-C", execDir, "run", "x")
	if r.Exec || r.Overrides.ProjectDir != execDir {
		t.Errorf("-C exec dir: %+v", r)
	}
}

func TestParseEqualsForm(t *testing.T) {
	d := t.TempDir()
	r := parse(t, "--agent=pi", "--dir="+d, "--ro=a", "--rw=b", "--env=A=1", "--profile=w")
	if r.Agent != "pi" || r.Overrides.ProjectDir != d || r.Overrides.Profile != "w" {
		t.Errorf("equals form: %+v", r)
	}
	want := []config.MountSpec{{Path: "a"}, {Path: "b", Writable: true}}
	if !reflect.DeepEqual(r.Overrides.Mounts, want) || !reflect.DeepEqual(r.Overrides.Env, []config.EnvSpec{{Name: "A", Value: "1", HasValue: true}}) {
		t.Errorf("equals mounts/env: %+v", r.Overrides)
	}
	if got := AgentArg([]string{"--agent=pi", "run"}); got != "pi" {
		t.Errorf("AgentArg(--agent=pi) = %q", got)
	}
	// Unknown options with "=" still pass through to the agent.
	if r := parse(t, "--model=foo"); !slices.Equal(r.AgentArgs, []string{"--model=foo"}) {
		t.Errorf("unknown --model=foo: %+v", r)
	}
	if _, err := Parse([]string{"--no-net=true"}, isSub); err == nil {
		t.Error("flag with =value was accepted")
	}
	if _, err := Parse([]string{"--agent="}, isSub); err == nil {
		t.Error("empty --agent= was accepted")
	}
}
