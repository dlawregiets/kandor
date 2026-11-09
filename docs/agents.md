# Agents

`kandor` runs one coding agent per invocation, selected by `--agent NAME`,
then `KANDOR_AGENT`, then the user file's `default_agent`, then `opencode`.
Unknown names are rejected with the list of known agents (built-ins plus
every `[agents.<name>]` in the user file).

| Agent | Kind | State by default |
| --- | --- | --- |
| `opencode` | Built-in, bespoke behavior | Unchanged: shared host data, or an isolated profile |
| `claude` | Built-in paths (probed: Claude Code 2.1.319) | `~/.claude` + `~/.claude.json` copied into the profile |
| `pi` | Config-only (probed: pi 1.4.0) | You declare it; snippet below |
| `copilot` | Config-only (probed: Copilot CLI 1.0.101) | You declare it; snippet below |

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

## Pi (config-only)

Paths and subcommands below come from `pi --help` (pi 1.4.0): global
settings live in `~/.pi/agent/settings.json`, sessions in
`~/.pi/agent/sessions/`, and it understands `PI_OFFLINE=1`. Drop this into
the user file (adjust if a later pi moves things):

```toml
[agents.pi]
binary = "pi"
subcommands = ["install", "remove", "uninstall", "update", "list", "config", "auth", "mcp"]

[agents.pi.paths]
copy = ["~/.pi"]

[agents.pi.env]
exclude = ["PI_"]
```

`copy` carries pi's settings and any stored login into the profile;
`exclude` keeps host `PI_*` variables out. Provider keys (`ANTHROPIC_API_KEY`
and friends) are forwarded automatically; `pi auth` on the host covers OAuth
logins before the first copy.

## Copilot (config-only)

From `copilot --help` (Copilot CLI 1.0.101): the user settings file is
`settings.json` in `~/.copilot` (or `$COPILOT_HOME` when set). Drop this
into the user file:

```toml
[agents.copilot]
binary = "copilot"
subcommands = ["app", "login", "help", "init", "config", "update", "version", "workflow", "sessions", "memories", "plugin", "mcp", "skill", "instruction", "lsp", "sandbox", "completion"]

[agents.copilot.paths]
copy = ["~/.copilot"]

[agents.copilot.env]
exclude = ["COPILOT_"]
```

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

[agents.aider.env]
pass    = ["OPENAI_API_KEY"]         # forwarded from the host
set     = { AIDER_NO_AUTO_COMMITS = "1" }
exclude = ["AIDER_"]                 # never forwarded
```

Path rules are enforced like mounts: absolute or `~` paths only, no broad
host paths, and one path in two modes of the same layer is an error. `share`
warns on every run. A `default_agent` in a project file is an error; agent
keys in a project file still print the widening warning.

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
