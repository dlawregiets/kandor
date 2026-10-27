package cli

// Usage returns the --help text.
func Usage() string { return usage }

const usage = `Run OpenCode inside a bubblewrap sandbox.

Usage:
  kandor [options] [--] [opencode arguments...]

The current directory (or --dir) is the only project directory mounted
read-write.  Your home directory is hidden.  By default OpenCode shares
the host's data directory (sessions and stored logins).
Use --isolated (or --profile) for a sandbox-only profile
under ~/.local/share/kandor/<profile>: the host config is copied in once so
plugins and settings keep working, but host sessions and stored credentials
are not exposed.

Options:
  -C, --dir DIR           Project directory (default: current directory)
      --ro PATH           Mount PATH read-only (repeatable)
      --rw PATH           Mount PATH read-write (repeatable)
      --env NAME[=VALUE]  Pass an environment variable into the sandbox
                          (repeatable).  Without =VALUE, the host value is used.
      --isolated          Use a sandbox-only profile instead of host state
      --shared            Share the host's OpenCode data (default)
      --profile NAME      Isolated profile name (implies --isolated;
                          default: "default")
      --no-seed           Do not copy the host config into a new isolated
                          profile
      --net               Share the host network (default; needed for LLM APIs)
      --no-net            Run without network access (--unshare-net)
      --ssh               Mount ~/.ssh read-only
      --ssh-agent         Forward $SSH_AUTH_SOCK into the sandbox
      --git-credentials   Mount ~/.git-credentials read-only
      --netrc             Mount ~/.netrc read-only
      --npmrc             Mount ~/.npmrc read-only
      --no-gitconfig      Do not mount ~/.gitconfig or ~/.config/git
      --no-protect-git    Leave the repository's hooks and config writable
                          (needed for 'git config', 'push -u', 'remote add')
      --allow-userns      Allow nested user namespaces (needed by browsers;
                          weakens the sandbox)
      --new-session       Detach from the controlling terminal (TIOCSTI
                          protection; may prevent TUI resize).  Default: on
                          only when the kernel permits TIOCSTI.
      --no-new-session    Keep the controlling terminal (enables TUI resize)
      --shell             Run a shell inside the sandbox instead of OpenCode
      --init-config       Create a commented config file at the standard path
                          (or $KANDOR_CONFIG) and exit; never overwrites
      --migrate-config    Convert the old line-based config file to
                          config.toml and exit; never overwrites
      --dry-run           Print the bwrap command and exit (environment
                          secrets are redacted; arguments are printed as-is)
      --verbose           Print sandbox details
  -h, --help              Show this help
  -V, --version           Show the wrapper version

Everything after "--" is passed to OpenCode verbatim.  Unknown options are
also passed to OpenCode, so these are equivalent:
  kandor --continue
  kandor -- --continue

A positional directory is used as the project directory (like -C).

Examples:
  kandor
  kandor -C ~/code/my-project
  kandor run "fix the failing tests"
  kandor -C ~/code/proj --isolated --profile work
  kandor auth login            # stored in the host data directory
  kandor --isolated auth login # stored in the isolated profile
  kandor --no-net --shell
  kandor --ro ~/.config/gcloud --rw ~/go -- run "build it"
  kandor --init-config
  kandor --dry-run

Configuration file:
  $KANDOR_CONFIG, or $XDG_CONFIG_HOME/kandor/config.toml
  (default ~/.config/kandor/config.toml).  TOML with the sections [mounts],
  [env], [sandbox], [credentials], [git], [profile] and [agents.opencode].
  Run 'kandor --init-config' for a commented example.  Command-line options
  override the file; for mounts of the same path, the later layer wins
  (file, then [agents.opencode], then the command line).
`
