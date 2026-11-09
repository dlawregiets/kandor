package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestProjectPath(t *testing.T) {
	if got := ProjectPath("/p"); got != filepath.Join("/p", ".kandor.toml") {
		t.Errorf("ProjectPath = %q", got)
	}
}

func TestLoadProjectMissing(t *testing.T) {
	_, ok, err := LoadProject(t.TempDir())
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}
	if ok {
		t.Error("LoadProject ok = true for a missing file")
	}
}

func TestLoadProjectPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(ProjectPath(dir), []byte("include = [\"go\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proj, ok, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}
	if !ok || !slices.Equal(proj.Include, []string{"go"}) {
		t.Errorf("LoadProject = %+v, %v", proj, ok)
	}
}

func TestLoadProjectLegacy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(ProjectPath(dir), []byte("ro /x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProject(dir); err == nil {
		t.Error("legacy project file was accepted")
	}
}

func TestWideningKeys(t *testing.T) {
	proj := &File{
		Mounts:  MountLists{ReadOnly: []string{"~/ro"}, ReadWrite: []string{"~/rw"}},
		Env:     EnvSection{Pass: []string{"TOKEN"}, Set: map[string]string{"A": "1"}},
		Sandbox: SandboxSection{Network: ptr(true)},
		Git:     GitSection{Protect: ptr(false)},
		Agents:  map[string]AgentSection{"opencode": {Binary: ptr("/x")}},
	}
	got := WideningKeys(proj, "opencode")
	want := []string{
		"mounts.read_only", "mounts.read_write", "env.pass", "env.set",
		"sandbox.network", "git.protect", "agents.opencode.binary",
	}
	if !slices.Equal(got, want) {
		t.Errorf("WideningKeys = %v, want %v", got, want)
	}
}

func TestWideningKeysNarrowingSilent(t *testing.T) {
	proj := &File{
		Include:     []string{"go"},
		Sandbox:     SandboxSection{Network: ptr(false)},
		Credentials: CredentialsSection{SSH: ptr(false)},
		Profile:     ProfileSection{Isolated: ptr(true)},
	}
	if got := WideningKeys(proj, "opencode"); len(got) != 0 {
		t.Errorf("WideningKeys = %v, want none", got)
	}
	if got := WideningKeys(&File{}, "opencode"); len(got) != 0 {
		t.Errorf("WideningKeys = %v, want none", got)
	}
}
