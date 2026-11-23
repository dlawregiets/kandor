# Agents

`kandor` runs one coding agent per invocation, selected by `--agent NAME`,
then `KANDOR_AGENT`, then the project file's `default_agent`, then the user
file's `default_agent`, then `opencode`. Unknown names are rejected with the
list of known agents (built-ins plus every `[agents.<name>]` in the user or
project file).

A project's `.kandor.toml` can switch the agent for that project only;
projects that do not set `default_agent` keep the user's choice. The switch
is announced with a `kandor: using agent ...` line, and a project that
defines its own `[agents.<name>]` section prints the usual widening warning
for settings such as `binary`.

| Agent | Probed version | State by default |
| --- | --- | --- |
| `opencode` | — | Unchanged: shared host data, or an isolated profile |
| `claude` | Claude Code 2.1.319 | `~/.claude` + `~/.claude.json` copied into the profile |
| `pi` | pi 1.4.0 | `~/.pi` copied into the profile |
| `copilot` | Copilot CLI 1.0.101 | `~/.copilot` copied into the profile |

Every built-in is just defaults: `[agents.<name>]` in the user or project
file overrides the binary, adds paths, or replaces the environment
exclusions.

## OpenCode

The default. Nothing changed: `--standalone` rewriting, `service.json`
masking, shared host data unless `--isolated`/`--profile`. See
[Profiles](profiles.md) and [Configuration](configuration.md).

## Claude

Claude keeps settings and login together, so the built-in descriptor copies
both into the profile: the sandbox never reads or writes the host's
`~/.claude` or `~/.claude.json`. The copy happens once per isolated profile;
a shared run copies into a temporary store. `CLAUDECODE` and
`CLAUDE_CODE_ENTRYPOINT` (child-process markers, verified in the installed
binary) are never forwarded back in.

Login options, easiest first:

- `ANTHROPIC_API_KEY` is forwarded automatically, so an API-key setup works
  with no extra configuration.
- Run `claude login` on the host first (OAuth needs a host browser), then
  let the profile copy pick the login up.
- Override per project if you keep a separate Claude home:

```toml
[agents.claude.paths]
copy = ["~/.claude-work", "~/.claude-work.json"]
```

## Pi

Probed from `pi --help` (pi 1.4.0): global settings live in
`~/.pi/agent/settings.json` and sessions in `~/.pi/agent/sessions/`. The
built-in descriptor:

- copies the small state under `~/.pi` into the profile (settings, login,
  model store);
- shares the derived trees with the host instead of copying them: installed
  extensions (`~/.pi/agent/npm`) and pi's own releases
  (`~/.pi/agent/install`). They run to over a gigabyte, and copying them made
  every start pay a full tree copy;
- keeps `~/.pi/agent/sessions` shared read-write with the host, so a session
  started in the sandbox can be resumed later (and host sessions are
  visible), the same trade-off as OpenCode's shared data directory;
- keeps downloaded helper binaries (`~/.pi/agent/bin`, e.g. `fd`) in a
  kandor-owned persistent cache, so they are downloaded once and never
  written back to the host;
- points Node's compile cache at a kandor-owned persistent directory instead
  of the sandbox's ephemeral `/tmp`, which otherwise makes every run pay a
  cold-start compile (about 100 ms on `pi --version` alone);
- keeps host `PI_*` variables out (`PI_OFFLINE` and friends would otherwise
  point at host state).

Provider keys (`ANTHROPIC_API_KEY` and friends) are forwarded automatically;
`pi auth` on the host covers OAuth logins before the first copy. If you keep
pi's state elsewhere, override the paths:

```toml
[agents.pi.paths]
copy = ["~/alt-pi"]
```

## Copilot

Probed from `copilot --help` (Copilot CLI 1.0.101): the user settings file is
`settings.json` in `~/.copilot` (`$COPILOT_HOME` relocates it; the built-in
assumes the default). The descriptor copies `~/.copilot` into the profile and
excludes host `COPILOT_*` variables, since `COPILOT_HOME` would otherwise
point the sandbox at host state.

`copilot login` is a device flow that needs a host browser, so authenticate
on the host first. `GH_TOKEN`/`GITHUB_TOKEN` pass through with `--env` when
scripting is needed.

## Defining your own agent

Any `[agents.<name>]` section (lowercase letters, digits, `-`, `_`; not
`exec` or `default`) makes a runnable agent with no code changes:

```toml
[agents.aider]
binary = "aider"                     # PATH name, or a path
subcommands = ["--help"]             # extra positional subcommands

[agents.aider.paths]
share   = ["~/.aider.conf.yml"]      # read-write host mount (warned)
copy    = ["~/.aider"]               # seeded into the profile once
private = ["~/.aider/cache"]         # fresh profile dir, host stays hidden
ro      = ["~/.config/git"]          # read-only host mount
cache   = ["~/.aider/bin"]           # kandor-owned persistent dir (never the host's)

[agents.aider.env]
pass    = ["OPENAI_API_KEY"]         # forwarded from the host
set     = { AIDER_NO_AUTO_COMMITS = "1" }
exclude = ["AIDER_"]                 # never forwarded
```

Path rules are enforced like mounts: absolute or `~` paths only, no broad
host paths, and one path in two modes of the same layer is an error. `share`
warns on every run. A project file may set `default_agent` and define its own
`[agents.<name>]` sections to try a harness in one repository; those settings
apply only there and agent keys still print the widening warning.

## `kandor exec`

`kandor exec [--] CMD...` runs an arbitrary command with no agent: no agent
state, no agent section, no credential probing. Kandor options come before
`exec`; everything after it is the command. The profile is always temporary
and host config is visible only through explicit `--ro`/`--rw`:

```sh
kandor --dry-run exec -- pytest -q
kandor -C ~/code/proj --ro ~/.cache/pip exec -- bash build.sh
```

`--shell`, `--agent`, `--isolated`, `--profile` and `--no-seed` are
rejected with `exec`.
