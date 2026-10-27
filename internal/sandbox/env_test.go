package sandbox

import (
	"slices"
	"strings"
	"testing"

	"github.com/dlawregiets/kandor/internal/agent"
	"github.com/dlawregiets/kandor/internal/config"
)

func envInput(environ ...string) (EnvInput, *[]string) {
	warns := &[]string{}
	return EnvInput{
		Environ:    environ,
		Home:       "/h",
		RuntimeDir: "/run/user/1000",
		Layout:     agent.Layout{Config: "/sb/config", Data: "/h/.local/share", State: "/sb/state", Cache: "/sb/cache"},
		AgentEnv:   []agent.EnvVar{{Name: "OPENCODE_DISABLE_AUTOUPDATE", Value: "1"}},
		Exclude:    agent.OpenCode.ExcludeEnv,
		Warn:       func(s string) { *warns = append(*warns, s) },
	}, warns
}

func environment(t *testing.T, in EnvInput) map[string]string {
	t.Helper()
	vars, err := Environment(in)
	if err != nil {
		t.Fatalf("Environment error: %v", err)
	}
	m := map[string]string{}
	for _, v := range vars {
		m[v.Name] = v.Value
	}
	return m
}

func TestEnvProviderAllowlist(t *testing.T) {
	in, _ := envInput("ANTHROPIC_API_KEY=a", "STRIPE_SECRET_KEY=s", "OPENCODE_SERVER_PASSWORD=p",
		"HTTPS_PROXY=http://proxy", "DBUS_SESSION_BUS_ADDRESS=unix:x", "RANDOM_VAR=r", "GIT_EDITOR=vim")
	m := environment(t, in)
	if m["ANTHROPIC_API_KEY"] != "a" || m["HTTPS_PROXY"] != "http://proxy" || m["GIT_EDITOR"] != "vim" {
		t.Errorf("allowed variables missing: %v", m)
	}
	for _, name := range []string{"STRIPE_SECRET_KEY", "OPENCODE_SERVER_PASSWORD", "DBUS_SESSION_BUS_ADDRESS", "RANDOM_VAR"} {
		if _, ok := m[name]; ok {
			t.Errorf("%s was forwarded", name)
		}
	}
}

func TestEnvFixedAndXDG(t *testing.T) {
	in, _ := envInput("USER=u", "PATH=/opt/bin", "LANG=de_DE.UTF-8", "COLORTERM=truecolor")
	m := environment(t, in)
	want := map[string]string{
		"HOME": "/h", "USER": "u", "LOGNAME": "u", "TMPDIR": "/tmp", "HOSTNAME": "kandor",
		"TERM": "xterm-256color", "SHELL": "/bin/bash", "PATH": "/usr/bin:/bin:/opt/bin", "LANG": "de_DE.UTF-8",
		"XDG_CONFIG_HOME": "/sb/config", "XDG_DATA_HOME": "/h/.local/share", "XDG_STATE_HOME": "/sb/state",
		"XDG_CACHE_HOME": "/sb/cache", "XDG_RUNTIME_DIR": "/run/user/1000",
		"OPENCODE_DISABLE_AUTOUPDATE": "1", "COLORTERM": "truecolor",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
	in, _ = envInput()
	if m := environment(t, in); m["LANG"] != "C.UTF-8" || m["PATH"] != "/usr/local/bin:/usr/bin:/bin" {
		t.Errorf("fallbacks: LANG=%q PATH=%q", m["LANG"], m["PATH"])
	}
}

func TestEnvSSHAuthSock(t *testing.T) {
	in, _ := envInput("SSH_AUTH_SOCK=/host/agent.sock")
	if _, ok := environment(t, in)["SSH_AUTH_SOCK"]; ok {
		t.Error("SSH_AUTH_SOCK forwarded without --ssh-agent")
	}
	in.SSHAuthSock = "/resolved/agent.sock"
	if environment(t, in)["SSH_AUTH_SOCK"] != "/resolved/agent.sock" {
		t.Error("resolved SSH_AUTH_SOCK not set")
	}
}

func TestEnvExtraOverridesGitConfig(t *testing.T) {
	in, _ := envInput()
	in.GitConfigGlobal = "/h/.config/git/config"
	in.Extra = []config.EnvSpec{{Name: "GIT_CONFIG_GLOBAL", Value: "x", HasValue: true}}
	vars, err := Environment(in)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(vars, func(v agent.EnvVar) bool { return v.Name == "GIT_CONFIG_GLOBAL" && v.Value == "/h/.config/git/config" })
	j := slices.IndexFunc(vars, func(v agent.EnvVar) bool { return v.Name == "GIT_CONFIG_GLOBAL" && v.Value == "x" })
	if i < 0 || j < 0 || i > j {
		t.Errorf("want the configured GIT_CONFIG_GLOBAL before --env's: %v", vars)
	}
}

func TestEnvExtraFromHost(t *testing.T) {
	in, _ := envInput("MY_TOKEN=t", "EMPTY=")
	in.Extra = []config.EnvSpec{{Name: "MY_TOKEN"}, {Name: "EMPTY"}}
	m := environment(t, in)
	if m["MY_TOKEN"] != "t" {
		t.Errorf("MY_TOKEN = %q", m["MY_TOKEN"])
	}
	if v, ok := m["EMPTY"]; !ok || v != "" {
		t.Error("a set-but-empty variable was skipped")
	}
}

func TestEnvExtraUnsetSkipped(t *testing.T) {
	in, warns := envInput()
	in.Extra = []config.EnvSpec{{Name: "NOPE"}}
	if _, ok := environment(t, in)["NOPE"]; ok {
		t.Error("unset variable forwarded")
	}
	if len(*warns) != 1 || (*warns)[0] != "environment variable NOPE is not set; skipping" {
		t.Errorf("warnings = %q", *warns)
	}
}

func TestEnvInvalidName(t *testing.T) {
	in, _ := envInput()
	in.Extra = []config.EnvSpec{{Name: "BAD-NAME", Value: "x", HasValue: true}}
	if _, err := Environment(in); err == nil || err.Error() != "invalid environment variable name: BAD-NAME" {
		t.Errorf("err = %v", err)
	}
}

func TestEnvAllowlistSorted(t *testing.T) {
	in, _ := envInput("OPENAI_B=2", "ANTHROPIC_A=1")
	vars, _ := Environment(in)
	a := slices.IndexFunc(vars, func(v agent.EnvVar) bool { return v.Name == "ANTHROPIC_A" })
	b := slices.IndexFunc(vars, func(v agent.EnvVar) bool { return v.Name == "OPENAI_B" })
	if a < 0 || b < 0 || a > b {
		t.Errorf("allowlisted variables not sorted: %v", vars)
	}
}

func TestFormatRedactsSecrets(t *testing.T) {
	out := FormatCommand([]string{"MY_TOKEN=s3cret", "HOME=/h"}, []string{"bwrap", "--"})
	if strings.Contains(out, "s3cret") || !strings.Contains(out, "MY_TOKEN=<redacted>") || !strings.Contains(out, "HOME=/h") {
		t.Errorf("FormatCommand = %q", out)
	}
	out = FormatCommand([]string{"AWS_REGION=eu-west-1", "SSH_AUTH_SOCK=/s"}, nil)
	if strings.Contains(out, "eu-west-1") || strings.Contains(out, "=/s") {
		t.Errorf("AWS_* and SSH_AUTH_SOCK not redacted: %q", out)
	}
}

func TestFormatRedactsURLCredentials(t *testing.T) {
	out := FormatCommand([]string{"HTTPS_PROXY=http://u:pw@p:3128"}, nil)
	if strings.Contains(out, "pw") || !strings.Contains(out, "@p:3128") {
		t.Errorf("FormatCommand = %q", out)
	}
}

// The sandbox environment is shown as an "env -i" prefix, so the dry-run
// output is still a command that reproduces the run.
func TestFormatEnvPrefix(t *testing.T) {
	out := FormatCommand([]string{"HOME=/h", "A=x y"}, []string{"bwrap", "--"})
	if out != "env -i HOME=/h 'A=x y' bwrap --" {
		t.Errorf("FormatCommand = %q", out)
	}
	if out := FormatCommand(nil, []string{"bwrap"}); out != "bwrap" {
		t.Errorf("FormatCommand without environment = %q", out)
	}
}

func TestEnvListLastWins(t *testing.T) {
	got := EnvList([]agent.EnvVar{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}, {Name: "A", Value: "3"}})
	if !slices.Equal(got, []string{"A=3", "B=2"}) {
		t.Errorf("EnvList = %q", got)
	}
}

func TestFormatQuotesSpaces(t *testing.T) {
	if out := FormatCommand(nil, []string{"--bind", "/a b", "/a b"}); out != "--bind '/a b' '/a b'" {
		t.Errorf("FormatCommand = %q", out)
	}
	if out := FormatCommand(nil, []string{"it's"}); out != `'it'\''s'` {
		t.Errorf("FormatCommand = %q", out)
	}
	if out := FormatCommand(nil, []string{""}); out != "''" {
		t.Errorf("FormatCommand(empty) = %q", out)
	}
}

func TestFormatPlainWordsBare(t *testing.T) {
	if out := FormatCommand(nil, []string{"--ro-bind", "/", "/", "--chmod", "1777", "/home/x/proj"}); out != "--ro-bind / / --chmod 1777 /home/x/proj" {
		t.Errorf("FormatCommand = %q", out)
	}
}
