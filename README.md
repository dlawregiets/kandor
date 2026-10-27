# kandor

Run [OpenCode](https://opencode.ai), [Pi](https://pi.dev) or
[oh-my-pi](https://github.com/can1357/oh-my-pi) (`omp`) inside a
[bubblewrap](https://github.com/containers/bubblewrap) sandbox on Linux.

`kandor` is named after the Bottle City of Kandor: a whole environment sealed
inside a container. It is a small Bash wrapper around `bwrap`. It gives the
agent (and everything it spawns: shells, language servers, MCP servers,
plugins, build tools) a private view of the host:

- `/` is mounted read-only.
- Your home directory is replaced by an empty tmpfs. Only the project and
  the agent's own state directories are visible.
- `/tmp`, `/var/tmp`, `/run/user/$UID`, `/mnt`, `/media`, `/srv` and `/run` are
  private. Host runtime sockets (DBus, keyring, journal, SSH agent) are hidden.
- The process gets its own user, PID, IPC, UTS and cgroup namespaces.
- Nested user namespaces are disabled when the kernel supports it.
- OpenCode is run with `--standalone` whenever the command uses a server, so
  it cannot attach to a background OpenCode service running outside the
  sandbox. The host's service credentials are never mounted; each profile
  writes its own `service.json` with a private port.

By default `kandor` shares the host's agent state: OpenCode uses the host
session database, while Pi and omp mount their whole `~/.pi/agent` and
`~/.omp` trees (including stored credentials). Use `--isolated` (or
`--profile NAME`) for a **sandbox-only profile** under
`~/.local/share/kandor/<profile>`: the host config is copied in once, so
plugins and settings keep working, but stored credentials, host sessions and
the host session database are not exposed.

## Agents

`kandor` runs OpenCode by default. Select another agent with `--agent`:

```sh
kandor --agent pi -C ~/code/my-project
kandor --agent omp -C ~/code/my-project
```

The agent's arguments are passed through after `--` (or unchanged once one is
passed on the command line). Only OpenCode treats a bare positional directory
as the project; for Pi and omp use `-C`/`--dir` (or their own `--cwd`).

With `--isolated`, each profile stores the agent's state separately:

| Agent | Profile state | Host state (shared default) |
| --- | --- | --- |
| `opencode` | `config/`, `data/`, `state/`, `cache/` as `opencode/` subtrees | `~/.config/opencode`, `~/.local/share/opencode` |
| `pi` | `<profile>/pi/agent` | `~/.pi/agent` |
| `omp` | `<profile>/omp` | `~/.omp` |

For Pi and omp the host config is copied into an isolated profile once, but
credential files (`auth.json`, `mcp-auth.json`, `agent.db` and similar) and
host sessions are never copied. Logins therefore differ by mode:

```sh
kandor auth login                 # OpenCode: host data dir, shared by default
kandor --agent omp -- login       # omp: host ~/.omp, shared by default
kandor --agent pi                 # Pi: /login in the TUI, host ~/.pi/agent
kandor --agent pi --isolated      # Pi: /login inside the isolated profile
```

Pi and omp do not have OpenCode's service registration; `--standalone` and
the `service.json` handling only apply to OpenCode.

The default agent can come from `KANDOR_AGENT`, the `agent` directive in
the configuration file, or `--agent` on the command line. Precedence is
`--agent` > `KANDOR_AGENT` > config file > `opencode`. `KANDOR_BIN`
overrides the binary path for the selected agent.

Create a commented configuration file at `~/.config/kandor/config` (or
`$KANDOR_CONFIG`) with:

```sh
kandor --init-config                 # default agent: opencode
kandor --init-config --agent omp     # make omp the default
```

To change the saved default later without editing the file by hand, use
`kandor --set-agent`:

```sh
kandor --set-agent pi
```

It creates the file if needed and replaces the existing `agent` directive
while leaving the rest of the file untouched. `--agent` still overrides the
saved default for a single run.

`kandor` consumes its own options (`-C`, `--profile`, `--env`, ...) before
the agent sees them. Pass flags that collide with kandor options, such as
omp's `--profile`, after `--`:

```sh
kandor --agent omp -C ~/code/proj -- --profile inner
```

## Requirements

- Linux
- `bash` 4.4+
- `bubblewrap` (`bwrap`) 0.5+, 0.9+ recommended
- Working unprivileged user namespaces, or a setuid `bwrap` installation
- `opencode`, `pi` or `omp` in `PATH` (or set `KANDOR_BIN`)

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
# Start the TUI in the current project (shares the host's sessions)
kandor

# Use a sandbox-only profile instead
kandor --isolated
kandor --isolated --profile work

# Pick a project directory
kandor -C ~/code/my-project

# Run Pi or oh-my-pi instead of OpenCode
kandor --agent pi -C ~/code/my-project
kandor --agent omp -C ~/code/my-project

# Non-interactive run
kandor run "fix the failing tests"
kandor --agent omp -- -p "fix the failing tests"

# Log in (OAuth providers; env API keys are forwarded automatically)
kandor auth login
kandor --agent omp -- login anthropic
kandor --agent pi --isolated      # then /login in the TUI

# Offline / no network at all
kandor --no-net

# Debug shell inside the sandbox
kandor --shell

# Show the exact bwrap command (environment secrets are redacted)
kandor --dry-run
```

Options and arguments are passed to the agent after the first positional
argument or after `--`. For OpenCode, a positional directory is used as the
project directory (like `-C`):

```sh
kandor run --model openrouter/anthropic/claude-sonnet-4 "review this"
kandor -- --continue
kandor --completions bash
```

Pi and omp take prompts and `@files` positionally, so use `-C` for the
project and pass their arguments after `--`:

```sh
kandor --agent pi -C ~/code/proj -- "review this"
kandor --agent omp -C ~/code/proj -- --model opus "review this"
```

> Note: `--version` is the wrapper version. Use `kandor -- --version` for
> the agent version.

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
├── cache/opencode/
├── pi/agent/          # Pi
└── omp/               # oh-my-pi
```

The first time a profile is used, the host config is copied in once:

- OpenCode: `~/.config/opencode` into `config/opencode` (except
  `service.json`).
- Pi: from `~/.pi/agent` into `pi/agent`, only `settings.json`,
  `keybindings.json`, `AGENTS.md`, `SYSTEM.md`, `APPEND_SYSTEM.md` and the
  `extensions`, `skills`, `prompts`, `themes`, `npm`, `git`, `bin` and
  `memory` directories. Credentials (`auth.json`, `mcp-auth.json`), sessions
  and anything else (including `models.json`, which can hold API keys) stay
  on the host; `kandor` lists what it skipped so you can copy it by hand.
- omp: `~/.omp/agent/config.yml`, settings, commands, prompts, skills,
  themes, tools, modules and memories, plus `~/.omp/plugins`, into `omp/`.
  The credential/session databases (`agent.db`, `history.db`, `models.db`,
  `sessions/`) are never copied.

This makes plugins and settings work without copying the host's sessions or
stored OAuth credentials. Delete the profile directory to start over, or use
`--no-seed` to start with an empty config. Do not run two sandboxes with the
same profile and agent at the same time; use different `--profile` names
instead.

Environment variables of known LLM providers are forwarded automatically to
every agent: those starting with `OPENAI_`, `ANTHROPIC_`, `OPENROUTER_`,
`GEMINI_`, `GOOGLE_`, `VERTEX_`, `AWS_`, `AZURE_`, `DEEPSEEK_`, `XAI_`,
`GROQ_`, `MISTRAL_`, `COHERE_`, `PERPLEXITY_`, `TOGETHER_`, `FIREWORKS_`,
`CEREBRAS_`, `OLLAMA_`, `LMSTUDIO_`, `HF_`, `REPLICATE_`, `MOONSHOT_`,
`KIMI_`, `ZAI_`, `ZHIPU_`, `MINIMAX_`, `DASHSCOPE_` or `LITELLM_`, plus
proxy and CA settings. Note that `AWS_*`, `AZURE_*` and `GOOGLE_*` include
cloud credentials (for Bedrock, Azure OpenAI and Vertex). Other secrets, such
as `STRIPE_SECRET_KEY` or `GITHUB_TOKEN`, are not forwarded; pass them
explicitly with `--env NAME` if the agent needs them. In an isolated profile, OAuth
logins are stored in the profile; log in once per profile:

```sh
kandor --isolated auth login     # OpenCode
kandor --isolated --agent omp -- login     # omp (pick a provider)
kandor --isolated --agent pi                # Pi: use /login in the TUI
```

### Shared profile (default)

```sh
kandor
kandor --shared              # same as the default, prints a warning
kandor --agent omp --shared
```

For OpenCode, this shares the host's live `~/.local/share/opencode`
read-write: the session database and the stored provider logins in
`auth.json`. Config, state and cache are copied or created in a
temporary directory for the duration of the run; the host's cached `rg`
binary is mounted read-only so OpenCode does not have to download it again.
The host's `service.json` password and service registration stay outside the
sandbox.

For Pi and omp the **entire** host agent tree (`~/.pi/agent`, `~/.omp`) is
mounted read-write: sessions, settings, caches and stored provider
credentials are all shared, and config/plugin/cache changes are written to
the host tree. Use `--isolated` to keep credentials and configuration
private instead.

Sharing is useful for continuing host sessions, but it is **less isolated**:

- The host's session data (all projects, prompts and tool output) is
  readable inside the sandbox. Treat it as trusted with anything that has
  ever appeared in a host session; that can include credentials or the host
  service password if they were ever printed.
- The sandbox can modify host sessions and host agent state. For OpenCode,
  config, plugin and cache changes are made in a temporary copy and
  discarded when the run exits. For Pi and omp they are written to the host
  `~/.pi/agent` / `~/.omp` trees, and the sandbox can also read the
  credentials stored there (`auth.json`, `mcp-auth.json`, `agent.db`).
- **Code planted there runs outside the sandbox later.** For Pi and omp,
  extensions, plugins, skills and tools that the sandbox installs into
  `~/.pi/agent` / `~/.omp` are loaded by the agent the next time you run it
  directly on the host, unsandboxed. A compromised sandbox in shared mode can
  therefore persist and escape.
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
| Pi agent directory | Isolated profile at `~/.pi/agent` (opt-in), read-write |
| omp config root | Isolated profile at `~/.omp` (opt-in), read-write |
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
itself may contain secrets that appeared in past sessions.) Pi and omp store
provider credentials in their agent directories (`~/.pi/agent/auth.json`,
`~/.pi/agent/mcp-auth.json`, `~/.omp/agent/agent.db`); an isolated profile
replaces those directories, so a sandbox cannot read the host's stored
credentials. In the default shared mode, both Pi and omp expose their whole
host trees (`~/.pi/agent`, including `auth.json` and `mcp-auth.json`, and
`~/.omp`, including `agent.db`).

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
- **The agent profile.** In an isolated profile, the sandbox can read and
  modify its own sessions, config, plugins and cache. That is intentional.
  In the default shared mode it can read and modify host sessions; config,
  state and cache are private copies for OpenCode. For Pi and omp the whole
  host `~/.pi/agent` / `~/.omp` tree is shared, including stored credentials.
- **GUI, GPU and devices.** The sandbox gets a minimal `/dev`; there is no
  X11/Wayland, GPU, audio, FUSE or raw disk access. `--allow-userns` is
  available for tools (for example headless browsers) that need nested user
  namespaces, but it weakens the sandbox.
- **The agent's own permission prompts.** This wrapper isolates the process;
  it does not replace the agent's permission system. Keep using it. omp also
  spawns local daemons and loopback bridges; those stay inside the sandbox
  namespaces (with `--no-net` the sandbox loopback is isolated as well).

## Options

Run `kandor --help` for the full list. Common options:

| Option | Meaning |
| --- | --- |
| `--agent NAME` | `opencode` (default), `pi` or `omp` |
| `--init-config` | Create a commented config file and exit |
| `--set-agent NAME` | Save `NAME` as the default agent in the config file |
| `-C, --dir DIR` | Project directory (default: `$PWD`) |
| `--ro PATH` | Extra read-only mount (repeatable) |
| `--rw PATH` | Extra read-write mount (repeatable) |
| `--env NAME[=VALUE]` | Pass an environment variable (repeatable) |
| `--isolated` | Use a sandbox-only profile (opt-in) |
| `--shared` | Share host agent state (default) |
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
agent omp
no-net
profile work
no-seed
ssh
ssh-agent
git-credentials
netrc
npmrc
no-gitconfig
no-protect-git
allow-userns
no-new-session
```

Create a starter file with `kandor --init-config` (optionally with
`--agent NAME`) or change the saved default with `kandor --set-agent NAME`.
Those helpers use `$KANDOR_CONFIG` when it is set. Command-line options are
applied after the file. A later mount of the same path wins, so an
`--rw PATH` on the command line overrides `ro PATH` in the config file.

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

The isolated profile's own cache (`.../cache/opencode`) is writable. Pi and
omp install extensions and plugins into their profile directories
(`.../pi/agent`, `.../omp/plugins`), which are writable. In shared mode Pi
and omp write into the host `~/.pi/agent` and `~/.omp` trees instead.

### The agent cannot find a session

You are using `--isolated` (or `--profile`). Host sessions are not part of
that profile. Either drop it to use the default shared state:

```sh
kandor
kandor --agent pi
kandor --agent omp
```

or export/import sessions with the agent's own tooling (`opencode session
export` / `session import`, `omp session pack` / `unpack`). Isolated-profile
OAuth logins also need to be repeated per profile: `kandor --isolated auth
login`, `kandor --isolated --agent omp -- login`, or `/login` inside Pi.

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

Shared mode (the default) is deliberately read-write for host state. For
OpenCode, config, state and cache are temporary copies, so config/plugin
changes are discarded, but sessions can be modified. For Pi and omp the
entire host `~/.pi/agent` / `~/.omp` tree is writable, so settings, plugins,
caches and stored credentials can change too. Use `--isolated` for untrusted
work.

## Uninstall

```sh
rm ~/.local/bin/kandor
rm -rf ~/.local/share/kandor   # isolated profiles
rm -rf ~/.config/kandor        # optional config file
```

## Tests

Two harnesses are included; `make check` runs both:

```sh
make check
# or: ./tests/unit.sh && ./tests/smoke.sh
```

`tests/unit.sh` sources `kandor` and exercises the command-line parser, the
config loader, the mount planner and the agent command builder without
starting a sandbox. `tests/smoke.sh` runs real sandboxes and checks the
sandbox boundary, private `/tmp`, hidden home, read-only `/etc`, isolated
profiles, extra mounts, network isolation and host service-password
isolation. Neither calls an LLM; the smoke tests need `bwrap` and
`opencode`.
