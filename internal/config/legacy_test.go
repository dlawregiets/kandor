package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIsLegacy(t *testing.T) {
	cases := map[string]bool{
		"# comment\n\nro ~/x\n": true,
		"no-net\n":              true,
		"  agent opencode\n":    true,
		"[mounts]\n":            false,
		"# only a comment\n":    false,
		"":                      false,
		"network = false\n":     false,
	}
	for in, want := range cases {
		if got := IsLegacy([]byte(in)); got != want {
			t.Errorf("IsLegacy(%q) = %v, want %v", in, got, want)
		}
	}
}

const legacySample = "# settings\r\nro ~/a\nrw ~/b # trailing comment\nrw ~/dir#with-hash\nenv X=1\nenv Y\nno-net\nssh\nno-protect-git\nnew-session\nprofile work\nagent opencode\n"

func TestConvertLegacy(t *testing.T) {
	f, err := ConvertLegacy([]byte(legacySample))
	if err != nil {
		t.Fatalf("ConvertLegacy error: %v", err)
	}
	if !reflect.DeepEqual(f.Mounts.ReadOnly, []string{"~/a"}) || !reflect.DeepEqual(f.Mounts.ReadWrite, []string{"~/b", "~/dir#with-hash"}) {
		t.Errorf("mounts = %+v", f.Mounts)
	}
	if !reflect.DeepEqual(f.Env.Set, map[string]string{"X": "1"}) || !reflect.DeepEqual(f.Env.Pass, []string{"Y"}) {
		t.Errorf("env = %+v", f.Env)
	}
	if f.Sandbox.Network == nil || *f.Sandbox.Network || f.Credentials.SSH == nil || !*f.Credentials.SSH ||
		f.Git.Protect == nil || *f.Git.Protect || f.Sandbox.NewSession == nil || *f.Sandbox.NewSession != SessionOn {
		t.Errorf("flags not converted: %+v", f)
	}
	if f.Profile.Name == nil || *f.Profile.Name != "work" {
		t.Errorf("profile = %+v", f.Profile)
	}
	if len(f.Agents) != 0 {
		t.Errorf("agent opencode produced agents: %+v", f.Agents)
	}
}

// The old format applied directives in order: a later "profile" isolated the
// run even after "shared", and a later "shared" undid an earlier "profile".
func TestConvertLegacyProfileOrder(t *testing.T) {
	isolated := func(content string) bool {
		t.Helper()
		f, err := ConvertLegacy([]byte(content))
		if err != nil {
			t.Fatal(err)
		}
		s, err := Merge(f, Overrides{}, "opencode", "/h", "/cwd", env(nil))
		if err != nil {
			t.Fatal(err)
		}
		return s.Isolated
	}
	if !isolated("shared\nprofile work\n") {
		t.Error(`"shared" then "profile work" migrated to shared mode`)
	}
	if isolated("profile work\nshared\n") {
		t.Error(`"profile work" then "shared" migrated to an isolated profile`)
	}
	if !isolated("isolated\nprofile work\n") {
		t.Error(`"isolated" then "profile work" is not isolated`)
	}
}

func TestConvertLegacyOtherAgent(t *testing.T) {
	_, err := ConvertLegacy([]byte("agent omp\n"))
	if err == nil || !strings.Contains(err.Error(), "only opencode is supported") {
		t.Errorf("err = %v", err)
	}
}

func TestConvertLegacyUnknownDirective(t *testing.T) {
	_, err := ConvertLegacy([]byte("ro ~/a\nbogus thing\n"))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("err = %v", err)
	}
}

func TestMigrateWritesTOMLBesideOld(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "config")
	if err := os.WriteFile(old, []byte(legacySample), 0o600); err != nil {
		t.Fatal(err)
	}
	dest, err := Migrate(Locations{TOML: filepath.Join(dir, "config.toml"), Legacy: old})
	if err != nil {
		t.Fatalf("Migrate error: %v", err)
	}
	if dest != filepath.Join(dir, "config.toml") {
		t.Errorf("dest = %q", dest)
	}
	got, err := Load(dest)
	if err != nil {
		t.Fatalf("Load(migrated) error: %v", err)
	}
	want, _ := ConvertLegacy([]byte(legacySample))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("migrated file = %+v, want %+v", got, want)
	}
	if st, _ := os.Stat(dest); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", st.Mode().Perm())
	}
	if data, _ := os.ReadFile(old); string(data) != legacySample {
		t.Error("old file was modified")
	}
}

func TestMigrateRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	old, dest := filepath.Join(dir, "config"), filepath.Join(dir, "config.toml")
	os.WriteFile(old, []byte("no-net\n"), 0o600)
	os.WriteFile(dest, []byte("# mine\n"), 0o600)
	if _, err := Migrate(Locations{TOML: dest, Legacy: old}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v", err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "# mine\n" {
		t.Error("existing config.toml was overwritten")
	}
}

func TestMigrateExplicitLegacyPath(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old")
	os.WriteFile(old, []byte("no-net\n"), 0o600)
	dest, err := Migrate(Locations{TOML: old, Legacy: filepath.Join(t.TempDir(), "config"), Explicit: true})
	if err != nil || dest != filepath.Join(dir, "config.toml") {
		t.Fatalf("Migrate = %q, %v", dest, err)
	}
}

func TestMigrateNothingToMigrate(t *testing.T) {
	dir := t.TempDir()
	if _, err := Migrate(Locations{TOML: filepath.Join(dir, "config.toml"), Legacy: filepath.Join(dir, "config")}); err == nil {
		t.Error("Migrate with no old file succeeded")
	}
}

func TestLoadReportsLegacy(t *testing.T) {
	path := writeFile(t, "ro ~/a\nno-net\n")
	_, err := Load(path)
	var le *LegacyError
	if !errors.As(err, &le) || le.Path != path {
		t.Errorf("err = %v, want *LegacyError", err)
	}
}
