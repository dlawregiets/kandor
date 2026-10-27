package agent

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// OpenCode is https://opencode.ai.
var OpenCode Agent = opencode{}

type opencode struct{}

func (opencode) Name() string       { return "opencode" }
func (opencode) Executable() string { return "opencode" }

var opencodeSubcommands = []string{
	"run", "auth", "mcp", "plugin", "debug", "session", "api", "serve", "acp", "mini",
	"models", "stats", "pair", "reload", "service", "upgrade", "update", "uninstall", "completions",
}

func (opencode) IsSubcommand(arg string) bool { return slices.Contains(opencodeSubcommands, arg) }

// The host's OpenCode service and session variables must not leak in.
var opencodeExcluded = []string{
	"OPENCODE", "OPENCODE_SESSION_ID", "OPENCODE_CLIENT", "OPENCODE_TERMINAL",
	"OPENCODE_PASSWORD", "OPENCODE_SERVER_PASSWORD", "OPENCODE_DB", "OPENCODE_CONFIG",
	"OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG_CONTENT", "AI_AGENT",
}

func (opencode) ExcludeEnv(name string) bool { return slices.Contains(opencodeExcluded, name) }

// Command adds --standalone wherever the command uses a server, so OpenCode
// cannot attach to a background service outside the sandbox, and refuses
// commands that would talk to that service or manage the host installation.
func (opencode) Command(bin string, args []string) ([]string, error) {
	// --standalone is a per-command flag, not a global one before a
	// subcommand.  Drop a leading one and re-insert it in the right place.
	if len(args) > 0 && args[0] == "--standalone" {
		args = args[1:]
	}

	first := ""
	for _, a := range args {
		if a == "" || a[0] != '-' {
			first = a
			break
		}
	}
	switch first {
	case "service":
		return nil, fmt.Errorf("refusing to run 'opencode service' inside the sandbox; use --standalone commands instead")
	case "uninstall", "upgrade", "update":
		return nil, fmt.Errorf("refusing to run 'opencode %s' inside the sandbox", first)
	case "pair":
		return nil, fmt.Errorf("refusing to run 'opencode pair' inside the sandbox (it connects to the host service)")
	}
	for _, a := range args {
		if a == "--server" || a == "--service" || len(a) > 9 && a[:9] == "--server=" {
			return nil, fmt.Errorf("refusing '%s' inside the sandbox; the sandbox always uses a private server", a)
		}
	}
	if first == "acp" && slices.Contains(args, "--login") {
		return nil, fmt.Errorf("use 'kandor auth login' instead of 'acp --login' inside the sandbox")
	}

	// Options after "--" are positional arguments (a prompt that happens to
	// be "--help"), not flags.
	flags := args
	if i := slices.Index(args, "--"); i >= 0 {
		flags = args[:i]
	}
	cmd := []string{bin}
	switch {
	case len(args) == 0:
		return append(cmd, "--standalone"), nil
	case slices.Contains(flags, "--standalone"),
		slices.Contains(flags, "--help"), slices.Contains(flags, "-h"), slices.Contains(flags, "--version"):
		return append(cmd, args...), nil
	}

	switch sub := args[0]; sub {
	case "completions", "debug", "mcp", "plugin", "serve", "acp":
		// These either do not use a server or start their own.
		return append(cmd, args...), nil
	case "auth", "session":
		// A nested subcommand such as "auth list" or "session export".
		for i := 1; i < len(args); i++ {
			if args[i] == "" || args[i][0] != '-' {
				cmd = append(cmd, sub, args[i], "--standalone")
				cmd = append(cmd, args[1:i]...)
				return append(cmd, args[i+1:]...), nil
			}
		}
		// Bare "auth" or "session" prints help; no server is involved.
		return append(cmd, args...), nil
	case "run", "api", "models", "stats", "mini", "reload":
		cmd = append(cmd, sub, "--standalone")
		return append(cmd, args[1:]...), nil
	}
	// No subcommand: the TUI, where --standalone is a top-level flag.
	cmd = append(cmd, "--standalone")
	return append(cmd, args...), nil
}

// Prepare creates OpenCode's config/data/state/cache directories, copies the
// host config in (once for an isolated profile, every run in shared mode,
// never the host's service.json) and writes a private service.json.  In
// shared mode, a host config, state or cache directory mounted read-write is
// used directly instead of a temporary copy, so OpenCode's writes reach it.
func (opencode) Prepare(l Layout, warn func(string)) (State, error) {
	dirs := []string{l.Config, l.Data, l.State, l.Cache}
	for i, d := range dirs {
		dirs[i] = filepath.Join(d, "opencode")
		if err := os.MkdirAll(dirs[i], 0o755); err != nil {
			return State{}, fmt.Errorf("cannot create OpenCode's config/data/state/cache directories")
		}
	}
	configDir, stateDir, cacheDir := dirs[0], dirs[2], dirs[3]
	st := State{Env: []EnvVar{{"OPENCODE_DISABLE_AUTOUPDATE", "1"}}}

	// Host directories used directly, by index into dirs.
	direct := map[int]bool{}
	for i, x := range []struct{ host, env string }{
		0: {l.HostConfig, "XDG_CONFIG_HOME"}, 2: {l.HostState, "XDG_STATE_HOME"}, 3: {l.HostCache, "XDG_CACHE_HOME"},
	} {
		if x.host == "" || l.Writable == nil {
			continue
		}
		host := filepath.Join(x.host, "opencode")
		if !l.Writable(host) {
			continue
		}
		if l.Isolated {
			warn(fmt.Sprintf("%s is mounted read-write, but OpenCode uses the isolated profile's copy", host))
			continue
		}
		direct[i] = true
		st.Binds = append(st.Binds, Bind{Src: host, Dest: host, Writable: true})
		st.Env = append(st.Env, EnvVar{x.env, x.host})
		warn(fmt.Sprintf("OpenCode writes directly to %s, which is mounted read-write", host))
	}

	hostConfig := filepath.Join(l.HostConfig, "opencode")
	switch {
	case l.Isolated && l.Seed:
		marker := filepath.Join(configDir, ".kandor-seeded")
		if _, err := os.Stat(marker); err != nil {
			copyConfig(hostConfig, configDir, "sandbox profile", warn)
			os.WriteFile(marker, nil, 0o644)
		}
	case !l.Isolated:
		if !direct[0] {
			copyConfig(hostConfig, configDir, "shared sandbox profile", warn)
		}
		var copies []string
		for i, name := range []string{0: "config", 2: "state", 3: "cache"} {
			if name != "" && !direct[i] {
				copies = append(copies, name)
			}
		}
		msg := "sharing the host OpenCode data directory read-write (sessions and stored logins in auth.json)"
		if len(copies) > 0 {
			msg += "; " + strings.Join(copies, ", ") + " use temporary copies"
		}
		warn(msg)
	}

	writeServiceFiles(configDir, stateDir, warn)
	// The host's service files hold its service's password and URL.  With
	// them, a sandbox sharing the network could drive the host service,
	// which runs commands outside the sandbox.
	st.Masks = []Mask{
		{Src: filepath.Join(configDir, "service.json"), Dest: filepath.Join(hostConfig, "service.json"), Placeholder: "{}\n"},
		{Src: filepath.Join(stateDir, "service.json"), Dest: filepath.Join(l.HostState, "opencode", "service.json"), Placeholder: "{}\n"},
	}

	for i, d := range dirs {
		if !direct[i] {
			st.Binds = append(st.Binds, Bind{Src: d, Dest: d, Writable: true})
		}
	}
	// Reuse the host's cached ripgrep binary without exposing the host cache
	// (and its npm packages) read-write.
	if hostBin := filepath.Join(l.HostCache, "opencode", "bin"); !l.Isolated && !direct[3] && isDir(hostBin) {
		st.Binds = append(st.Binds, Bind{Src: hostBin, Dest: filepath.Join(cacheDir, "bin"), FollowSymlinks: true})
	}
	return st, nil
}

// writeServiceFiles gives the sandbox its own service registration on a free
// port, so a sandbox service cannot collide with the host's.
func writeServiceFiles(configDir, stateDir string, warn func(string)) {
	content := "{}\n"
	if port, err := freePort(); err == nil {
		content = fmt.Sprintf("{\"port\": %d}\n", port)
	} else {
		warn("could not find a free TCP port for OpenCode's private service")
	}
	for path, data := range map[string]string{
		filepath.Join(configDir, "service.json"): content,
		filepath.Join(stateDir, "service.json"):  "{}\n",
	} {
		os.Remove(path)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			warn("could not write " + path)
		}
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func copyConfig(src, dst, what string, warn func(string)) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() == "service.json" || e.Name()[0] == '.' {
			continue
		}
		from := filepath.Join(src, e.Name())
		if err := copyTree(from, filepath.Join(dst, e.Name())); err != nil {
			warn(fmt.Sprintf("could not copy %s into the %s", from, what))
		}
	}
}

// copyTree copies like "cp -aL": symlinks are followed and modes and
// modification times are kept.
func copyTree(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	switch {
	case st.IsDir():
		if err := os.MkdirAll(dst, st.Mode().Perm()|0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		var firstErr error
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		os.Chmod(dst, st.Mode().Perm())
		os.Chtimes(dst, st.ModTime(), st.ModTime())
		return firstErr
	case st.Mode().IsRegular():
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		os.Remove(dst)
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Chtimes(dst, st.ModTime(), st.ModTime())
	}
	return nil // sockets, devices and pipes are skipped
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
