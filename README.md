# kandor

> [!CAUTION]
> This is a vibe coded sandbox security tool and not meant for production use.
> This is just a learning experience.

Run a coding agent ([OpenCode](https://opencode.ai), Claude Code, pi,
Copilot, or one you define) inside a
[bubblewrap](https://github.com/containers/bubblewrap) sandbox on Linux.

`kandor` is named after the Bottle City of Kandor: a whole environment sealed
inside a container. It is a small Go program that runs `bwrap`. It gives the
agent (and everything it spawns: shells, language servers, MCP servers,
plugins, build tools) a private view of the host:

- `/` is mounted read-only.
- Your home directory is replaced by an empty tmpfs. Only the project and
  the agent's own state directories are visible.
- `/tmp`, `/var/tmp`, `/run/user/$UID`, `/mnt`, `/media`, `/srv` and `/run` are
  private. Host runtime sockets (DBus, keyring, journal, SSH agent) are hidden.
- The process gets its own user, PID, IPC, UTS and cgroup namespaces.
- Nested user namespaces are disabled when the kernel supports it.
- OpenCode (the default agent) is run with `--standalone` whenever the
  command uses a server, so it cannot attach to a background OpenCode
  service running outside the sandbox. The host's service credentials are
  never mounted; each profile writes its own `service.json` with a private
  port.

By default `kandor` shares the host's state the way the agent expects it:
OpenCode shares its data directory (the session database and stored logins),
and the other built-in agents have their state directories copied into the
sandbox (pi shares its installed extensions and releases, which are too large
to copy). Use `--isolated` (or `--profile NAME`) for a **sandbox-only
profile** under `~/.local/share/kandor/<profile>`: the host config is copied
in once, so plugins and settings keep working, but stored credentials, host
sessions and the host session database are not exposed.

Pick the agent with `--agent`, `KANDOR_AGENT`, `default_agent` in the
configuration, or a project's `.kandor.toml`. `opencode` is the default;
`claude`, `pi` and `copilot` are built in, and any other harness is a
`[agents.<name>]` section in the configuration, no code needed. To sandbox
something that is not an agent at all, use `kandor exec`:

```sh
kandor --agent pi
kandor exec -- pytest -q
```

`KANDOR_BIN` overrides the agent binary path.

Create a commented configuration file at `~/.config/kandor/config.toml` (or
`$KANDOR_CONFIG`) with:

```sh
kandor --init-config
```

`kandor` consumes its own options (`-C`, `--profile`, `--env`, ...) before
the agent sees them. Pass flags that collide with kandor options after `--`.
Options `kandor` does not know, such as OpenCode's `--continue` or
`--model`, are passed through unchanged.

## Requirements

- Linux
- `bubblewrap` (`bwrap`) 0.5+, 0.9+ recommended
- `git`, optional (used to find and protect the project's repository)
- Go 1.27+ to build or `go install` from source
- Working unprivileged user namespaces, or a setuid `bwrap` installation
- the agent binary in `PATH` (default `opencode`), or set `KANDOR_BIN`

Check that bubblewrap works on your system:

```sh
bwrap --ro-bind / / --unshare-user-try -- /bin/true && echo ok
```

If that fails, see [Troubleshooting](docs/troubleshooting.md).

## Install

### With `go install`

With Go 1.27 or newer you can install from the module proxy without cloning:

```sh
go install github.com/dlawregiets/kandor/cmd/kandor@latest
```

`@latest` picks the newest tagged release, or the latest commit on the default
branch if no release tag is newer.

The binary is written to `$(go env GOPATH)/bin` (usually `~/go/bin`), so make
sure that directory is in your `PATH`:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

A `go install` build is not stamped with a release version, so `kandor
--version` reports `dev`; use `make install` or a release tarball when the
version string matters.

### From a clone

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

# Pick another built-in agent
kandor --agent pi

# Sandbox an arbitrary command instead of an agent
kandor exec -- pytest -q

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

Options and arguments are passed to the agent after the first positional
argument or after `--`. A positional directory is used as the project
directory (like `-C`):

```sh
kandor run --model openrouter/anthropic/claude-sonnet-4 "review this"
kandor -- --continue
kandor --completions bash
```

> Note: `--version` is the wrapper version. Use `kandor -- --version` for
> the OpenCode version.

## Documentation

- [Configuration](docs/configuration.md) — options, the config file,
  per-project `.kandor.toml` and reusable `[sections.*]` stacks
- [Agents](docs/agents.md) — `--agent`, built-in OpenCode/Claude/pi/copilot
  support, defining your own agents, `exec` mode
- [Profiles](docs/profiles.md) — isolated vs. shared OpenCode state
- [Security model](docs/security.md) — what the sandbox isolates and what it does not
- [Typical setups](docs/setups.md) — builds, SSH, local models, offline runs
- [Troubleshooting](docs/troubleshooting.md) — user namespaces, git, sessions, resizing
- [Development](docs/development.md) — tests, CI and releases

## Uninstall

```sh
make uninstall                               # if installed with make install
rm "$(go env GOPATH)/bin/kandor"             # if installed with go install
rm -rf ~/.local/share/kandor                 # isolated profiles
rm -rf ~/.config/kandor                      # optional config file
```
