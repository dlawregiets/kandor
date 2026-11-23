# Configuration

## Options

Run `kandor --help` for the full list. Common options:

| Option | Meaning |
| --- | --- |
| `--init-config` | Create a commented config file and exit |
| `--init-project` | Create `.kandor.toml` in the project, prompting for sections |
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
| `--dry-run` | Print the planned bwrap command; creates nothing (redacts secrets) |

For safety, `--ro` and `--rw` refuse `/`, `$HOME`, ancestors of `$HOME`,
`/run/user/$UID`, and top-level host directories such as `/tmp`, `/etc` or
`/usr`. Mount a subdirectory or file instead (for example `--rw ~/go`).

If a configured mount would make the project directory read-only, `kandor`
warns and asks for confirmation before starting the sandbox. An explicit
`--ro` of the project directory skips the prompt.

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

Paths may start with `~` or `$HOME`. Unknown sections, keys and agent names,
and values of the wrong type, are errors, so a typo cannot silently change
the sandbox.

### Per-project configuration

A `.kandor.toml` file in the project directory adds project-specific settings
on top of the user file. It accepts the same keys, plus an `include` list
that opts into reusable stacks defined in the user file:

```toml
# ~/code/my-go-project/.kandor.toml
include = ["go"]

[env.set]
GOTOOLCHAIN = "local"

# Try a different harness in this project only.
default_agent = "aider"

[agents.aider]
binary = "aider"

[agents.aider.paths]
copy = ["~/.aider"]
```

A project `default_agent` overrides the user file's choice for that project
only; other projects keep the user default (or `opencode`). The project can
also define a new `[agents.<name>]` section, so experimenting with a harness
does not require changing the user configuration. *kandor* prints
`kandor: using agent "..." from ...` when the project selects the agent.

The user file may define any number of named stacks under `[sections.*]`,
with the same settings as the top level. The `[sections.default]` stack, when
defined, always applies:

```toml
# ~/.config/kandor/config.toml
[sections.default.mounts]
read_write = ["~/.cache/go-build"]

[sections.go.mounts]
read_write = ["~/go"]

[sections.go.env.set]
GOCACHE = "/home/you/.cache/go-build"

[sections.python.mounts]
read_write = ["~/.cache/uv"]
```

Layers apply in order and the later layer wins: the user file's top-level
keys, its `default` section, its own `include` list, the project file's
`include` list, the project file's own keys, `[agents.opencode]`, then the
command line. (A `sections` table in a project file is an error: stacks live
in the user file.) Because a borrowed repository should not silently widen
its own sandbox, project keys that widen it (extra mounts, environment,
credentials, ...) print a `kandor: warning:` naming the file and the keys.

Create a commented starter file with `kandor --init-config` (it uses
`$KANDOR_CONFIG` when that is set and never overwrites an existing file).
`kandor --init-project` (optionally with `-C`) writes a `.kandor.toml`
starter into the project directory instead, prompting for each stack in the
user file to include; it also never overwrites.

Command-line options override the files. Lists such as mounts and
environment variables add to the files' lists. When the same path is mounted
more than once, the later layer wins, in the order: the user file, its
`default` and included sections, the project file, the `[agents.opencode]`
section, the command line. So `--ro PATH`
makes a configured `read_write` path read-only for one run, and `--rw PATH`
makes a `read_only` path writable. Listing the same path as both
`read_only` and `read_write` in one layer is an error.

