package config

import (
	"reflect"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func merge(t *testing.T, f *File, o Overrides, envm map[string]string) Settings {
	t.Helper()
	s, err := Merge(f, o, "opencode", "/h", "/cwd", env(envm))
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}
	return s
}

func TestMergeDefaults(t *testing.T) {
	s := merge(t, &File{}, Overrides{}, nil)
	if !s.Network || !s.ProtectGit || !s.MountGitConfig || !s.Seed {
		t.Errorf("boolean defaults wrong: %+v", s)
	}
	if s.Isolated || s.AllowUserns || s.SSH || s.SSHAgent || s.GitCredentials || s.Netrc || s.Npmrc {
		t.Errorf("opt-in settings on by default: %+v", s)
	}
	if s.Profile != "default" || s.ProjectDir != "/cwd" || s.NewSession != SessionAuto || s.AgentBinary != "" {
		t.Errorf("defaults wrong: %+v", s)
	}
}

func TestMergeCommandLineOverridesFile(t *testing.T) {
	f := &File{Sandbox: SandboxSection{Network: ptr(false)}, Credentials: CredentialsSection{SSH: ptr(true)}}
	s := merge(t, f, Overrides{Network: ptr(true), ProjectDir: "/p"}, nil)
	if !s.Network {
		t.Error("--net did not override network = false")
	}
	if !s.SSH {
		t.Error("file ssh = true lost when the command line did not set it")
	}
	if s.ProjectDir != "/p" {
		t.Errorf("ProjectDir = %q, want /p", s.ProjectDir)
	}
}

func TestMergeProfileImpliesIsolated(t *testing.T) {
	s := merge(t, &File{Profile: ProfileSection{Name: ptr("work")}}, Overrides{}, nil)
	if !s.Isolated || s.Profile != "work" {
		t.Errorf("profile.name did not imply isolated: %+v", s)
	}
	s = merge(t, &File{Profile: ProfileSection{Name: ptr("work"), Isolated: ptr(false)}}, Overrides{}, nil)
	if s.Isolated {
		t.Error("explicit profile.isolated = false was overridden by profile.name")
	}
	s = merge(t, &File{Profile: ProfileSection{Name: ptr("work")}}, Overrides{Isolated: ptr(false)}, nil)
	if s.Isolated {
		t.Error("--shared did not override the file's profile")
	}
}

func TestMergeMountOrder(t *testing.T) {
	f := &File{
		Mounts: MountLists{ReadOnly: []string{"~/a"}, ReadWrite: []string{"~/b"}},
		Agents: map[string]AgentSection{"opencode": {Mounts: MountLists{ReadOnly: []string{"~/c"}}}},
	}
	s := merge(t, f, Overrides{Mounts: []MountSpec{{Path: "~/a", Writable: true}}}, nil)
	want := []MountSpec{{"/h/a", false}, {"/h/b", true}, {"/h/c", false}, {"/h/a", true}}
	if !reflect.DeepEqual(s.Mounts, want) {
		t.Errorf("Mounts = %+v, want %+v", s.Mounts, want)
	}
}

func TestMergeSameLayerConflict(t *testing.T) {
	f := &File{Mounts: MountLists{ReadOnly: []string{"~/a"}, ReadWrite: []string{"$HOME/a"}}}
	_, err := Merge(f, Overrides{}, "opencode", "/h", "/cwd", env(nil))
	if err == nil || !strings.Contains(err.Error(), `"/h/a" is listed as both read_only and read_write`) {
		t.Errorf("err = %v", err)
	}
}

func TestMergeCommandLineConflict(t *testing.T) {
	o := Overrides{Mounts: []MountSpec{{Path: "/x", Writable: false}, {Path: "/x", Writable: true}}}
	if _, err := Merge(&File{}, o, "opencode", "/h", "/cwd", env(nil)); err == nil {
		t.Error("--ro X --rw X on one command line was accepted")
	}
}

func TestMergeEnvOrder(t *testing.T) {
	f := &File{
		Env:    EnvSection{Pass: []string{"P"}, Set: map[string]string{"B": "2", "A": "1"}},
		Agents: map[string]AgentSection{"opencode": {Env: EnvSection{Pass: []string{"Q"}, Set: map[string]string{"Z": "9"}}}},
	}
	s := merge(t, f, Overrides{Env: []EnvSpec{{Name: "C", Value: "3", HasValue: true}}}, nil)
	want := []EnvSpec{{"P", "", false}, {"A", "1", true}, {"B", "2", true}, {"Q", "", false}, {"Z", "9", true}, {"C", "3", true}}
	if !reflect.DeepEqual(s.Env, want) {
		t.Errorf("Env = %+v, want %+v", s.Env, want)
	}
}

func TestMergeAgentBinary(t *testing.T) {
	f := &File{Agents: map[string]AgentSection{"opencode": {Binary: ptr("/from/file")}}}
	if s := merge(t, f, Overrides{}, nil); s.AgentBinary != "/from/file" {
		t.Errorf("AgentBinary = %q, want /from/file", s.AgentBinary)
	}
	if s := merge(t, f, Overrides{}, map[string]string{"KANDOR_BIN": "/from/env"}); s.AgentBinary != "/from/env" {
		t.Errorf("AgentBinary = %q, want KANDOR_BIN", s.AgentBinary)
	}
}

func TestMergeInvalidProfile(t *testing.T) {
	_, err := Merge(&File{}, Overrides{Profile: "../x"}, "opencode", "/h", "/cwd", env(nil))
	if err == nil || !strings.Contains(err.Error(), "invalid profile name '../x'") {
		t.Errorf("err = %v", err)
	}
}
