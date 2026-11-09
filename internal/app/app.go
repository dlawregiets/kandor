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
	"slices"
	"strconv"
	"strings"

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
	return run(args, os.Stdin, os.Stdout, os.Stderr)
}

type output struct{ stdout, stderr io.Writer }

func (o output) log(msg string)  { fmt.Fprintf(o.stderr, "kandor: %s\n", msg) }
func (o output) warn(msg string) { fmt.Fprintf(o.stderr, "kandor: warning: %s\n", msg) }
func (o output) fail(err error) int {
	fmt.Fprintf(o.stderr, "kandor: error: %s\n", err)
	return 1
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	o := output{stdout, stderr}
	runner.MarkInheritedFDsCloseOnExec()

	// Provisional parse to find --agent, --help and --version; the agent
	// itself is resolved from the configuration right after.
	pre, err := cli.Parse(args, agent.ProvisionalIsSubcommand)
	if err != nil {
		return o.fail(err)
	}
	if pre.Help {
		fmt.Fprint(stdout, cli.Usage())
		return 0
	}
	if pre.Version {
		fmt.Fprintf(stdout, "kandor %s\n", version)
		return 0
	}

	loc := config.Locate(os.Getenv)
	if pre.InitConfig {
		if err := config.WriteTemplate(loc.TOML); err != nil {
			return o.fail(err)
		}
		o.log("created " + loc.TOML)
		return 0
	}
	file, err := config.Load(loc.TOML)
	if err != nil {
		return o.fail(err)
	}

	// --agent wins, then KANDOR_AGENT, then the user configuration.

	// --agent wins, then KANDOR_AGENT, then the user configuration.
	agentName := pre.Agent
	if agentName == "" {
		agentName = os.Getenv("KANDOR_AGENT")
	}
	if agentName == "" {
		agentName = file.DefaultAgent
	}
	if agentName == "" {
		agentName = agent.OpenCode.Name()
	}
	known := agent.Known(file.Agents)
	if !slices.Contains(known, agentName) {
		return o.fail(fmt.Errorf("unknown agent %q (known: %s)", agentName, strings.Join(known, ", ")))
	}
	req, err := cli.Parse(args, agent.IsSubcommandFor(agentName, file.Agents[agentName]))
	if err != nil {
		return o.fail(err)
	}
	if req.Exec {
		if req.Shell {
			return o.fail(errors.New("--shell and exec are mutually exclusive"))
		}
		if req.Agent != "" {
			return o.fail(errors.New("exec does not take --agent"))
		}
		if req.Overrides.Isolated != nil || req.Overrides.Profile != "" || req.Overrides.Seed != nil {
			return o.fail(errors.New("exec does not take --isolated, --profile or --no-seed"))
		}
	}

	home, err := validHome(os.Getenv("HOME"))
	if err != nil {
		return o.fail(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return o.fail(err)
	}
	projDir := config.ResolveProjectDir(req.Overrides, cwd)
	projPath := config.ProjectPath(projDir)
	proj, haveProj, err := config.LoadProject(projDir)
	if err != nil {
		return o.fail(err)
	}
	if req.InitProject {
		dir, err := validProject(projDir, home)
		if err != nil {
			return o.fail(err)
		}
		if err := initProject(dir, file, stdin, stderr); err != nil {
			return o.fail(err)
		}
		return 0
	}
	mergeAgent := agentName
	if req.Exec {
		// No agent runs: agent sections stay out of the merge.
		mergeAgent = ""
	}
	s, err := config.Merge(file, proj, req.Overrides, mergeAgent, home, cwd, os.Getenv)
	if err != nil {
		return o.fail(err)
	}
	if haveProj {
		if w := config.WideningKeys(proj, mergeAgent); len(w) > 0 {
			o.warn(fmt.Sprintf("%s widens the sandbox: %s", projPath, strings.Join(w, ", ")))
		}
	}
	var ag agent.Agent
	if !req.Exec {
		ag = agent.For(agentName, s.Agent)
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
	if !req.DryRun {
		if err := runner.Preflight(features); err != nil {
			return o.fail(err)
		}
	}

	var binary string
	var execCmd []string
	if req.Exec {
		var execArgs []string
		if binary, execArgs, err = resolveExec(req.AgentArgs); err != nil {
			return o.fail(err)
		}
		execCmd = append([]string{binary}, execArgs...)
		if s.Isolated {
			o.warn("exec always uses a temporary profile; ignoring the isolated profile settings")
			s.Isolated = false
		}
	} else {
		if binary, err = findBinary(req.Shell, s.AgentBinary, ag); err != nil {
			return o.fail(err)
		}
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
	layout, cleanup, err := profile.Setup(s, host, kandorHome, tmpdir, uid, profileScope(agentName), req.DryRun)
	if err != nil {
		return o.fail(err)
	}
	defer cleanup()
	if req.Exec {
		// No agent state: XDG data gets a private temporary directory
		// instead of the host's.
		execData, err := os.MkdirTemp("", "kandor-exec-data")
		if err != nil {
			return o.fail(err)
		}
		defer os.RemoveAll(execData)
		layout.Data = execData
	}
	layout.Writable = writableMounts(s.Mounts)
	state := agent.State{}
	if !req.Exec {
		var err error
		if state, err = ag.Prepare(layout, o.warn); err != nil {
			return o.fail(err)
		}
		// Host-state paths declared by [agents.<name>] (built-in defaults
		// plus the merged user, section and project layers) become extra
		// binds.
		spec := agent.Resolve(ag.Name(), s.Agent)
		state.Binds = append(state.Binds,
			agent.Materialize(spec, filepath.Join(layout.Cache, ag.Name(), "paths"),
				filepath.Join(layout.HostCache, "kandor", ag.Name()), home, req.DryRun, layout.Isolated, o.warn)...)
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
		DryRun:     req.DryRun,
	})
	if err != nil {
		return o.fail(err)
	}

	disableWorks := false
	if !s.AllowUserns {
		if req.DryRun {
			// Plan to disable nested user namespaces when bwrap supports
			// it, without spending a probe subprocess on a preview.
			disableWorks = features.DisableUserns
		} else {
			disableWorks = runner.DisableUsernsWorks(features)
		}
	}
	if req.Verbose && !req.DryRun && !s.AllowUserns && !disableWorks {
		o.warn("nested user namespaces cannot be disabled on this system")
	}
	sandbox.AddNamespaces(p, s, features, disableWorks, newSession, s.ProjectDir)

	vars, err := sandbox.Environment(sandbox.EnvInput{
		Environ:         os.Environ(),
		Home:            home,
		RuntimeDir:      runtimeDir,
		Layout:          layout,
		AgentEnv:        state.Env,
		Exclude:         excludeEnv(ag),
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
	if req.Exec {
		cmd = execCmd
	} else if req.Shell {
		cmd = append([]string{binary, "-l"}, req.AgentArgs...)
	} else if cmd, err = ag.Command(binary, req.AgentArgs); err != nil {
		return o.fail(err)
	}

	if req.Verbose {
		o.log("project:  " + s.ProjectDir)
		if haveProj {
			o.log("project config: " + projPath)
		}
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

// profileScope namespaces isolated profiles per agent, except OpenCode's,
// which keeps the historical flat layout.
func profileScope(agentName string) string {
	if agentName == agent.OpenCode.Name() {
		return ""
	}
	return agentName
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

// excludeEnv adapts a possibly absent agent to the environment filter.
func excludeEnv(ag agent.Agent) func(string) bool {
	if ag == nil {
		return nil
	}
	return ag.ExcludeEnv
}

// resolveExec resolves an exec command the way findBinary resolves agent
// binaries: a slash runs the path itself, otherwise PATH is searched.
func resolveExec(argv []string) (bin string, args []string, err error) {
	name := argv[0]
	if strings.Contains(name, "/") {
		bin = name
	} else {
		found, lookErr := exec.LookPath(name)
		if lookErr != nil {
			return "", nil, fmt.Errorf("%s was not found in PATH", name)
		}
		bin = found
	}
	resolved, err := paths.Resolve(bin)
	if err != nil {
		return "", nil, fmt.Errorf("cannot resolve %s", name)
	}
	if !executable(resolved) {
		return "", nil, fmt.Errorf("%s is not executable: %s", name, resolved)
	}
	return resolved, argv[1:], nil
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
