package config

import (
	"reflect"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func merge(t *testing.T, f *File, o Overrides, envm map[string]string) Settings {
	t.Helper()
	return mergeProj(t, f, nil, o, envm)
}

func mergeProj(t *testing.T, user, proj *File, o Overrides, envm map[string]string) Settings {
	t.Helper()
	s, err := Merge(user, proj, o, "opencode", "/h", "/cwd", env(envm))
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
	_, err := Merge(f, nil, Overrides{}, "opencode", "/h", "/cwd", env(nil))
	if err == nil || !strings.Contains(err.Error(), `"/h/a" is listed as both read_only and read_write`) {
		t.Errorf("err = %v", err)
	}
}

func TestMergeCommandLineConflict(t *testing.T) {
	o := Overrides{Mounts: []MountSpec{{Path: "/x", Writable: false}, {Path: "/x", Writable: true}}}
	if _, err := Merge(&File{}, nil, o, "opencode", "/h", "/cwd", env(nil)); err == nil {
		t.Error("--ro X --rw X on one command line was accepted")
	}
}

func TestMergeEnvOrder(t *testing.T) {
	f := &File{
		Env:    EnvSection{Pass: []string{"P"}, Set: map[string]string{"B": "2", "A": "1"}},
		Agents: map[string]AgentSection{"opencode": {Env: AgentEnv{Pass: []string{"Q"}, Set: map[string]string{"Z": "9"}}}},
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
	_, err := Merge(&File{}, nil, Overrides{Profile: "../x"}, "opencode", "/h", "/cwd", env(nil))
	if err == nil || !strings.Contains(err.Error(), "invalid profile name '../x'") {
		t.Errorf("err = %v", err)
	}
}

func TestMergeDefaultSectionAlwaysIncluded(t *testing.T) {
	user := &File{
		Env: EnvSection{Set: map[string]string{"A": "top"}},
		Sections: map[string]Section{
			"default": {Env: EnvSection{Set: map[string]string{"A": "default", "B": "default"}}},
			"go":      {Env: EnvSection{Set: map[string]string{"C": "go"}}},
		},
	}
	s := mergeProj(t, user, &File{}, Overrides{}, nil)
	want := []EnvSpec{{"A", "top", true}, {"A", "default", true}, {"B", "default", true}}
	// The "go" section is not included, so C is absent.
	if !reflect.DeepEqual(s.Env, want) {
		t.Errorf("Env = %+v, want %+v", s.Env, want)
	}
}

func TestMergeIncludeOrder(t *testing.T) {
	user := &File{
		Env:     EnvSection{Set: map[string]string{"A": "top"}},
		Include: []string{"go"},
		Sections: map[string]Section{
			"default": {Env: EnvSection{Set: map[string]string{"A": "default"}}},
			"go":      {Env: EnvSection{Set: map[string]string{"A": "go-user"}}},
			"python":  {Env: EnvSection{Set: map[string]string{"A": "python"}}},
		},
	}
	proj := &File{
		Include: []string{"python", "go"},
		Env:     EnvSection{Set: map[string]string{"A": "project"}},
	}
	s := mergeProj(t, user, proj, Overrides{Env: []EnvSpec{{Name: "A", Value: "cli", HasValue: true}}}, nil)
	var got []string
	for _, e := range s.Env {
		got = append(got, e.Value)
	}
	want := []string{"top", "default", "go-user", "python", "project", "cli"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Env order = %v, want %v", got, want)
	}
}

func TestMergeUnknownSection(t *testing.T) {
	user := &File{Sections: map[string]Section{"go": {}}}
	proj := &File{Include: []string{"rust"}}
	_, err := Merge(user, proj, Overrides{}, "opencode", "/h", "/cwd", env(nil))
	if err == nil || !strings.Contains(err.Error(), `unknown section "rust" in the project file's include list`) {
		t.Errorf("err = %v", err)
	}
	_, err = Merge(&File{Include: []string{"rust"}}, nil, Overrides{}, "opencode", "/h", "/cwd", env(nil))
	if err == nil || !strings.Contains(err.Error(), `unknown section "rust" in the user file's include list`) {
		t.Errorf("err = %v", err)
	}
}

func TestMergeProjectSectionsRejected(t *testing.T) {
	proj := &File{Sections: map[string]Section{"go": {}}}
	_, err := Merge(&File{}, proj, Overrides{}, "opencode", "/h", "/cwd", env(nil))
	if err == nil || !strings.Contains(err.Error(), "sections must be defined in the user configuration file") {
		t.Errorf("err = %v", err)
	}
}

func TestMergeSectionAgentLayer(t *testing.T) {
	user := &File{
		Mounts: MountLists{ReadOnly: []string{"~/top"}},
		Agents: map[string]AgentSection{"opencode": {Mounts: MountLists{ReadOnly: []string{"~/agent"}}}},
		Sections: map[string]Section{
			"go": {
				Mounts: MountLists{ReadOnly: []string{"~/go"}},
				Agents: map[string]AgentSection{"opencode": {Mounts: MountLists{ReadOnly: []string{"~/go-agent"}}}},
			},
		},
	}
	proj := &File{
		Include: []string{"go"},
		Mounts:  MountLists{ReadOnly: []string{"~/proj"}},
	}
	s := mergeProj(t, user, proj, Overrides{}, nil)
	var got []string
	for _, m := range s.Mounts {
		got = append(got, m.Path)
	}
	// Top layers first (user, section, project), then agent layers in the
	// same order, so agent settings keep beating plain ones.
	want := []string{"/h/top", "/h/go", "/h/proj", "/h/agent", "/h/go-agent"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Mounts = %v, want %v", got, want)
	}
}

func TestMergeSectionPathsFoldIntoAgent(t *testing.T) {
	user := &File{
		Sections: map[string]Section{
			"go": {Paths: AgentPaths{Share: []string{"~/go"}}},
		},
		Agents: map[string]AgentSection{
			"opencode": {Paths: AgentPaths{RO: []string{"~/ro"}}},
		},
	}
	proj := &File{Include: []string{"go"}}
	if _, err := Merge(user, proj, Overrides{}, "opencode", "/h", "/cwd", env(nil)); err != nil {
		t.Fatalf("section paths rejected: %v", err)
	}
	// A section listing one path under two modes is an error, like mounts.
	user.Sections["go"] = Section{
		Paths: AgentPaths{Share: []string{"~/x"}},
		Agents: map[string]AgentSection{
			"opencode": {Paths: AgentPaths{Copy: []string{"$HOME/x"}}},
		},
	}
	_, err := Merge(user, proj, Overrides{}, "opencode", "/h", "/cwd", env(nil))
	if err == nil || !strings.Contains(err.Error(), `agents.opencode: "/h/x" is listed in both share and copy`) {
		t.Errorf("err = %v", err)
	}
}

func TestMergeAgentPathErrors(t *testing.T) {
	user := &File{Agents: map[string]AgentSection{"pi": {Paths: AgentPaths{Share: []string{""}}}}}
	if _, err := Merge(user, nil, Overrides{}, "pi", "/h", "/cwd", env(nil)); err == nil ||
		!strings.Contains(err.Error(), "agents.pi: empty path in share") {
		t.Errorf("err = %v", err)
	}
	proj := &File{DefaultAgent: "pi"}
	if _, err := Merge(&File{}, proj, Overrides{}, "pi", "/h", "/cwd", env(nil)); err == nil ||
		!strings.Contains(err.Error(), "default_agent may only be set in the user configuration file") {
		t.Errorf("err = %v", err)
	}
}

func TestMergeAgentSectionAcrossLayers(t *testing.T) {
	user := &File{
		Agents: map[string]AgentSection{
			"pi": {Binary: ptr("/usr/bin/pi"), Env: AgentEnv{Exclude: []string{"A"}}},
		},
		Sections: map[string]Section{
			"default": {
				Agents: map[string]AgentSection{
					"pi": {Paths: AgentPaths{Share: []string{"~/s1"}}, Env: AgentEnv{Exclude: []string{"A", "B"}}},
				},
			},
		},
		Include: []string{"extra"},
	}
	user.Sections["extra"] = Section{
		Agents: map[string]AgentSection{
			"pi": {Binary: ptr("/opt/pi"), Paths: AgentPaths{RO: []string{"~/ro"}}},
		},
	}
	s, err := Merge(user, &File{}, Overrides{}, "pi", "/h", "/cwd", env(nil))
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}
	if s.Agent.Binary == nil || *s.Agent.Binary != "/opt/pi" {
		t.Errorf("Agent.Binary = %+v, want last-wins /opt/pi", s.Agent.Binary)
	}
	if len(s.Agent.Paths.Share) != 1 || len(s.Agent.Paths.RO) != 1 {
		t.Errorf("Agent.Paths = %+v", s.Agent.Paths)
	}
	var got []string
	for _, e := range s.Agent.Env.Exclude {
		got = append(got, e)
	}
	if len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Errorf("Agent.Env.Exclude = %v", got)
	}
	// Other agents are untouched.
	if len(s.Agent.Subcommands) != 0 {
		t.Errorf("Agent.Subcommands = %v", s.Agent.Subcommands)
	}
}
