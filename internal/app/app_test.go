package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dlawregiets/kandor/internal/config"
	"github.com/dlawregiets/kandor/internal/sandbox"
)

func call(t *testing.T, args ...string) (int, string, string) {
	return callIn(t, "", args...)
}

func callIn(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
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

func requireBwrap(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	probe := exec.Command("bwrap", "--ro-bind", "/", "/", "--unshare-user-try", "--", "/bin/true")
	if err := probe.Run(); err != nil {
		t.Skipf("bwrap cannot create a user namespace here: %v", err)
	}
}

func TestMainVersion(t *testing.T) {
	code, out, _ := call(t, "--version")
	if code != 0 || out != "kandor "+version+"\n" {
		t.Errorf("--version = %d, %q; want kandor %s", code, out, version)
	}
}

func TestMainHelp(t *testing.T) {
	code, out, _ := call(t, "--help")
	if code != 0 || !strings.Contains(out, "Run a coding agent inside a bubblewrap sandbox.") {
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

func TestMainInitConfigAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k", "config.toml")
	t.Setenv("KANDOR_CONFIG", path)
	code, _, errs := call(t, "--init-config", "--agent", "pi")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `default_agent = "pi"`) {
		t.Errorf("--init-config --agent pi does not set default_agent:\n%s", content)
	}
}

func TestMainInitConfigUnknownAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("KANDOR_CONFIG", path)
	code, _, errs := call(t, "--init-config", "--agent", "nope")
	if code != 1 || !strings.Contains(errs, `unknown agent "nope"`) {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("--init-config wrote a config for an unknown agent")
	}
}

func TestMainInitConfigNeedsHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("KANDOR_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := t.TempDir()
	t.Chdir(dir)
	code, _, errs := call(t, "--init-config")
	if code != 1 || !strings.Contains(errs, "HOME is not set") {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
	if _, err := os.Stat(filepath.Join(dir, ".config")); err == nil {
		t.Error("--init-config wrote a relative .config without HOME")
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
	requireBwrap(t)
	configFile(t, "[sandbox]\nnetwork = false\n")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-kandor-test")
	t.Setenv("KANDOR_BIN", "/usr/bin/true")
	t.Setenv("KANDOR_HOME", t.TempDir())
	project, _ := filepath.EvalSymlinks(t.TempDir())
	bin, _ := filepath.EvalSymlinks("/usr/bin/true")
	code, out, errs := call(t, "-C", project, "--isolated", "--dry-run", "--", "--continue")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	for _, want := range []string{"env -i ", "'OPENROUTER_API_KEY=<redacted>'", " bwrap --ro-bind / /",
		"--bind " + project + " " + project, "--unshare-net", "-- " + bin + " --standalone --continue"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output is missing %q:\n%s", want, out)
		}
	}
	// Values on bwrap's command line are visible to every user through ps.
	_, bwrapArgs, _ := strings.Cut(out, " bwrap ")
	for _, leak := range []string{"--setenv", "--clearenv", "OPENROUTER_API_KEY"} {
		if strings.Contains(bwrapArgs, leak) {
			t.Errorf("bwrap's arguments contain %q:\n%s", leak, bwrapArgs)
		}
	}
}

func TestMainDryRunNeedsNoWritableTmp(t *testing.T) {
	requireBwrap(t)
	configFile(t, "")
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	project, _ := filepath.EvalSymlinks(t.TempDir())
	if code, _, errs := call(t, "-C", project, "--dry-run"); code != 0 {
		t.Errorf("agent dry-run: code = %d, stderr = %q", code, errs)
	}
	if code, _, errs := call(t, "-C", project, "--dry-run", "exec", "--", "true"); code != 0 {
		t.Errorf("exec dry-run: code = %d, stderr = %q", code, errs)
	}
}

func TestWritableMounts(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	a, b, link := filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "link")
	os.Mkdir(a, 0o755)
	os.Mkdir(b, 0o755)
	os.Symlink(a, link)
	w := writableMounts([]config.MountSpec{{Path: link, Writable: true}, {Path: b, Writable: true}, {Path: b}})
	if !w(a) || !w(link) {
		t.Error("a read-write mount (given through a symlink) is not writable")
	}
	if w(b) {
		t.Error("a later read-only mount of the same path did not win")
	}
	if w(filepath.Join(dir, "missing")) {
		t.Error("an unmounted path is writable")
	}
}

func TestConfirmReadOnlyProject(t *testing.T) {
	project, _ := filepath.EvalSymlinks(t.TempDir())
	plan := sandbox.NewPlan(sandbox.Features{})
	plan.RW(project, project)
	if err := confirmReadOnlyProject(plan, project, nil, strings.NewReader(""), &bytes.Buffer{}, false); err != nil {
		t.Fatalf("a writable project prompted: %v", err)
	}

	plan.Bind(project, project, false)
	var errb bytes.Buffer
	if err := confirmReadOnlyProject(plan, project, nil, strings.NewReader("n\n"), &errb, false); err == nil {
		t.Fatal("a read-only project started without confirmation")
	}
	for _, want := range []string{"mounted read-only", "[y/N]"} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("stderr is missing %q: %q", want, errb.String())
		}
	}
	errb.Reset()
	if err := confirmReadOnlyProject(plan, project, nil, strings.NewReader("y\n"), &errb, false); err != nil {
		t.Fatalf("confirmation was not accepted: %v", err)
	}
	if err := confirmReadOnlyProject(plan, project, []config.MountSpec{{Path: project}}, strings.NewReader(""), &bytes.Buffer{}, false); err != nil {
		t.Errorf("an explicit --ro prompted: %v", err)
	}
}

// bindSource returns the source of the bind that mounts dest.
func bindSource(out, dest string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		for i := 0; i+2 < len(fields); i++ {
			if (fields[i] == "--bind" || fields[i] == "--ro-bind") && fields[i+2] == dest {
				return fields[i+1], true
			}
		}
	}
	return "", false
}

func TestMainDryRunAgentPaths(t *testing.T) {
	requireBwrap(t)
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("KANDOR_BIN", "/usr/bin/true")
	t.Setenv("KANDOR_HOME", t.TempDir())
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("TMPDIR", tmp)
	for _, d := range []string{"goshare", "goro", "gocopy"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	configFile(t, "[agents.opencode.paths]\nshare = [\"~/goshare\"]\nro = [\"~/goro\"]\ncopy = [\"~/gocopy\"]\nprivate = [\"~/goprivate\"]\n")
	project, _ := filepath.EvalSymlinks(t.TempDir())
	code, out, errs := call(t, "-C", project, "--dry-run")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	for _, want := range []string{
		"--bind " + filepath.Join(home, "goshare") + " " + filepath.Join(home, "goshare"),
		"--ro-bind " + filepath.Join(home, "goro") + " " + filepath.Join(home, "goro"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run is missing %q:\n%s", want, out)
		}
	}
	// Copy and private state live in the temporary shared profile, not in
	// the host's cache directory.
	for _, name := range []string{"gocopy", "goprivate"} {
		dest := filepath.Join(home, name)
		src, ok := bindSource(out, dest)
		if !ok {
			t.Errorf("dry-run has no bind for %s:\n%s", dest, out)
			continue
		}
		if !strings.HasPrefix(src, tmp+string(filepath.Separator)) {
			t.Errorf("bind for %s uses %s, want a path under TMPDIR %s", dest, src, tmp)
		}
		if want := filepath.Join("cache", "opencode", "paths", name); !strings.HasSuffix(src, want) {
			t.Errorf("bind for %s uses %s, want a path ending in %s", dest, src, want)
		}
	}
	if !strings.Contains(errs, "is mounted read-write into the sandbox") {
		t.Errorf("no share warning in stderr: %q", errs)
	}
}

func TestMainUnknownAgent(t *testing.T) {
	configFile(t, "")
	code, _, errs := call(t, "--agent", "bogus", "--dry-run")
	if code != 1 || !strings.Contains(errs, `unknown agent "bogus" (known: claude, copilot, opencode, pi)`) {
		t.Errorf("code = %d, stderr = %q", code, errs)
	}
}

func TestMainAgentSelection(t *testing.T) {
	user := "[agents.pi]\nbinary = \"/usr/bin/true\"\n"
	project := t.TempDir()
	if code, _, errs := initProjectCall(t, "", user, project, "--agent", "pi", "--init-project"); code != 0 {
		t.Fatalf("--agent pi: code = %d, stderr = %q", code, errs)
	} else if _, err := os.Stat(config.ProjectPath(project)); err != nil {
		t.Fatalf("--agent pi did not write the project file: %v", err)
	}
	project = t.TempDir()
	t.Setenv("KANDOR_AGENT", "pi")
	if code, _, errs := initProjectCall(t, "", user, project, "--init-project"); code != 0 {
		t.Fatalf("KANDOR_AGENT=pi: code = %d, stderr = %q", code, errs)
	}
	project = t.TempDir()
	if code, _, errs := initProjectCall(t, "", "default_agent = \"pi\"\n"+user, project, "--init-project"); code != 0 {
		t.Fatalf("default_agent: code = %d, stderr = %q", code, errs)
	}
}

func TestResolveAgent(t *testing.T) {
	user := &config.File{DefaultAgent: "pi"}
	proj := &config.File{DefaultAgent: "copilot"}
	if got := resolveAgent("claude", proj, user); got != "claude" {
		t.Errorf("--agent selection = %q", got)
	}
	if got := resolveAgent("", proj, user); got != "copilot" {
		t.Errorf("project selection = %q", got)
	}
	t.Setenv("KANDOR_AGENT", "pi")
	if got := resolveAgent("", proj, user); got != "pi" {
		t.Errorf("KANDOR_AGENT selection = %q", got)
	}
	t.Setenv("KANDOR_AGENT", "")
	if got := resolveAgent("", &config.File{}, user); got != "pi" {
		t.Errorf("user selection = %q", got)
	}
	if got := resolveAgent("", nil, &config.File{}); got != "opencode" {
		t.Errorf("default selection = %q", got)
	}
}

func TestMainProjectDefaultAgent(t *testing.T) {
	requireBwrap(t)
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("KANDOR_BIN", "/usr/bin/true")
	t.Setenv("KANDOR_HOME", t.TempDir())
	cache, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("XDG_CACHE_HOME", cache)
	if err := os.MkdirAll(filepath.Join(home, "pistate"), 0o755); err != nil {
		t.Fatal(err)
	}
	configFile(t, "[agents.pi.paths]\nshare = [\"~/pistate\"]\n")
	project, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.WriteFile(config.ProjectPath(project), []byte("default_agent = \"pi\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := call(t, "-C", project, "--dry-run")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	want := "--bind " + filepath.Join(home, "pistate") + " " + filepath.Join(home, "pistate")
	if !strings.Contains(out, want) {
		t.Errorf("project default_agent did not select pi; missing %q in:\n%s", want, out)
	}
	if !strings.Contains(errs, `using agent "pi" from `+config.ProjectPath(project)) {
		t.Errorf("no project agent notice in stderr: %q", errs)
	}
}

func TestMainProjectDefinesAndSelectsAgent(t *testing.T) {
	requireBwrap(t)
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("KANDOR_BIN", "/usr/bin/true")
	t.Setenv("KANDOR_HOME", t.TempDir())
	cache, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("XDG_CACHE_HOME", cache)
	if err := os.MkdirAll(filepath.Join(home, "aiderstate"), 0o755); err != nil {
		t.Fatal(err)
	}
	configFile(t, "")
	project, _ := filepath.EvalSymlinks(t.TempDir())
	cfg := `default_agent = "aider"

[agents.aider]
binary = "/usr/bin/true"

[agents.aider.paths]
share = ["~/aiderstate"]
`
	if err := os.WriteFile(config.ProjectPath(project), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := call(t, "-C", project, "--dry-run")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	want := "--bind " + filepath.Join(home, "aiderstate") + " " + filepath.Join(home, "aiderstate")
	if !strings.Contains(out, want) {
		t.Errorf("project-defined agent not used; missing %q in:\n%s", want, out)
	}
	if !strings.Contains(errs, "agents.aider.binary") {
		t.Errorf("project-defined agent binary not reported: %q", errs)
	}
}

func TestMainDryRunGenericAgent(t *testing.T) {
	requireBwrap(t)
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("KANDOR_BIN", "/usr/bin/true")
	t.Setenv("KANDOR_HOME", t.TempDir())
	cache, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("XDG_CACHE_HOME", cache)
	if err := os.MkdirAll(filepath.Join(home, "pistate"), 0o755); err != nil {
		t.Fatal(err)
	}
	configFile(t, "[agents.pi.paths]\nshare = [\"~/pistate\"]\n")
	project, _ := filepath.EvalSymlinks(t.TempDir())
	code, out, errs := call(t, "--agent", "pi", "-C", project, "--dry-run")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	want := "--bind " + filepath.Join(home, "pistate") + " " + filepath.Join(home, "pistate")
	if !strings.Contains(out, want) {
		t.Errorf("dry-run is missing %q:\n%s", want, out)
	}
}

func TestMainExecValidation(t *testing.T) {
	configFile(t, "[agents.pi]\n")
	if code, _, errs := call(t, "exec"); code != 1 || !strings.Contains(errs, "exec requires a command") {
		t.Errorf("bare exec: code = %d, stderr = %q", code, errs)
	}
	if code, _, errs := call(t, "--shell", "exec", "--", "true"); code != 1 ||
		!strings.Contains(errs, "--shell and exec are mutually exclusive") {
		t.Errorf("--shell exec: code = %d, stderr = %q", code, errs)
	}
	if code, _, errs := call(t, "--agent", "pi", "exec", "--", "true"); code != 1 ||
		!strings.Contains(errs, "exec does not take --agent") {
		t.Errorf("--agent exec: code = %d, stderr = %q", code, errs)
	}
	if code, _, errs := call(t, "--isolated", "exec", "--", "true"); code != 1 ||
		!strings.Contains(errs, "exec does not take --isolated, --profile or --no-seed") {
		t.Errorf("--isolated exec: code = %d, stderr = %q", code, errs)
	}
	if code, _, errs := call(t, "--init-project", "exec", "--", "true"); code != 1 ||
		!strings.Contains(errs, "exec and --init-project are mutually exclusive") {
		t.Errorf("init-project exec: code = %d, stderr = %q", code, errs)
	}
	if code, _, errs := call(t, "--init-project", "--shell"); code != 1 ||
		!strings.Contains(errs, "--shell and --init-project are mutually exclusive") {
		t.Errorf("init-project --shell: code = %d, stderr = %q", code, errs)
	}
	if code, _, errs := call(t, "--init-project", "--dry-run"); code != 1 ||
		!strings.Contains(errs, "--init-project and --dry-run are mutually exclusive") {
		t.Errorf("init-project --dry-run: code = %d, stderr = %q", code, errs)
	}
}

func TestMainExecIgnoresAmbientAgent(t *testing.T) {
	requireBwrap(t)
	configFile(t, "default_agent = \"aider\"\n")
	t.Setenv("KANDOR_AGENT", "aider")
	project, _ := filepath.EvalSymlinks(t.TempDir())
	code, out, errs := call(t, "-C", project, "--dry-run", "exec", "--", "true")
	if code != 0 || !strings.Contains(out, " bwrap ") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, out, errs)
	}
}

func TestMainExecDoesNotMountAgentTools(t *testing.T) {
	requireBwrap(t)
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", home)
	agentBin := filepath.Join(home, ".opencode", "bin")
	if err := os.MkdirAll(agentBin, 0o755); err != nil {
		t.Fatal(err)
	}
	configFile(t, "")
	project, _ := filepath.EvalSymlinks(t.TempDir())
	code, out, errs := call(t, "-C", project, "--dry-run", "exec", "--", "true")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	if strings.Contains(out, "--ro-bind "+agentBin+" "+agentBin) {
		t.Errorf("exec mounted the agent's tool directory:\n%s", out)
	}
}

func TestMainInitProjectIgnoresAmbientAgent(t *testing.T) {
	configFile(t, "")
	t.Setenv("KANDOR_AGENT", "bogus")
	project, _ := filepath.EvalSymlinks(t.TempDir())
	code, _, errs := call(t, "-C", project, "--init-project")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	if _, err := os.Stat(config.ProjectPath(project)); err != nil {
		t.Errorf("--init-project did not write the project file: %v", err)
	}
}

func TestMainDryRunExec(t *testing.T) {
	requireBwrap(t)
	configFile(t, "")
	t.Setenv("KANDOR_BIN", "/usr/bin/true")
	t.Setenv("KANDOR_HOME", t.TempDir())
	project, _ := filepath.EvalSymlinks(t.TempDir())
	bin, _ := filepath.EvalSymlinks("/usr/bin/true")
	code, out, errs := call(t, "-C", project, "--dry-run", "exec", "--", "/usr/bin/true", "hello")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errs)
	}
	for _, want := range []string{
		"--bind " + project + " " + project, "-- " + bin + " hello",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run is missing %q:\n%s", want, out)
		}
	}
	// Only look at the command after bwrap's "--" separator: the forwarded
	// environment may legitimately mention opencode (for example in PATH).
	fields := strings.Fields(out)
	sep := slices.Index(fields, "--")
	if sep < 0 {
		t.Fatalf("dry-run has no command separator:\n%s", out)
	}
	for _, f := range fields[sep+1:] {
		if strings.Contains(f, "opencode") {
			t.Errorf("exec run mentions opencode:\n%s", out)
			break
		}
	}
}
