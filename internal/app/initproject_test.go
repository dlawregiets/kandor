package app

import (
	"os"
	"strings"
	"testing"

	"github.com/dlawregiets/kandor/internal/config"
)

func initProjectCall(t *testing.T, stdin, userConfig, projectDir string, args ...string) (int, string, string) {
	t.Helper()
	configFile(t, userConfig)
	return callIn(t, stdin, append([]string{"-C", projectDir}, args...)...)
}

func TestMainInitProject(t *testing.T) {
	project := t.TempDir()
	code, _, errs := initProjectCall(t, "y\nn\n",
		"[sections.go.mounts]\nread_write = [\"~/go\"]\n[sections.python.mounts]\nread_write = [\"~/.cache/uv\"]\n",
		project, "--init-project")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	content, err := os.ReadFile(config.ProjectPath(project))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `include = ["go"]`) {
		t.Errorf("project file does not include go:\n%s", content)
	}
	if !strings.Contains(errs, "kandor: created "+config.ProjectPath(project)) {
		t.Errorf("stderr = %q", errs)
	}
}

func TestMainInitProjectRefusesOverwrite(t *testing.T) {
	project := t.TempDir()
	existing := config.ProjectPath(project)
	if err := os.WriteFile(existing, []byte("include = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := initProjectCall(t, "y\n", "[sections.go]\n", project, "--init-project")
	if code != 1 || !strings.Contains(errs, "project configuration already exists: "+existing) {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
	if content, _ := os.ReadFile(existing); string(content) != "include = []\n" {
		t.Error("--init-project overwrote an existing file")
	}
}

func TestMainInitProjectNoSections(t *testing.T) {
	project := t.TempDir()
	code, _, errs := initProjectCall(t, "", "", project, "--init-project")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	content, err := os.ReadFile(config.ProjectPath(project))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "include = []") {
		t.Errorf("project file has no empty include list:\n%s", content)
	}
}

func TestMainInitProjectSkipsDefaultSection(t *testing.T) {
	project := t.TempDir()
	code, _, errs := initProjectCall(t, "", "[sections.default.mounts]\nread_write = [\"~/.cache/go-build\"]\n",
		project, "--init-project")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	if strings.Contains(errs, `include "default"`) {
		t.Errorf("--init-project prompted for the default section: %q", errs)
	}
	content, err := os.ReadFile(config.ProjectPath(project))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "include = []") {
		t.Errorf("project file includes the default section:\n%s", content)
	}
}

func TestMainInitProjectBadDir(t *testing.T) {
	configFile(t, "")
	if code, _, errs := callIn(t, "", "-C", "/", "--init-project"); code != 1 ||
		!strings.Contains(errs, "refusing to use / as the project directory") {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
}

func TestDescribeSection(t *testing.T) {
	empty := describeSection(config.Section{})
	if empty != "no settings" {
		t.Errorf("empty = %q", empty)
	}
	full := describeSection(config.Section{
		Mounts: config.MountLists{ReadOnly: []string{"a"}, ReadWrite: []string{"b", "c"}},
		Env:    config.EnvSection{Set: map[string]string{"A": "1"}},
	})
	if full != "3 mounts, 1 environment variable" {
		t.Errorf("full = %q", full)
	}
	other := describeSection(config.Section{Sandbox: config.SandboxSection{Network: ptr(false)}})
	if other != "other settings" {
		t.Errorf("other = %q", other)
	}
}

func ptr[T any](v T) *T { return &v }
