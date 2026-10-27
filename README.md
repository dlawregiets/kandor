# kandor

> [!CAUTION]
> This is a vibe coded sandbox security tool and not meant for production use.
> This is just a learning experience.

Run [OpenCode](https://opencode.ai) inside a
[bubblewrap](https://github.com/containers/bubblewrap) sandbox on Linux.

`kandor` is named after the Bottle City of Kandor: a whole environment sealed
inside a container. It is a small Go program that runs `bwrap`. It gives
OpenCode (and everything it spawns: shells, language servers, MCP servers,
plugins, build tools) a private view of the host:

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

By default `kandor` shares the host's OpenCode data directory (the session
database and stored logins). Use `--isolated` (or `--profile NAME`) for a
**sandbox-only profile** under `~/.local/share/kandor/<profile>`: the host
config is copied in once, so plugins and settings keep working, but stored
credentials, host sessions and the host session database are not exposed.

`kandor` currently supports OpenCode only. `KANDOR_BIN` overrides the path
to the `opencode` binary.

Create a commented configuration file at `~/.config/kandor/config.toml` (or
`$KANDOR_CONFIG`) with:

```sh
kandor --init-config
```

`kandor` consumes its own options (`-C`, `--profile`, `--env`, ...) before
OpenCode sees them. Pass flags that collide with kandor options after `--`.
Options `kandor` does not know, such as OpenCode's `--agent` or
`--continue`, are passed through unchanged.

## Requirements

- Linux
- `bubblewrap` (`bwrap`) 0.5+, 0.9+ recommended
- `git`, optional (used to find and protect the project's repository)
- Go 1.27+ to build from source
- Working unprivileged user namespaces, or a setuid `bwrap` installation
- `opencode` in `PATH` (or set `KANDOR_BIN`)

Check that bubblewrap works on your system:

```sh
bwrap --ro-bind / / --unshare-user-try -- /bin/true && echo ok
```

If that fails, see [Troubleshooting](#troubleshooting).

## Install

Build and install it for the current user with:

```sh
make install
```

This builds `bin/kandor` and installs it to `~/.local/bin`. Override the location with `PREFIX` or
`BINDIR`:

```sh
make install PREFIX=/usr/local   # requires write access to /usr/local
make install BINDIR="$HOME/bin"
```

Remove it with `make uninstall`. `make build` only builds `bin/kandor`;
the result is a single static binary you can copy anywhere.

## Quick start

```sh
# Start the TUI in the current project (shares the host's sessions)
kandor

# Use a sandbox-only profile instead
kandor --isolated
kandor --isolated --profile work

# Pick a project directory
kandor -C ~/code/my-project

# Non-interactive run
kandor run "fix the failing tests"

# Log in (OAuth providers; env API keys are forwarded automatically)
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

### Isolated profile (`--isolated`)

```sh
kandor --isolated           # profile "default"
kandor --profile work       # implies --isolated
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
`config/opencode` once (except `service.json`). Stored logins live in the
data directory (`~/.local/share/opencode/auth.json`) and are not copied.
Note that `opencode.json` itself is copied, so API keys written directly into
it reach the profile.

This makes plugins and settings work without copying the host's sessions or
stored OAuth credentials. Delete the profile directory to start over, or use
`--no-seed` to start with an empty config. Do not run two sandboxes with the
same profile at the same time; use different `--profile` names
instead.

Environment variables of known LLM providers are forwarded automatically to
OpenCode: those starting with `OPENAI_`, `ANTHROPIC_`, `OPENROUTER_`,
`GEMINI_`, `GOOGLE_`, `VERTEX_`, `AWS_`, `AZURE_`, `DEEPSEEK_`, `XAI_`,
`GROQ_`, `MISTRAL_`, `COHERE_`, `PERPLEXITY_`, `TOGETHER_`, `FIREWORKS_`,
`CEREBRAS_`, `OLLAMA_`, `LMSTUDIO_`, `HF_`, `REPLICATE_`, `MOONSHOT_`,
`KIMI_`, `ZAI_`, `ZHIPU_`, `MINIMAX_`, `DASHSCOPE_` or `LITELLM_`, plus
proxy and CA settings. Note that `AWS_*`, `AZURE_*` and `GOOGLE_*` include
cloud credentials (for Bedrock, Azure OpenAI and Vertex). Other secrets, such
as `STRIPE_SECRET_KEY` or `GITHUB_TOKEN`, are not forwarded; pass them
explicitly with `--env NAME` if OpenCode needs them. In an isolated profile,
OAuth logins are stored in the profile; log in once per profile:

```sh
kandor --isolated auth login
```

### Shared profile (default)

```sh
kandor
kandor --shared              # same as the default, prints a warning
```

This shares the host's live `~/.local/share/opencode`
read-write: the session database and the stored provider logins in
`auth.json`. Config, state and cache are copied or created in a
temporary directory for the duration of the run; the host's cached `rg`
binary is mounted read-only so OpenCode does not have to download it again.
The host's `service.json` password and service registration stay outside the
sandbox, even when a mount includes their directories: kandor covers them
with the sandbox's private copies.

To keep OpenCode's config changes (models, plugins, agents), mount the host
directory read-write, for example `--rw ~/.config/opencode` or
`read_write = ["~/.config/opencode"]` under `[mounts]`. OpenCode then uses that
directory directly instead of a temporary copy. The same works for
`~/.local/state/opencode` and `~/.cache/opencode`. An isolated profile always
uses its own copy.

Sharing is useful for continuing host sessions, but it is **less isolated**:

- The host's session data (all projects, prompts and tool output) is
  readable inside the sandbox. Treat it as trusted with anything that has
  ever appeared in a host session; that can include credentials or the host
  service password if they were ever printed.
- The sandbox can read the stored provider logins in `auth.json` and modify
  host sessions. Config, plugin and cache changes are made in a temporary
  copy and discarded when the run exits.
- Anything the sandbox writes into the host data directory is read by
  OpenCode when you next run it on the host, outside the sandbox.
- Host OpenCode must not be running a conflicting migration; SQLite WAL
  generally allows concurrent access, but avoid concurrent heavy writes.

`kandor` warns whenever it shares host state (the default). Use
`--isolated` for untrusted projects.

## Security model

### What is isolated

| Area | Inside the sandbox |
| --- | --- |
| Host filesystem | Read-only at `/` |
| `$HOME` | Empty tmpfs; only selected paths are mounted |
| Project | Read-write (the only source tree by default) |
| OpenCode config/data/state/cache | Isolated profile (opt-in), read-write |
| `/tmp`, `/var/tmp` | Private tmpfs |
| `/run`, `/run/user/$UID` | Private; host DBus/keyring/journal sockets hidden |
| `/mnt`, `/media`, `/srv`, `/root` | Private tmpfs |
| Processes | Private PID namespace; host processes invisible |
| IPC | Private IPC namespace |
| Hostname | `kandor` |
| User namespaces | Nested user namespaces disabled (when supported) |
| Capabilities | All dropped; no-new-privs |
| Terminal | `--die-with-parent`; new session when TIOCSTI is enabled |
| Git hooks and config | Read-only unless `--no-protect-git` (see below) |

The host's OpenCode service credentials are not mounted. Isolated profiles
write their own `service.json` with a private port, and shared mode uses a
temporary config/state copy, so a compromised sandbox cannot read the
password from those service files. (In shared mode the session database
itself may contain secrets that appeared in past sessions.) Stored provider
logins live in `~/.local/share/opencode/auth.json`: an isolated profile has
its own data directory, so a sandbox cannot read the host's logins, but the
default shared mode mounts the host data directory, including `auth.json`.

Git worktrees and submodules work: the common git directory is mounted
read-write only after validating the worktree's `.git` back-reference, so an
untrusted project cannot point its `.git` file at an arbitrary host
directory. `--rw` is available if you need to override this for a trusted
repository. A repository rooted at `$HOME` or above (for example a dotfiles
repository) is never mounted.

Because the project's git directory is writable, a compromised sandbox could
otherwise plant a hook, or a `core.fsmonitor` / filter command in
`.git/config`, that runs on the host the next time you run `git` there. By
default `kandor` therefore:

- mounts `.git/hooks` and `.git/config` read-only (creating an empty
  `hooks` directory if the repository has none);
- turns `.git`, a worktree's `.git` file and its per-worktree `commondir`
  into mount points or read-only files, so they cannot be renamed or
  rewritten to point at a repository of the sandbox's choosing;
- after the run, moves aside a `.git/commondir` the sandbox created (git
  would follow it to another repository's config), and warns if the sandbox
  created a new repository in the project.

This breaks git commands that write the repository config, such as
`git config`, `git push -u`, `git remote add` and `git branch
--set-upstream-to`. Run those on the host, or pass `--no-protect-git` for a
trusted project. The protection does not cover hooks kept inside the project
(`core.hooksPath` pointing at, say, `.husky/`), nested repositories the
agent creates, or other files the host runs later (`Makefile`,
`package.json` scripts, `.envrc`, editor tasks). Review changes to those
before running them on the host.

### What is not isolated

- **The kernel.** This is a namespace sandbox, not a virtual machine. Kernel
  bugs, `bwrap` bugs and hardware flaws are outside its scope.
- **The network.** By default the sandbox shares the host network namespace.
  It can reach the internet (needed for LLM APIs), but also localhost and LAN
  services. The host OpenCode service password is not exposed in either mode;
  other local services may be unauthenticated. Sharing the network namespace
  also shares **abstract Unix sockets**, which are not files and cannot be
  hidden by mounts: X11 (`@/tmp/.X11-unix/X0`) and some D-Bus and input
  method buses stay reachable. If your X server accepts local connections
  without a cookie (for example `xhost +SI:localuser:$USER`, common with
  Xwayland), the sandbox can send keystrokes to host windows. Use `--no-net`
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
- **The OpenCode profile.** In an isolated profile, the sandbox can read and
  modify its own sessions, config, plugins and cache. That is intentional.
  In the default shared mode it can read and modify host sessions and read
  the stored logins; config, state and cache are private copies.
- **GUI, GPU and devices.** The sandbox gets a minimal `/dev`; there is no
  X11/Wayland, GPU, audio, FUSE or raw disk access. `--allow-userns` is
  available for tools (for example headless browsers) that need nested user
  namespaces, but it weakens the sandbox.
- **The agent's own permission prompts.** This wrapper isolates the process;
  it does not replace OpenCode's permission system. Keep using it.

## Options

Run `kandor --help` for the full list. Common options:

| Option | Meaning |
| --- | --- |
| `--init-config` | Create a commented config file and exit |
| `--migrate-config` | Convert the old line-based config file to `config.toml` |
| `-C, --dir DIR` | Project directory (default: `$PWD`) |
| `--ro PATH` | Extra read-only mount (repeatable) |
| `--rw PATH` | Extra read-write mount (repeatable) |
| `--env NAME[=VALUE]` | Pass an environment variable (repeatable) |
| `--isolated` | Use a sandbox-only profile (opt-in) |
| `--shared` | Share the host's OpenCode data (default) |
| `--profile NAME` | Isolated profile name (implies `--isolated`) |
| `--no-seed` | Do not copy host config into a new profile |
| `--no-net` | No network (`--unshare-net`) |
| `--ssh` | Mount `~/.ssh` read-only |
| `--ssh-agent` | Forward `$SSH_AUTH_SOCK` read-only |
| `--git-credentials` | Mount `~/.git-credentials` read-only |
| `--netrc`, `--npmrc` | Mount the corresponding credential file read-only |
| `--no-gitconfig` | Do not mount git config |
| `--no-protect-git` | Leave the repository's hooks and config writable |
| `--allow-userns` | Allow nested user namespaces |
| `--new-session` / `--no-new-session` | TIOCSTI protection vs. terminal resizing |
| `--shell` | Run a shell inside the sandbox |
| `--dry-run` | Print the bwrap command (redacts environment secrets) |

For safety, `--ro` and `--rw` refuse `/`, `$HOME`, ancestors of `$HOME`,
`/run/user/$UID`, and top-level host directories such as `/tmp`, `/etc` or
`/usr`. Mount a subdirectory or file instead (for example `--rw ~/go`).

### Configuration file

`kandor` reads `$KANDOR_CONFIG`, or `$XDG_CONFIG_HOME/kandor/config.toml`
(default `~/.config/kandor/config.toml`). It is a TOML file with one section
per topic; every key is optional:

```toml
[mounts]
read_only  = ["~/.config/gcloud"]
read_write = ["~/go", "~/.cache/go-build"]

[env]
pass = ["GITHUB_TOKEN"]          # copied from your shell

[env.set]
GOPROXY = "https://proxy.golang.org,direct"

[sandbox]
network      = true              # false = no network
allow_userns = false
new_session  = "auto"            # "auto", true or false

[credentials]                    # all off unless you turn them on
ssh             = false
ssh_agent       = false
git_credentials = false
netrc           = false
npmrc           = false

[git]
mount_config = true              # your ~/.gitconfig
protect      = true              # read-only hooks and .git/config

[profile]
isolated = false
name     = "default"             # setting it implies isolated = true
seed     = true

[agents.opencode]                # only when running OpenCode
binary = "/opt/opencode/bin/opencode"   # KANDOR_BIN overrides it

[agents.opencode.mounts]
read_only = []
```

Paths may start with `~` or `$HOME`. Unknown sections and keys, values of the
wrong type and agents other than `opencode` are errors, so a typo cannot
silently change the sandbox.

Create a commented starter file with `kandor --init-config` (it uses
`$KANDOR_CONFIG` when that is set and never overwrites an existing file).

Command-line options override the file. Lists such as mounts and
environment variables add to the file's lists. When the same path is mounted
more than once, the later layer wins, in the order: the file's top-level
sections, the `[agents.opencode]` section, the command line. So `--ro PATH`
makes a configured `read_write` path read-only for one run, and `--rw PATH`
makes a `read_only` path writable. Listing the same path as both
`read_only` and `read_write` in one layer is an error.

#### Migrating from the old format

Earlier versions used a line-based file at `~/.config/kandor/config`. If
that file exists and `config.toml` does not, `kandor` stops and asks you to
convert it:

```sh
kandor --migrate-config
```

This writes `config.toml` next to the old file (never overwriting) and leaves
the old file in place, so you can delete it once you have checked the result.
If `KANDOR_CONFIG` points at an old-format file, the new file is written in
the same directory and you need to point `KANDOR_CONFIG` at it.

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

The default host network allows reaching a local model server:

```sh
kandor --env OLLAMA_HOST=http://127.0.0.1:11434
```

Remember that this also exposes other host services on localhost.

### Offline

```sh
kandor --no-net
```

With no network, the agent cannot call a remote provider. Use a local model
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

### `git config` or `git push -u` fails with "Device or resource busy"

The repository's `.git/config` is mounted read-only (see
[Security model](#security-model)). Run the command on the host, or start
`kandor` with `--no-protect-git` for a trusted project.

### Plugin install fails

The Bun install cache is mounted read-only (`~/.bun`), so the sandbox cannot
poison the host's package cache. If a plugin needs to write to it, opt in:

```sh
kandor --rw ~/.bun/install/cache
```

The isolated profile's own cache (`.../cache/opencode`) is writable.

### OpenCode cannot find a session

You are using `--isolated` (or `--profile`). Host sessions are not part of
that profile. Either drop it to use the default shared state:

```sh
kandor
```

or export/import sessions with `opencode session export` / `session import`.
Isolated-profile OAuth logins also need to be repeated per profile:
`kandor --isolated auth login`.

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

### Shared mode changed my host state

Shared mode (the default) is deliberately read-write for host state.
Config, state and cache are temporary copies unless you mount them
read-write, so config/plugin changes are discarded, but sessions and stored logins in the host data directory can be
modified. Use `--isolated` for untrusted work.

## Uninstall

```sh
rm ~/.local/bin/kandor
rm -rf ~/.local/share/kandor   # isolated profiles
rm -rf ~/.config/kandor        # optional config file
```

## Tests

`make test` runs `go vet` and the Go tests. `make check` also builds a static
binary and runs the smoke tests:

```sh
make test
make check
# or: go test ./... && make build && ./tests/smoke.sh
```

The Go tests cover the command-line parser, configuration loading and
migration, the mount planner, git protection, environment filtering and the
OpenCode command builder without starting a sandbox. `tests/smoke.sh` runs
real sandboxes with the built `bin/kandor` and checks the sandbox boundary,
private `/tmp`, hidden home, read-only `/etc`, isolated profiles, extra
mounts, network isolation, host service-password isolation, git protection
and signal handling. Neither calls an LLM; the smoke tests need `bwrap` and
`opencode`.

## Continuous integration and releases

`.github/workflows/ci.yml` runs on every push and pull request. It checks
formatting, runs `go vet` and the Go tests with the race detector, and builds
static `linux/amd64` and `linux/arm64` binaries as artifacts. Feature and fix
branches can therefore be tested before they merge into `develop`, and release
or hotfix branches before they merge into `main`. The smoke tests are not run
there because they need a real `opencode` binary; run `make check` locally.

Releases follow GitFlow. Merge the release or hotfix branch into `main`, then
tag the merge commit:

```sh
git tag -a v0.4.0 -m "kandor 0.4.0"
git push origin v0.4.0
```

The pushed tag becomes the binary version; there is no version file to keep in
sync. Local and CI builds are stamped with `git describe` output instead: the
nearest tag plus any commits since it, or the commit SHA when no tag describes
the build.

`.github/workflows/release.yml` checks that the tag is a semantic version and
that stable tags are reachable from `main`; pre-release tags may also be on a
`release/*` or `hotfix/*` branch. It then runs the tests and publishes static
tarballs for both architectures plus `SHA256SUMS`. Tags with a pre-release
suffix such as `v0.4.0-rc.1` are marked as GitHub pre-releases.
