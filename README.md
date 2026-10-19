# kandor

Run [OpenCode](https://opencode.ai) inside a [bubblewrap](https://github.com/containers/bubblewrap)
sandbox on Linux.

`kandor` is named after the Bottle City of Kandor: a whole environment sealed
inside a container. It is a small Bash wrapper around `bwrap`. It gives
OpenCode (and everything OpenCode spawns: shells, language servers, MCP
servers, plugins, build tools) a private view of the host:

- `/` is mounted read-only.
- Your home directory is replaced by an empty tmpfs. Only the project and
  OpenCode's own state directories are visible.
- `/tmp`, `/var/tmp`, `/run/user/$UID`, `/mnt`, `/media`, `/srv` and `/run` are
  private. Host runtime sockets (DBus, keyring, journal, SSH agent) are hidden.
- The process gets its own user, PID, IPC, UTS and cgroup namespaces.
- Nested user namespaces are disabled when the kernel supports it.
- OpenCode is run with `--standalone` whenever the command uses a server, so
  it cannot attach to a background OpenCode service running outside the
  sandbox. The host's service credentials are never mounted; each profile
  writes its own `service.json` with a private port.

By default OpenCode also gets a **sandbox-only profile** under
`~/.local/share/kandor/<profile>`. Your host config is copied in once, so
plugins and settings keep working, but host sessions and the host session
database are not exposed. Use `--shared` if you want to share the host's
session database instead.

## Requirements

- Linux
- `bash` 4.3+
- `bubblewrap` (`bwrap`) 0.5+, 0.9+ recommended
- Working unprivileged user namespaces, or a setuid `bwrap` installation
- `opencode` in `PATH` (or set `KANDOR_BIN`)

Check that bubblewrap works on your system:

```sh
bwrap --ro-bind / / --unshare-user-try -- /bin/true && echo ok
```

If that fails, see [Troubleshooting](#troubleshooting).

## Install

Install it for the current user with:

```sh
make install
```

This installs `kandor` to `~/.local/bin`. Override the location with `PREFIX` or
`BINDIR`:

```sh
make install PREFIX=/usr/local   # requires write access to /usr/local
make install BINDIR="$HOME/bin"
```

Remove it with `make uninstall`.

Alternatively, install or symlink it manually:

```sh
install -m 0755 kandor ~/.local/bin/kandor
ln -s "$PWD/kandor" ~/.local/bin/kandor
```

## Quick start

```sh
# Start the TUI in the current project, using an isolated profile
kandor

# Pick a project directory
kandor -C ~/code/my-project

# Non-interactive run
kandor run "fix the failing tests"

# Log in once per isolated profile (OAuth providers; env API keys are
# forwarded automatically)
kandor auth login

# Offline / no network at all
kandor --no-net

# Debug shell inside the sandbox
kandor --shell

# Show the exact bwrap command (environment secrets are redacted)
kandor --dry-run
```

Options and arguments are passed to OpenCode after the first positional
argument or after `--`. A positional directory is used as the project
directory (like `-C`):

```sh
kandor run --model openrouter/anthropic/claude-sonnet-4 "review this"
kandor -- --continue
kandor --completions bash
```

> Note: `--version` is the wrapper version. Use `kandor -- --version` for
> the OpenCode version.

## Profiles

### Isolated profile (default)

```sh
kandor                      # profile "default"
kandor --profile work       # profile "work"
kandor --profile work --no-seed
```

State lives in:

```
~/.local/share/kandor/<profile>/
├── config/opencode/
├── data/opencode/
├── state/opencode/
└── cache/opencode/
```

The first time a profile is used, `~/.config/opencode` is copied into
`config/opencode` (except `service.json`). This makes plugins and settings
work without copying the host's session database. Delete the profile
directory to start over, or use `--no-seed` to start with an empty config.
Do not run two sandboxes with the same profile at the same time; use
different `--profile` names instead.

API keys from the environment (for example `OPENROUTER_API_KEY`,
`ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OPENAI_API_KEY`, `AWS_*`, `AZURE_*`)
are forwarded automatically. OAuth logins are stored in the profile's
database; run `kandor auth login` once per profile.

### Shared profile

```sh
kandor --shared
```

This shares the host's live `~/.local/share/opencode` (the session database).
Config, state and cache are copied or created in a temporary directory for
the duration of the run; the host's cached `rg` binary is mounted read-only
so OpenCode does not have to download it again. The host's `service.json`
password and service registration stay outside the sandbox. It is useful for
continuing host sessions, but it is **less isolated**:

- The host session database (all projects, prompts and tool output) is
  readable inside the sandbox. Treat it as trusted with anything that has
  ever appeared in a host session; that can include credentials or the host
  service password if they were ever printed.
- The sandbox can modify host sessions. Config, plugin and cache changes are
  made in the temporary copy and are discarded when the run exits.
- Host OpenCode must not be running a conflicting migration; SQLite WAL
  generally allows concurrent access, but avoid concurrent heavy writes.

`kandor` prints a warning for `--shared`. Prefer the isolated profile for
untrusted projects.

## Security model

### What is isolated

| Area | Inside the sandbox |
| --- | --- |
| Host filesystem | Read-only at `/` |
| `$HOME` | Empty tmpfs; only selected paths are mounted |
| Project | Read-write (the only source tree by default) |
| OpenCode config/data/state/cache | Isolated profile, read-write |
| `/tmp`, `/var/tmp` | Private tmpfs |
| `/run`, `/run/user/$UID` | Private; host DBus/keyring/journal sockets hidden |
| `/mnt`, `/media`, `/srv`, `/root` | Private tmpfs |
| Processes | Private PID namespace; host processes invisible |
| IPC | Private IPC namespace |
| Hostname | `kandor` |
| User namespaces | Nested user namespaces disabled (when supported) |
| Capabilities | All dropped; no-new-privs |
| Terminal | `--die-with-parent`; new session when TIOCSTI is enabled |

The host's OpenCode service credentials are not mounted. Isolated profiles
write their own `service.json` with a private port, and `--shared` uses a
temporary config/state copy, so a compromised sandbox cannot read the
password from those service files. (In `--shared` mode the session database
itself may contain secrets that appeared in past sessions.)

Git worktrees and submodules work: the common git directory is mounted
read-write only after validating the worktree's `.git` back-reference, so an
untrusted project cannot point its `.git` file at an arbitrary host
directory. `--rw` is available if you need to override this for a trusted
repository.

### What is not isolated

- **The kernel.** This is a namespace sandbox, not a virtual machine. Kernel
  bugs, `bwrap` bugs and hardware flaws are outside its scope.
- **The network.** By default the sandbox shares the host network namespace.
  It can reach the internet (needed for LLM APIs), but also localhost and LAN
  services. The host OpenCode service password is not exposed in the isolated
  profile, but other local services may be unauthenticated. Use `--no-net`
  for offline work, or firewall host services if this matters.
- **Read-only host files.** `/` is read-only and the home directory and
  common runtime/scratch directories are hidden, but readable host files in
  places such as `/opt`, `/etc` and `/var/lib` are still visible. Do not keep
  secrets in world-readable system locations.
- **Data you expose.** The project directory is read-write. Common user tool
  directories (`~/.local/bin`, `~/bin`, `~/go/bin`, `~/.cargo/bin`,
  `~/.bun`) are mounted read-only so tools work; anything stored there is
  readable. Git config (`~/.gitconfig`, `~/.config/git`) is mounted read-only
  unless `--no-gitconfig`. `--ro`/`--rw` mounts, `--ssh`, `--ssh-agent`,
  `--git-credentials`, `--netrc`, `--npmrc` and `--env` deliberately expose
  more. If `GOOGLE_APPLICATION_CREDENTIALS`, `AWS_SHARED_CREDENTIALS_FILE`,
  `AWS_CONFIG_FILE`, `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE` or a similar path
  variable is set, that file is mounted read-only so the provider can use it.
  Environment API keys are visible to the agent (it needs them to call the
  provider).
- **The OpenCode profile.** In the default isolated profile, the sandbox can
  read and modify its own sessions, config, plugins and cache. That is
  intentional. In `--shared` mode it can read and modify the host session
  database; config, state and cache are private copies.
- **GUI, GPU and devices.** The sandbox gets a minimal `/dev`; there is no
  X11/Wayland, GPU, audio, FUSE or raw disk access. `--allow-userns` is
  available for tools (for example headless browsers) that need nested user
  namespaces, but it weakens the sandbox.
- **OpenCode's own permission prompts.** This wrapper isolates the process;
  it does not replace OpenCode's permission system. Keep using it.

## Options

Run `kandor --help` for the full list. Common options:

| Option | Meaning |
| --- | --- |
| `-C, --dir DIR` | Project directory (default: `$PWD`) |
| `--ro PATH` | Extra read-only mount (repeatable) |
| `--rw PATH` | Extra read-write mount (repeatable) |
| `--env NAME[=VALUE]` | Pass an environment variable (repeatable) |
| `--isolated` | Use an isolated profile (default) |
| `--shared` | Share host sessions; private config/state/cache |
| `--profile NAME` | Isolated profile name |
| `--no-seed` | Do not copy host config into a new profile |
| `--no-net` | No network (`--unshare-net`) |
| `--ssh` | Mount `~/.ssh` read-only |
| `--ssh-agent` | Forward `$SSH_AUTH_SOCK` read-only |
| `--git-credentials` | Mount `~/.git-credentials` read-only |
| `--netrc`, `--npmrc` | Mount the corresponding credential file read-only |
| `--no-gitconfig` | Do not mount git config |
| `--allow-userns` | Allow nested user namespaces |
| `--new-session` / `--no-new-session` | TIOCSTI protection vs. terminal resizing |
| `--shell` | Run a shell inside the sandbox |
| `--dry-run` | Print the bwrap command (redacts environment secrets) |

For safety, `--ro` and `--rw` refuse `/`, `$HOME`, ancestors of `$HOME`,
`/run/user/$UID`, and top-level host directories such as `/tmp`, `/etc` or
`/usr`. Mount a subdirectory or file instead (for example `--rw ~/go`).

### Configuration file

`~/.config/kandor/config` (or `$KANDOR_CONFIG`) supports one
directive per line:

```sh
# Extra mounts
rw ~/go
rw ~/.cache/go-build
ro ~/.config/gcloud

# Environment
env GOPROXY=https://proxy.golang.org,direct

# Defaults
no-net
profile work
no-seed
ssh
ssh-agent
git-credentials
netrc
npmrc
no-gitconfig
allow-userns
no-new-session
```

Command-line options are applied after the file. A later mount of the same
path wins, so an `--rw PATH` on the command line overrides `ro PATH` in the
config file.

## Typical setups

### Rust/Go/Node builds

Builds need their toolchains and caches. The toolchains in `/usr` are
read-only and available. Add the caches you need explicitly:

```sh
kandor \
  --rw ~/.cargo \
  --rw ~/go \
  --rw ~/.cache/go-build \
  --rw ~/.npm \
  run "build and test"
```

Only add directories you are comfortable exposing to the agent.

### Git push over SSH

```sh
kandor --ssh --ssh-agent -- run "commit and push"
```

`~/.ssh` is read-only and the agent socket is forwarded read-only. The agent
can use your keys but cannot read or modify the key files. If you use HTTPS
credentials, add `--git-credentials`.

### Local model or proxy on the host

The default shared network allows reaching a local model server:

```sh
kandor --env OLLAMA_HOST=http://127.0.0.1:11434
```

Remember that this also exposes other host services on localhost.

### Offline

```sh
kandor --no-net
```

With no network, OpenCode cannot call a remote provider. Use a local model
server reachable without the network namespace, or run without `--no-net` and
restrict host services.

## Troubleshooting

### `bubblewrap cannot create a user namespace`

Check:

```sh
sysctl kernel.unprivileged_userns_clone
bwrap --ro-bind / / --unshare-user-try -- /bin/true
```

- Debian/Ubuntu: `sudo sysctl -w kernel.unprivileged_userns_clone=1`
- Ubuntu 23.10+: AppArmor may restrict unprivileged user namespaces. Install
  a `bubblewrap` package that uses setuid, or adjust the AppArmor profile.
- Some distributions ship `bwrap` setuid root; `kandor` falls back to
  `--unshare-user-try` automatically.
- Inside a container, namespace creation may be blocked by the container's
  seccomp/AppArmor policy. Run the container with the needed permissions.

### `unshare: unshare failed: No space left on device`

This is the expected result of `--disable-userns` (nested user namespaces are
disabled). If a tool such as a browser needs them, use `--allow-userns`.

### Plugin install fails

The Bun install cache is mounted read-only (`~/.bun`), so the sandbox cannot
poison the host's package cache. If a plugin needs to write to it, opt in:

```sh
kandor --rw ~/.bun/install/cache
```

The isolated profile's own cache (`.../cache/opencode`) is writable.

### OpenCode cannot find a session

You are using the isolated profile. Host sessions are not copied. Either:

```sh
kandor --shared
```

or export/import sessions with `opencode session export` / `session import`.
OAuth logins also need to be repeated once per profile with
`kandor auth login`.

### Terminal resizing / TIOCSTI

`TIOCSTI` lets a process inject keystrokes into the controlling terminal. If
the kernel allows it (`/proc/sys/dev/tty/legacy_tiocsti` is `1` or missing),
`kandor` defaults to `--new-session` to prevent that. A side effect is
that the TUI may not receive `SIGWINCH`, so terminal resizes are not
repainted. On kernels where TIOCSTI is disabled (the common default),
`--new-session` is off and resizing works normally.

Override the choice explicitly:

```sh
kandor --no-new-session   # resizing, but TIOCSTI is possible
kandor --new-session      # TIOCSTI protection, resize may not work
```

### `--shared` changed my host state

`--shared` is deliberately read-write for the host session database. Config,
state and cache are temporary copies, so config/plugin/cache changes are
discarded, but sessions can be modified. Use the default isolated profile for
untrusted work.

## Uninstall

```sh
rm ~/.local/bin/kandor
rm -rf ~/.local/share/kandor   # isolated profiles
rm -rf ~/.config/kandor        # optional config file
```

## Tests

A smoke test is included:

```sh
make check
# or: ./tests/smoke.sh
```

It checks the sandbox boundary, private `/tmp`, hidden home, read-only `/etc`,
isolated profiles, extra mounts, network isolation and host service-password
isolation. It does not call an LLM.
