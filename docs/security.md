# Security model


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

