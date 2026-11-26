# `--agent` in the init templates, and a .gitignore entry for `--init-project`

Status: awaiting approval.

## Intent

`kandor --init-config --agent pi` and `kandor --init-project --agent pi` should
write the chosen agent as `default_agent` instead of the commented
`"opencode"` placeholder, so the generated file is usable without editing.
`--init-project` should also keep the project file out of git for the user.

## Goals

- `--init-config --agent NAME` writes `default_agent = "NAME"` uncommented in
  the user template.
- `--init-project --agent NAME` writes `default_agent = "NAME"` uncommented in
  `.kandor.toml`.
- `--init-project` adds `.kandor.toml` to the project's `.gitignore` when the
  entry is absent, creating `.gitignore` when it does not exist.
- No `--agent`: both templates keep today's commented `# default_agent =
  "opencode"` line.
- Both flags still never overwrite an existing file.

## Non-goals

- `KANDOR_AGENT` and a user config's `default_agent` do not feed the
  templates; only an explicit `--agent` does.
- `--init-config` never touches `.gitignore` (nothing is written to a project).
- No check whether the project is a git repository.

## Approaches

1. **Parameterise the existing templates** (chosen): `config.Template(agent)`
   and `config.WriteTemplate(path, agent)` substitute the commented
   `default_agent` line; `initProject` takes the same agent string.
   Smallest diff, keeps one source of truth per template.
2. Generate files by uncommenting and rewriting the template text afterwards.
   More code, and it would uncomment the explanatory lines too.
3. Add a separate `default_agent` snippet appended to the file. Leaves the
   commented placeholder in place; two places to read.

## Design

- `config.Template(agent string) string`: `agent == ""` returns today's text;
  otherwise the commented two-line `default_agent` block is replaced by
  `default_agent = "<agent>"` plus the same explanatory comment.
  `WriteTemplate(path, agent string)` passes it through.
- `initProject(projDir, user, agent, stdin, stderr)`: the project template's
  commented `default_agent` line is written uncommented when `agent != ""`.
- New `addGitignore(projDir) (created bool, err error)` in
  `internal/app/initproject.go`:
  - entry is `.kandor.toml`; an existing line `.kandor.toml` or
    `/.kandor.toml` (optional surrounding whitespace) counts as present;
  - missing `.gitignore` is created with the entry and a trailing newline;
  - an existing file without a final newline gets one before the entry;
  - a write error aborts `--init-project` before `.kandor.toml` is written;
  - a change is reported as `kandor: added .kandor.toml to <path>`.
- Validation: `--agent` is already checked against known agents on the
  `--init-project` path. `--init-config` gains the same check against the
  built-in agents (`agent.Known()`), because no config file exists yet to
  define more. Unknown agent => exit 1, nothing written. (Call: a name only
  defined in an existing project file, e.g. `[agents.foo]` in
  `.kandor.toml`, is rejected by `--init-config --agent foo`; the user can
  set that line by hand.)

## Left to the builder

- Message wording/format for the `.gitignore` notice.
- Whether `tests/smoke.sh` gains a case; the Go tests cover the behaviour.
