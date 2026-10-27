# kandor: port to Go with a TOML configuration file

Date: 2026-10-27
Status: draft for review

## Intent

`kandor` runs OpenCode inside a bubblewrap (`bwrap`) sandbox on Linux. It is
currently a ~1,600-line Bash script. The next features are a friendlier,
better organised configuration file, then per-project configuration, then
support for other coding agents again (Pi, oh-my-pi and similar). All of those
are structured data problems that Bash handles badly, and the script is
security-sensitive: most of the bugs found in the recent review came from Bash
itself (empty arrays under `set -u`, command substitution overwriting
variables, hand-rolled file-descriptor closing).

This project ports `kandor` to Go and replaces the one-directive-per-line
configuration file with a sectioned TOML file. It also lays the groundwork for
the later features without building them.

## Goals

1. **Behavioural parity.** The Go binary does what the current OpenCode-only
   Bash script does: same command-line options, same sandbox, same security
   properties. The Bash script on the `opencode-only` branch is the reference;
   the existing `tests/smoke.sh` is the parity check.
2. **A sectioned TOML global configuration file** (schema below) that replaces
   the line-based file.
3. **Layered configuration:** built-in defaults, then the global file, then the
   command line. Adding a per-project layer later should mean adding one layer,
   not restructuring.
4. **Every configurable setting is labelled as narrowing or widening** the
   sandbox, in code, so a future per-project layer can apply narrowing settings
   automatically and require approval for widening ones.
5. **An agent abstraction** with OpenCode as the only built-in agent, so Pi and
   omp can be added later as new implementations rather than branches scattered
   through the code.
6. **A single static binary** whose only required runtime dependency is
   `bwrap`. Like the Bash script, it calls the `git` program when it is
   installed (repository discovery with `git rev-parse`, reading a
   submodule's `core.worktree` with `git config --file`) and skips the git
   handling when it is not. My call: reimplementing git's repository discovery
   would be the largest and most security-sensitive part of the port for no
   user-visible gain.

## Non-goals (for this project)

- Per-project configuration files and the approval step. The design must leave
  room for them (goals 3 and 4); building them is a later project.
- Re-adding Pi, omp or any other agent.
- New sandbox features or changes to the security model beyond what parity
  requires.
- Platforms other than Linux.

## Anti-goals (it fails even if it technically works)

- The Go version is weaker than the Bash version in any way: a mount, an
  environment variable, a file descriptor or a host socket that the Bash
  version kept out of the sandbox gets in.
- A typo in the configuration file is silently ignored.
- New runtime dependencies (an interpreter, shared libraries, a daemon).
  Optional use of `git`, as today, is not new.
- Supporting both configuration formats indefinitely.

## Constraints

- Linux only. `bwrap` stays an external program; `kandor` builds its argument
  list and runs it.
- Go 1.27 (installed). Standard library plus one TOML library. My call:
  `github.com/BurntSushi/toml`, because its decode metadata reports undecoded
  keys, which gives strict "unknown key is an error" behaviour.
- Installed with `make install` into `~/.local/bin` as today.
- The user does not want Claude listed as a co-author on commits.

## Behaviour to preserve

The Bash script is the authority on details; this list is what must not be
lost. Where this list and the script disagree, the script wins unless this
document says otherwise.

**Command line.** Same options and meanings: `-C/--dir/--project`, `--ro`,
`--rw`, `--env NAME[=VALUE]`, `--isolated`, `--shared`, `--profile`,
`--no-seed`, `--net`, `--no-net`, `--ssh`, `--ssh-agent`, `--git-credentials`,
`--netrc`, `--npmrc`, `--no-gitconfig`, `--protect-git`, `--no-protect-git`,
`--allow-userns`, `--new-session`, `--no-new-session`, `--shell`,
`--init-config`, `--dry-run` (alias `--print-command`), `--verbose`,
`-h/--help`, `-V/--version`. Parsing stops at `--`, at the first unknown
option, or at the first positional argument; everything from there goes to
OpenCode. A positional argument that is an existing directory and not an
OpenCode subcommand name is the project directory. This needs a small custom
parser; Go's `flag` package cannot express these rules.

**OpenCode command.** `--standalone` insertion rules per subcommand, refusal of
`service`, `pair`, `uninstall`/`upgrade`/`update`, `--server`/`--service`, and
`acp --login`.

**Mounts.** Read-only root; hidden home (tmpfs over the home parent); private
`/tmp`, `/var/tmp`, `/run`, `/mnt`, `/media`, `/srv`, `/root`; resolver file
re-exposed when `/etc/resolv.conf` points into `/run`; private runtime
directory; project read-write; validated git common directory (worktree
back-reference, genuine submodule check, nothing rooted at HOME or above);
OpenCode state directories; binary directory and user tool directories
read-only with symlink-target handling (skip broad paths and parents of the
project); git config files; opt-in credentials; files named by credential/CA
environment variables; explicit extra mounts with the same refusal rules.

**Git protection.** Read-only hooks and config, pinned `.git`/`.git` file/
`commondir`, and the post-run audit (quarantine a new `commondir`, warn about a
newly created repository).

**Profiles.** Shared mode (temporary config/state/cache copy, host data
directory, stale temporary directory cleanup) and isolated mode (profile under
`$KANDOR_HOME` or `$XDG_DATA_HOME/kandor`, one-time seeding, `service.json`
with a free port).

**Namespaces and security options.** User namespace with `--disable-userns`
when the kernel supports it, PID/IPC/UTS/cgroup namespaces, optional network
namespace, hostname, `--new-session` default based on
`/proc/sys/dev/tty/legacy_tiocsti`, `--die-with-parent`, `--cap-drop ALL`,
feature detection from `bwrap --help`, the user-namespace preflight and its
error message.

**Environment.** `--clearenv`, the fixed variables, the provider-prefix
allowlist, the excluded variables, `--env` handling, and `--dry-run` redaction
(names and URL credentials).

**Process handling.** No file descriptors other than stdin, stdout and stderr
reach `bwrap`. `kandor` stays the parent of `bwrap` (no exec) so cleanup and
the git audit run when it exits, including on SIGINT/SIGTERM/SIGHUP. The
sandbox's exit status becomes `kandor`'s exit status.

**Environment variables read by kandor.** `KANDOR_CONFIG`, `KANDOR_HOME`,
`KANDOR_BIN`, plus `HOME`, `XDG_*`, `TMPDIR`, `SHELL`, `SSH_AUTH_SOCK` and the
others the script reads.

## Configuration file

**Location.** `$KANDOR_CONFIG` if set, otherwise
`$XDG_CONFIG_HOME/kandor/config.toml` (default `~/.config/kandor/config.toml`).
A missing file is fine.

**Schema** (every key optional; values shown are the defaults unless noted):

```toml
[mounts]
read_only  = []                  # e.g. ["~/.config/gcloud"]
read_write = []                  # e.g. ["~/go", "~/.cache/go-build"]

[env]
pass = []                        # names copied from the host environment

[env.set]                        # NAME = "value"

[sandbox]
network      = true
allow_userns = false
new_session  = "auto"            # "auto", true or false

[credentials]
ssh             = false
ssh_agent       = false
git_credentials = false
netrc           = false
npmrc           = false

[git]
mount_config = true              # ~/.gitconfig, ~/.config/git
protect      = true

[profile]
isolated = false
name     = "default"
seed     = true

[agents.opencode]
# binary = "/path/to/opencode"   # same role as KANDOR_BIN

[agents.opencode.mounts]
read_only  = []
read_write = []

[agents.opencode.env]
pass = []

[agents.opencode.env.set]
```

**Rules.**

- Unknown sections, unknown keys and wrong types are errors that name the file,
  the key and (where the library provides it) the line.
- `[agents.<name>]` with a name other than `opencode` is an error ("unknown
  agent") for now.
- Paths may start with `~` or `$HOME`; nothing else is expanded.
- Precedence: built-in defaults, then the file, then the command line.
  Booleans: the command line overrides the file. Lists: the command line
  appends to the file's lists.
- Mounts of the same path in different layers: the later layer wins, in the
  order global file, global agent section, command line. So `--ro X` makes a
  file `read_write` entry read-only for that run, and `--rw X` makes a file
  `read_only` entry writable. My call: this is a small change from the Bash
  script (where read-write always won because all read-only extras were
  emitted first), but it is what users expect from "the command line
  overrides the file", and it lets a future per-project layer narrow a mount.
  Within one layer, a path in both `read_only` and `read_write` is an error.
- Agent sections apply only when that agent runs and are applied after the
  top-level sections (agent mounts are appended after global mounts; agent
  `env.set` overrides global `env.set`).
- `env.set` names must be valid environment variable names; `[env]` entries
  behave exactly like `--env NAME` (`pass`) and `--env NAME=VALUE` (`set`).
- `KANDOR_BIN` overrides `agents.opencode.binary`.
- `profile.name` follows the existing validation and setting it implies
  `isolated = true`, matching `--profile`.

**Narrowing and widening labels.** Each key and each command-line option is
classified in code as narrowing, widening or neutral, for example:
`sandbox.network = false` narrows, `mounts.read_write` widens, any
`credentials.*` set to true widens, `git.protect = false` widens,
`profile.isolated = true` narrows. Nothing uses these labels yet except a test
that fails if a setting has no label.

**`--init-config`** writes this schema with every value commented out and a
one-line explanation per key, never overwriting an existing file (as today).

**Old line-based file.** `kandor` stops with an error telling the user to run
`kandor --migrate-config` when either:

- the TOML file does not exist but the old file
  (`$XDG_CONFIG_HOME/kandor/config`) does, or
- the file the configuration path points at (including via `KANDOR_CONFIG`)
  fails to parse as TOML and its first directive line is a known old-format
  directive.

`--migrate-config` reads the old file (the one named by `KANDOR_CONFIG` if
that file is in the old format, otherwise the default old location), writes
the equivalent `config.toml` in the same directory (never overwriting), leaves
the old file in place, and prints the new path, reminding the user to update
`KANDOR_CONFIG` if they set it. My call: refusing to run is clearer than silently ignoring the
old settings, and it keeps the old parser confined to the migration code. A
legacy `agent opencode` line migrates to nothing; any other agent is an error,
as today.

## Agent abstraction

An `Agent` interface (or equivalent) supplies everything agent-specific:
binary name, the state directories to create and mount for shared and isolated
mode, seeding rules, environment variables to set and to exclude, the
subcommand names that count as subcommands for positional-directory handling,
command-line rewriting and refusals, and pre-run file preparation
(`service.json` for OpenCode). The sandbox planner knows nothing about OpenCode
beyond what the interface tells it.

Only OpenCode is implemented. There is no `--agent` option (it continues to
pass through to OpenCode's own `--agent` flag). How a user selects an agent
later (a config key, an option name that does not collide with OpenCode's) is
deliberately left for the project that re-adds agents.

## Testing

- Go unit tests for the command-line parser, configuration loading (including
  every error case and the migration), the narrowing/widening labels, the
  OpenCode command builder, and the sandbox planner. The planner should be a
  function from resolved inputs to a `bwrap` argument list so it can be tested
  without starting a sandbox (filesystem checks use temporary directories).
- `tests/smoke.sh` keeps running against the built binary and must pass. Its
  few uses of the old configuration format are updated to TOML. `tests/unit.sh`
  sources the Bash script, so it is replaced by the Go tests and removed.
- `make check` runs `go vet`, the Go tests and the smoke tests.

## Repository layout and build

- First step: commit the current OpenCode-only working tree (script, tests,
  README) on the `opencode-only` branch so the reference implementation is in
  git history before the port starts. The port is done on a new branch from
  that commit. No Claude co-author line on any commit.

- The Bash script is removed once the Go binary passes the smoke tests; it
  stays in git history and on the `opencode-only` branch.
- `make build` produces the binary (location is the builder's call, but not a
  file named `kandor` at the repository root that would be confused with the
  old script; ignore it in `.gitignore`). `make install` builds and installs
  it. The smoke tests are pointed at the built binary.
- `--version` reports 0.3.0 (my call: the configuration format change is a
  breaking change).
- README updated for the new configuration file, `--migrate-config`, building
  from source, and the Go requirement for building.

## Left to the builder

- Package layout and internal names.
- How `--help` text and `--init-config` comments are generated (by hand or from
  the schema).
- Signal handling details, provided the process-handling behaviour above holds.
- Whether the free-port helper keeps the `python3` path (Go can bind port 0
  directly, which is simpler).
- Ordering and wording of warnings, as long as each existing warning still
  appears when its condition occurs.
