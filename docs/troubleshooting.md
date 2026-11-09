# Troubleshooting


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
[Security model](security.md)). Run the command on the host, or start
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

