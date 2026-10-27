package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadErr(t *testing.T, content string) (string, error) {
	t.Helper()
	path := writeFile(t, content)
	_, err := Load(path)
	if err == nil {
		t.Fatalf("Load(%q) succeeded, want error", content)
	}
	return path, err
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("Load(missing) error: %v", err)
	}
	if f == nil || f.Sandbox.Network != nil || len(f.Mounts.ReadOnly) != 0 {
		t.Fatalf("Load(missing) = %+v, want empty File", f)
	}
}

const fullSchema = `
[mounts]
read_only  = ["~/ro"]
read_write = ["~/rw"]

[env]
pass = ["PASSED"]

[env.set]
GOPROXY = "direct"

[sandbox]
network      = false
allow_userns = true
new_session  = true

[credentials]
ssh             = true
ssh_agent       = true
git_credentials = true
netrc           = true
npmrc           = true

[git]
mount_config = false
protect      = false

[profile]
isolated = true
name     = "work"
seed     = false

[agents.opencode]
binary = "/opt/opencode"

[agents.opencode.mounts]
read_only  = ["~/a"]
read_write = ["~/b"]

[agents.opencode.env]
pass = ["AGENT_PASS"]

[agents.opencode.env.set]
AGENT_SET = "1"
`

func TestLoadFullSchema(t *testing.T) {
	f, err := Load(writeFile(t, fullSchema))
	if err != nil {
		t.Fatalf("Load(full schema) error: %v", err)
	}
	if f.Sandbox.Network == nil || *f.Sandbox.Network {
		t.Error("sandbox.network not decoded as false")
	}
	if f.Sandbox.NewSession == nil || *f.Sandbox.NewSession != SessionOn {
		t.Error("sandbox.new_session = true not decoded as SessionOn")
	}
	if f.Mounts.ReadWrite[0] != "~/rw" || f.Env.Set["GOPROXY"] != "direct" || f.Env.Pass[0] != "PASSED" {
		t.Errorf("mounts/env not decoded: %+v %+v", f.Mounts, f.Env)
	}
	if f.Credentials.Npmrc == nil || !*f.Credentials.Npmrc || f.Git.Protect == nil || *f.Git.Protect {
		t.Error("credentials/git not decoded")
	}
	if f.Profile.Name == nil || *f.Profile.Name != "work" || f.Profile.Seed == nil || *f.Profile.Seed {
		t.Error("profile not decoded")
	}
	oc, ok := f.Agents["opencode"]
	if !ok || oc.Binary == nil || *oc.Binary != "/opt/opencode" || oc.Mounts.ReadWrite[0] != "~/b" || oc.Env.Set["AGENT_SET"] != "1" {
		t.Errorf("agents.opencode not decoded: %+v", oc)
	}

	f, err = Load(writeFile(t, "[sandbox]\nnew_session = \"auto\"\n"))
	if err != nil || f.Sandbox.NewSession == nil || *f.Sandbox.NewSession != SessionAuto {
		t.Errorf("new_session = \"auto\" → %v, %v; want SessionAuto", f.Sandbox.NewSession, err)
	}
	f, err = Load(writeFile(t, "[sandbox]\nnew_session = false\n"))
	if err != nil || *f.Sandbox.NewSession != SessionOff {
		t.Errorf("new_session = false → %v; want SessionOff", err)
	}
}

func TestLoadUnknownKey(t *testing.T) {
	path, err := loadErr(t, "[sandbox]\nnetwrok = false\n")
	if !strings.Contains(err.Error(), `unknown setting "sandbox.netwrok"`) || !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q", err)
	}
}

func TestLoadUnknownSection(t *testing.T) {
	_, err := loadErr(t, "[mount]\nread_only = []\n")
	if !strings.Contains(err.Error(), `"mount`) {
		t.Errorf("error = %q, want it to name the section", err)
	}
}

func TestLoadWrongType(t *testing.T) {
	path, err := loadErr(t, "[sandbox]\nnetwork = \"no\"\n")
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error = %q, want path and line 2", err)
	}
}

func TestLoadBadNewSession(t *testing.T) {
	_, err := loadErr(t, "[sandbox]\nnew_session = \"sometimes\"\n")
	if !strings.Contains(err.Error(), "new_session") && !strings.Contains(err.Error(), "sometimes") {
		t.Errorf("error = %q", err)
	}
}

func TestLoadUnknownAgent(t *testing.T) {
	_, err := loadErr(t, "[agents.pi]\nbinary = \"/x\"\n")
	if !strings.Contains(err.Error(), `unknown agent "pi"`) {
		t.Errorf("error = %q", err)
	}
}

func TestLoadInvalidEnvName(t *testing.T) {
	_, err := loadErr(t, "[env.set]\n\"BAD-NAME\" = \"x\"\n")
	if !strings.Contains(err.Error(), "invalid environment variable name: BAD-NAME") {
		t.Errorf("error = %q", err)
	}
	_, err = loadErr(t, "[agents.opencode.env.set]\n\"1X\" = \"x\"\n")
	if !strings.Contains(err.Error(), "invalid environment variable name: 1X") {
		t.Errorf("agent env error = %q", err)
	}
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLocate(t *testing.T) {
	loc := Locate(env(map[string]string{"HOME": "/h", "KANDOR_CONFIG": "/etc/k.toml"}))
	if loc.TOML != "/etc/k.toml" || !loc.Explicit || loc.Legacy != "/h/.config/kandor/config" {
		t.Errorf("explicit Locate = %+v", loc)
	}
	loc = Locate(env(map[string]string{"HOME": "/h", "XDG_CONFIG_HOME": "/x"}))
	if loc.TOML != "/x/kandor/config.toml" || loc.Explicit || loc.Legacy != "/x/kandor/config" {
		t.Errorf("XDG Locate = %+v", loc)
	}
	loc = Locate(env(map[string]string{"HOME": "/h"}))
	if loc.TOML != "/h/.config/kandor/config.toml" {
		t.Errorf("default Locate = %+v", loc)
	}
}

func TestCheckLegacy(t *testing.T) {
	dir := t.TempDir()
	loc := Locations{TOML: filepath.Join(dir, "config.toml"), Legacy: filepath.Join(dir, "config")}
	if err := CheckLegacy(loc); err != nil {
		t.Errorf("neither file: %v", err)
	}
	if err := os.WriteFile(loc.Legacy, []byte("no-net\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var le *LegacyError
	if err := CheckLegacy(loc); !errors.As(err, &le) || le.Path != loc.Legacy {
		t.Errorf("legacy only: %v, want *LegacyError for %s", err, loc.Legacy)
	} else if !strings.Contains(err.Error(), "kandor --migrate-config") {
		t.Errorf("message = %q", err)
	}
	if err := os.WriteFile(loc.TOML, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckLegacy(loc); err != nil {
		t.Errorf("both files: %v, want nil", err)
	}
}
