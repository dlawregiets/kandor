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
Options `kandor` does not know, such as OpenCode's `--continue` or
`--model`, are passed through unchanged.

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

If that fails, see [Troubleshooting](docs/troubleshooting.md).

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
rm ~/.local/bin/kandor
rm -rf ~/.local/share/kandor   # isolated profiles
rm -rf ~/.config/kandor        # optional config file
```
