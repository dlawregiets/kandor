package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func call(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func configFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANDOR_CONFIG", path)
	return path
}

func TestMainVersion(t *testing.T) {
	code, out, _ := call(t, "--version")
	if code != 0 || out != "kandor 0.3.0\n" {
		t.Errorf("--version = %d, %q", code, out)
	}
}

func TestMainHelp(t *testing.T) {
	code, out, _ := call(t, "--help")
	if code != 0 || !strings.Contains(out, "Run OpenCode inside a bubblewrap sandbox.") {
		t.Errorf("--help = %d, %q", code, out)
	}
}

func TestMainUnknownConfigKey(t *testing.T) {
	configFile(t, "[sandbox]\nnope = 1\n")
	code, _, errs := call(t, "--dry-run")
	if code != 1 || !strings.Contains(errs, `kandor: error: `) || !strings.Contains(errs, `unknown setting "sandbox.nope"`) {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
}

func TestMainLegacyConfig(t *testing.T) {
	configFile(t, "no-net\n")
	code, _, errs := call(t, "--dry-run")
	if code != 1 || !strings.Contains(errs, "--migrate-config") {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
}

func TestMainInitConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k", "config.toml")
	t.Setenv("KANDOR_CONFIG", path)
	code, _, errs := call(t, "--init-config")
	if code != 0 || !strings.Contains(errs, "kandor: created "+path) {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
	if code, _, _ := call(t, "--init-config"); code != 1 {
		t.Error("--init-config overwrote an existing file")
	}
}

func TestMainMigrateConfig(t *testing.T) {
	old := filepath.Join(t.TempDir(), "old-config")
	os.WriteFile(old, []byte("no-net\n"), 0o600)
	t.Setenv("KANDOR_CONFIG", old)
	code, _, errs := call(t, "--migrate-config")
	dest := filepath.Join(filepath.Dir(old), "config.toml")
	if code != 0 || !strings.Contains(errs, "kandor: wrote "+dest) || !strings.Contains(errs, "KANDOR_CONFIG") {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
}

func TestMainBadOption(t *testing.T) {
	code, _, errs := call(t, "--ro")
	if code != 1 || errs != "kandor: error: --ro requires a path\n" {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
}

func TestMainProjectValidation(t *testing.T) {
	configFile(t, "")
	code, _, errs := call(t, "-C", "/", "--dry-run")
	if code != 1 || !strings.Contains(errs, "refusing to use / as the project directory") {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
	code, _, errs = call(t, "-C", os.Getenv("HOME"), "--dry-run")
	if code != 1 || !strings.Contains(errs, "refusing to use HOME as the project directory") {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
}

func TestMainDryRun(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	configFile(t, "[sandbox]\nnetwork = false\n")
	t.Setenv("KANDOR_BIN", "/usr/bin/true")
	t.Setenv("KANDOR_HOME", t.TempDir())
	project, _ := filepath.EvalSymlinks(t.TempDir())
	bin, _ := filepath.EvalSymlinks("/usr/bin/true")
	code, out, errs := call(t, "-C", project, "--isolated", "--dry-run", "--", "--continue")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	for _, want := range []string{"bwrap ", "--ro-bind / /", "--bind " + project + " " + project, "--unshare-net",
		"--clearenv", "-- " + bin + " --standalone --continue"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output is missing %q:\n%s", want, out)
		}
	}
}
