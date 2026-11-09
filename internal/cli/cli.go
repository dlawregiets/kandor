// Package cli parses kandor's command line.
package cli

import (
	"errors"
	"os"
	"strings"

	"github.com/dlawregiets/kandor/internal/config"
)

// Request is the parsed command line.
type Request struct {
	Overrides config.Overrides
	// AgentArgs are passed to the agent unchanged.
	AgentArgs []string

	Help, Version           bool
	InitConfig, InitProject bool
	Shell, DryRun, Verbose  bool
	// Agent selects the coding agent; "" resolves from KANDOR_AGENT, the
	// user configuration, then the "opencode" default.
	Agent string
	// Exec runs an arbitrary command instead of an agent.  AgentArgs is
	// the command.
	Exec bool
}

type option struct {
	key     string // the configuration key this option sets, or ""
	missing string // error when the option's argument is missing; "" for flags
	apply   func(r *Request, arg string)
}

func on(p **bool, v bool) func(*Request, string) {
	return func(*Request, string) { *p = &v }
}

func session(m config.NewSessionMode) func(*Request, string) {
	return func(r *Request, _ string) { r.Overrides.NewSession = &m }
}

// options maps every option kandor consumes.  Each entry needs the Request it
// will modify, so the table is built per parse.
func options(r *Request) map[string]option {
	o := &r.Overrides
	dir := option{"", "requires a directory argument", func(r *Request, v string) { o.ProjectDir = v }}
	dryRun := option{"", "", func(r *Request, _ string) { r.DryRun = true }}
	return map[string]option{
		"-C":        dir,
		"--dir":     dir,
		"--project": dir,
		"--ro": {"mounts.read_only", "--ro requires a path", func(r *Request, v string) {
			o.Mounts = append(o.Mounts, config.MountSpec{Path: v})
		}},
		"--rw": {"mounts.read_write", "--rw requires a path", func(r *Request, v string) {
			o.Mounts = append(o.Mounts, config.MountSpec{Path: v, Writable: true})
		}},
		"--env": {"env.set", "--env requires NAME or NAME=VALUE", func(r *Request, v string) {
			name, value, has := strings.Cut(v, "=")
			o.Env = append(o.Env, config.EnvSpec{Name: name, Value: value, HasValue: has})
		}},
		"--isolated": {"profile.isolated", "", on(&o.Isolated, true)},
		"--shared":   {"profile.isolated", "", on(&o.Isolated, false)},
		"--profile": {"profile.name", "--profile requires a name", func(r *Request, v string) {
			o.Profile = v
			isolated := true
			o.Isolated = &isolated
		}},
		"--no-seed":         {"profile.seed", "", on(&o.Seed, false)},
		"--net":             {"sandbox.network", "", on(&o.Network, true)},
		"--no-net":          {"sandbox.network", "", on(&o.Network, false)},
		"--ssh":             {"credentials.ssh", "", on(&o.SSH, true)},
		"--ssh-agent":       {"credentials.ssh_agent", "", on(&o.SSHAgent, true)},
		"--git-credentials": {"credentials.git_credentials", "", on(&o.GitCredentials, true)},
		"--netrc":           {"credentials.netrc", "", on(&o.Netrc, true)},
		"--npmrc":           {"credentials.npmrc", "", on(&o.Npmrc, true)},
		"--no-gitconfig":    {"git.mount_config", "", on(&o.MountGitConfig, false)},
		"--protect-git":     {"git.protect", "", on(&o.ProtectGit, true)},
		"--no-protect-git":  {"git.protect", "", on(&o.ProtectGit, false)},
		"--allow-userns":    {"sandbox.allow_userns", "", on(&o.AllowUserns, true)},
		"--new-session":     {"sandbox.new_session", "", session(config.SessionOn)},
		"--no-new-session":  {"sandbox.new_session", "", session(config.SessionOff)},
		"--shell":           {"", "", func(r *Request, _ string) { r.Shell = true }},
		"--init-config":     {"", "", func(r *Request, _ string) { r.InitConfig = true }},
		"--init-project":    {"", "", func(r *Request, _ string) { r.InitProject = true }},
		"--agent":           {"", "--agent requires a name", func(r *Request, v string) { r.Agent = v }},
		"--dry-run":         dryRun,
		"--print-command":   dryRun,
		"--verbose":         {"", "", func(r *Request, _ string) { r.Verbose = true }},
		"-h":                {"", "", func(r *Request, _ string) { r.Help = true }},
		"--help":            {"", "", func(r *Request, _ string) { r.Help = true }},
		"-V":                {"", "", func(r *Request, _ string) { r.Version = true }},
		"--version":         {"", "", func(r *Request, _ string) { r.Version = true }},
	}
}

// OptionKeys maps each option to the configuration key it sets ("" for
// options that are not settings).
func OptionKeys() map[string]string {
	keys := map[string]string{}
	for name, opt := range options(&Request{}) {
		keys[name] = opt.key
	}
	return keys
}

// AgentArg returns the --agent selection without full parsing, so the agent
// (and its subcommands) is known before the command line is parsed.  The
// scan mirrors Parse: it stops at "--", at unknown options and at the first
// non-directory positional, and it skips the values of known options.
func AgentArg(args []string) string {
	var r Request
	opts := options(&r)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--agent" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		opt, known := opts[arg]
		if !known {
			if !strings.HasPrefix(arg, "-") && isDir(arg) {
				continue
			}
			break
		}
		if opt.missing != "" {
			i++
		}
	}
	return ""
}

// execIndex finds a leading "exec" mode marker: the first positional that
// is not a directory.  Kandor options before it still apply; everything
// after it is the command.  A directory named exec keeps its meaning: use
// -C exec (or ./exec) for that project.
func execIndex(args []string, opts map[string]option) (int, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return -1, false
		}
		if arg == "exec" {
			return i, true
		}
		opt, known := opts[arg]
		if !known {
			if !strings.HasPrefix(arg, "-") && isDir(arg) {
				continue
			}
			return -1, false
		}
		if opt.missing != "" {
			if i+1 >= len(args) {
				return -1, false
			}
			i++
		}
	}
	return -1, false
}

// Parse parses args.  Parsing stops at "--" (dropped), at the first option
// kandor does not know, or at the first positional argument; everything from
// there is passed to the agent.  A positional argument that is an existing
// directory and not an agent subcommand is the project directory, and
// parsing continues after it.  A leading "exec" switches to generic-command
// mode instead: kandor options before it apply, everything after it is the
// command.  --help and --version stop parsing.
func Parse(args []string, isSubcommand func(string) bool) (Request, error) {
	var r Request
	opts := options(&r)
	if i, ok := execIndex(args, opts); ok {
		if err := parseInto(&r, opts, args[:i], isSubcommand); err != nil {
			return Request{}, err
		}
		rest := args[i+1:]
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return Request{}, errors.New("exec requires a command")
		}
		r.Exec = true
		r.AgentArgs = rest
		return r, nil
	}
	if err := parseInto(&r, opts, args, isSubcommand); err != nil {
		return Request{}, err
	}
	return r, nil
}

func parseInto(r *Request, opts map[string]option, args []string, isSubcommand func(string) bool) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			r.AgentArgs = append(r.AgentArgs, args[i+1:]...)
			break
		}
		opt, known := opts[arg]
		if !known {
			if !strings.HasPrefix(arg, "-") && !isSubcommand(arg) && isDir(arg) {
				r.Overrides.ProjectDir = arg
				continue
			}
			r.AgentArgs = append(r.AgentArgs, args[i:]...)
			break
		}
		value := ""
		if opt.missing != "" {
			if i+1 >= len(args) {
				msg := opt.missing
				if !strings.HasPrefix(msg, "-") {
					msg = arg + " " + msg
				}
				return errors.New(msg)
			}
			i++
			value = args[i]
		}
		opt.apply(r, value)
		if r.Help || r.Version {
			break
		}
	}
	return nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
