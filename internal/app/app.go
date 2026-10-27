// Package app runs kandor: it ties the command line, the configuration, the
// agent and the sandbox planner together.
package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/dlawregiets/kandor/internal/agent"
	"github.com/dlawregiets/kandor/internal/cli"
	"github.com/dlawregiets/kandor/internal/config"
	"github.com/dlawregiets/kandor/internal/paths"
	"github.com/dlawregiets/kandor/internal/profile"
	runner "github.com/dlawregiets/kandor/internal/run"
	"github.com/dlawregiets/kandor/internal/sandbox"
)

// version is the wrapper version, stamped at build time with
// -ldflags "-X github.com/dlawregiets/kandor/internal/app.version=<version>".
// It falls back to "dev" for plain go build and go test runs.
var version = "dev"

// Main runs kandor and returns its exit status.
func Main(args []string) int {
	return run(args, os.Stdout, os.Stderr)
}

type output struct{ stdout, stderr io.Writer }

func (o output) log(msg string)  { fmt.Fprintf(o.stderr, "kandor: %s\n", msg) }
func (o output) warn(msg string) { fmt.Fprintf(o.stderr, "kandor: warning: %s\n", msg) }
func (o output) fail(err error) int {
	fmt.Fprintf(o.stderr, "kandor: error: %s\n", err)
	return 1
}

func run(args []string, stdout, stderr io.Writer) int {
	o := output{stdout, stderr}
	runner.MarkInheritedFDsCloseOnExec()
	ag := agent.OpenCode

	req, err := cli.Parse(args, ag.IsSubcommand)
	if err != nil {
		return o.fail(err)
	}
	if req.Help {
		fmt.Fprint(stdout, cli.Usage())
		return 0
	}
	if req.Version {
		fmt.Fprintf(stdout, "kandor %s\n", version)
		return 0
	}

	loc := config.Locate(os.Getenv)
	if req.MigrateConfig {
		dest, err := config.Migrate(loc)
		if err != nil {
			return o.fail(err)
		}
		o.log("wrote " + dest)
		if loc.Explicit {
			o.log("update KANDOR_CONFIG to point at it")
		}
		return 0
	}
	if req.InitConfig {
		if err := config.WriteTemplate(loc.TOML); err != nil {
			return o.fail(err)
		}
		o.log("created " + loc.TOML)
		return 0
	}
	if err := config.CheckLegacy(loc); err != nil {
		return o.fail(err)
	}
	file, err := config.Load(loc.TOML)
	if err != nil {
		return o.fail(err)
	}

	home, err := validHome(os.Getenv("HOME"))
	if err != nil {
		return o.fail(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return o.fail(err)
	}
	s, err := config.Merge(file, req.Overrides, ag.Name(), home, cwd, os.Getenv)
	if err != nil {
		return o.fail(err)
	}
	if s.ProjectDir, err = validProject(s.ProjectDir, home); err != nil {
		return o.fail(err)
	}

	if _, err := exec.LookPath("bwrap"); err != nil {
		return o.fail(errors.New("bubblewrap (bwrap) is not installed"))
	}
	help, _ := exec.Command("bwrap", "--help").CombinedOutput()
	if len(help) == 0 {
		return o.fail(errors.New("bwrap does not appear to work"))
	}
	features := sandbox.ParseFeatures(string(help))
	if err := runner.Preflight(features); err != nil {
		return o.fail(err)
	}

	binary, err := findBinary(req.Shell, s.AgentBinary, ag)
	if err != nil {
		return o.fail(err)
	}

	resolve := func(env, fallback string) string {
		if v := os.Getenv(env); v != "" {
			return paths.ResolveMaybe(v, cwd)
		}
		return paths.ResolveMaybe(fallback, cwd)
	}
	host := profile.XDG{
		Config: resolve("XDG_CONFIG_HOME", filepath.Join(home, ".config")),
		Data:   resolve("XDG_DATA_HOME", filepath.Join(home, ".local", "share")),
		State:  resolve("XDG_STATE_HOME", filepath.Join(home, ".local", "state")),
		Cache:  resolve("XDG_CACHE_HOME", filepath.Join(home, ".cache")),
	}
	kandorHome := ""
	if v := os.Getenv("KANDOR_HOME"); v != "" {
		kandorHome = paths.ResolveMaybe(v, cwd)
	}
	tmpdir := os.Getenv("TMPDIR")
	if tmpdir == "" {
		tmpdir = "/tmp"
	}
	uid := os.Getuid()
	// From here on kandor creates temporary files.  Catch signals until they
	// are cleaned up (deferred calls run in reverse, so the guard stops last).
	guard := runner.NewGuard()
	defer guard.Stop()
	layout, cleanup, err := profile.Setup(s, host, kandorHome, tmpdir, uid)
	if err != nil {
		return o.fail(err)
	}
	defer cleanup()
	layout.Writable = writableMounts(s.Mounts)
	state, err := ag.Prepare(layout, o.warn)
	if err != nil {
		return o.fail(err)
	}

	newSession, explicit := sandbox.NewSessionDefault(s.NewSession, sandbox.TIOCSTIPath)
	if newSession && !explicit {
		o.warn("TIOCSTI is enabled on this kernel; using --new-session for safety")
		o.warn("terminal resizing may not work; pass --no-new-session to disable it")
	}

	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/run/user/" + strconv.Itoa(uid)
	}
	runtimeDir = paths.ResolveMaybe(runtimeDir, cwd)
	if st, err := os.Stat(runtimeDir); err != nil || !st.IsDir() {
		runtimeDir = "/tmp/kandor-runtime"
	}

	p := sandbox.NewPlan(features)
	mounts, err := sandbox.BuildMounts(p, sandbox.Input{
		Settings:   s,
		Host:       paths.Host{Home: home, RuntimeDir: runtimeDir, UID: uid},
		Features:   features,
		AgentBinds: state.Binds,
		AgentMasks: state.Masks,
		Binary:     binary,
		XDGConfig:  host.Config,
		Getenv:     os.Getenv,
		Warn:       o.warn,
	})
	if err != nil {
		return o.fail(err)
	}

	disableWorks := !s.AllowUserns && runner.DisableUsernsWorks(features)
	if req.Verbose && !s.AllowUserns && !disableWorks {
		o.warn("nested user namespaces cannot be disabled on this system")
	}
	sandbox.AddNamespaces(p, s, features, disableWorks, newSession, s.ProjectDir)

	vars, err := sandbox.Environment(sandbox.EnvInput{
		Environ:         os.Environ(),
		Home:            home,
		RuntimeDir:      runtimeDir,
		Layout:          layout,
		AgentEnv:        state.Env,
		Exclude:         ag.ExcludeEnv,
		Extra:           s.Env,
		SSHAuthSock:     mounts.SSHAuthSock,
		GitConfigGlobal: mounts.GitConfigGlobal,
		Warn:            o.warn,
	})
	if err != nil {
		return o.fail(err)
	}
	env := sandbox.EnvList(vars)

	var cmd []string
	if req.Shell {
		cmd = append([]string{binary, "-l"}, req.AgentArgs...)
	} else if cmd, err = ag.Command(binary, req.AgentArgs); err != nil {
		return o.fail(err)
	}

	if req.Verbose {
		o.log("project:  " + s.ProjectDir)
		if s.Isolated {
			o.log(fmt.Sprintf("profile:  %s (%s)", s.Profile, layout.ProfileRoot))
		} else {
			o.log("profile:  shared sessions (private config/state/cache copy)")
		}
		network := "shared"
		if !s.Network {
			network = "isolated"
		}
		o.log("network:  " + network)
		o.log("runtime:  " + runtimeDir)
		o.log("command:  " + binary)
	}

	argv := append([]string{"bwrap"}, p.Args()...)
	argv = append(append(argv, "--"), cmd...)
	// A signal during startup ends the run here, after which the deferred
	// cleanup removes the temporary profile.
	if sig, ok := guard.Pending(); ok {
		return 128 + int(sig)
	}
	if req.DryRun {
		fmt.Fprintln(stdout, sandbox.FormatCommand(env, argv))
		return 0
	}

	code := guard.Child(argv, env)
	sandbox.Audit(mounts.Git, s.ProjectDir, o.warn)
	return code
}

// writableMounts reports whether a path is an extra mount that ends up
// read-write (the last mount of a path wins).  Paths are compared resolved.
func writableMounts(mounts []config.MountSpec) func(string) bool {
	rw := map[string]bool{}
	for _, m := range mounts {
		if resolved, err := paths.Resolve(m.Path); err == nil {
			rw[resolved] = m.Writable
		}
	}
	return func(p string) bool {
		resolved, err := paths.Resolve(p)
		return err == nil && rw[resolved]
	}
}

func validHome(home string) (string, error) {
	if home == "" {
		return "", errors.New("HOME is not set")
	}
	if st, err := os.Stat(home); err != nil || !st.IsDir() {
		return "", fmt.Errorf("HOME is not a directory: %s", home)
	}
	resolved, err := paths.Resolve(home)
	if err != nil {
		return "", fmt.Errorf("cannot resolve HOME: %s", home)
	}
	if resolved == "/" {
		return "", errors.New("refusing to run with HOME=/")
	}
	return resolved, nil
}

func validProject(dir, home string) (string, error) {
	resolved, err := paths.Resolve(dir)
	if err != nil {
		return "", fmt.Errorf("project directory does not exist: %s", dir)
	}
	switch st, _ := os.Stat(resolved); {
	case !st.IsDir():
		return "", fmt.Errorf("not a directory: %s", resolved)
	case resolved == "/":
		return "", errors.New("refusing to use / as the project directory")
	case resolved == home:
		return "", errors.New("refusing to use HOME as the project directory; run from a project subdirectory")
	case paths.Contains(resolved, home):
		return "", errors.New("the project directory may not be an ancestor of HOME")
	}
	return resolved, nil
}

// findBinary resolves the debug shell or the agent's executable.
func findBinary(shell bool, configured string, ag agent.Agent) (string, error) {
	if shell {
		bin := os.Getenv("SHELL")
		if bin == "" || !executable(bin) {
			bin = "/bin/bash"
		}
		if !executable(bin) {
			return "", errors.New("no usable shell found (tried $SHELL and /bin/bash)")
		}
		if resolved, err := paths.Resolve(bin); err == nil {
			return resolved, nil
		}
		return "/bin/bash", nil
	}
	name := ag.Executable()
	bin := configured
	if bin == "" {
		found, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("%s was not found in PATH (set KANDOR_BIN to override)", name)
		}
		bin = found
	}
	resolved, err := paths.Resolve(bin)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the %s binary", name)
	}
	if !executable(resolved) {
		return "", fmt.Errorf("%s is not executable: %s", name, resolved)
	}
	return resolved, nil
}

func executable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}
