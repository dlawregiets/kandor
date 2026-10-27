#!/usr/bin/env bash
#
# Smoke tests for kandor.  These do not call an LLM and do not modify
# anything outside a temporary directory (except reading the host's
# OpenCode config/data directories).
#
set -uo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(cd -- "$here/.." && pwd)
kandor=$root/bin/kandor

tmp=$(mktemp -d "${TMPDIR:-/tmp}/kandor-test.XXXXXX")
trap 'rm -rf -- "$tmp"' EXIT

export KANDOR_HOME=$tmp/profiles
export KANDOR_CONFIG=$tmp/config

pass=0
fail=0
ok()   { pass=$((pass + 1)); printf 'ok   %s\n' "$1"; }
bad()  { fail=$((fail + 1)); printf 'FAIL %s\n' "$1"; }
check(){ if eval "$2"; then ok "$1"; else bad "$1"; fi; }

command -v bwrap >/dev/null 2>&1 || { echo "bwrap not installed" >&2; exit 1; }
[[ -x $kandor ]] || { echo "missing $kandor; run make build first" >&2; exit 1; }
command -v opencode >/dev/null 2>&1 || { echo "opencode not installed" >&2; exit 1; }

# A project plus a secret outside it.  Both live under the private /tmp, so
# the secret must not be visible inside the sandbox.
mkdir -p "$tmp/project" "$tmp/secret" "$tmp/ro" "$tmp/rw"
echo "top secret" > "$tmp/secret/host-only"
echo "read only" > "$tmp/ro/file"
echo "inside" > "$tmp/project/visible"
printf 'name = "smoke"\n' > "$tmp/project/smoke.txt"

# ---------------------------------------------------------------------------
# CLI basics
# ---------------------------------------------------------------------------
check "--help works" "$kandor --help >/dev/null 2>&1"
check "--version works" "$kandor --version | grep -q '^kandor '"

init_cfg=$tmp/init-config
KANDOR_CONFIG=$init_cfg $kandor --init-config >/dev/null 2>&1
check "--init-config creates a config file" "[[ -f $init_cfg ]]"
check "the generated config is accepted" \
  "KANDOR_CONFIG=$init_cfg $kandor --dry-run >/dev/null 2>&1"

KANDOR_CONFIG=$init_cfg $kandor --init-config >/dev/null 2>&1
rc=$?
check "--init-config does not overwrite an existing file" "[[ $rc -ne 0 ]]"

dry=$($kandor -C "$tmp/project" --isolated --dry-run 2>/dev/null)
check "dry-run mounts / read-only" "grep -q -- '--ro-bind / /' <<<\"\$dry\""
check "dry-run unshares PID" "grep -q -- '--unshare-pid' <<<\"\$dry\""
check "dry-run clears the environment" "grep -q -- '--clearenv' <<<\"\$dry\""
check "dry-run binds the project" "grep -q -- \"--bind $tmp/project $tmp/project\" <<<\"\$dry\""
check "dry-run binds the isolated config directory" \
  "grep -q -- \"--bind $tmp/profiles/default/config/opencode\" <<<\"\$dry\""
check "dry-run forces --standalone for the TUI" "grep -q -- '--standalone' <<<\"\$dry\""

dry_shared=$($kandor -C "$tmp/project" --dry-run 2>/dev/null)
check "shared mode is the default" \
  "! grep -q -- 'profiles/default/config/opencode' <<<\"\$dry_shared\""
check "default dry-run still hides the home directory" \
  "grep -q -- '--tmpfs /home' <<<\"\$dry_shared\""

secret_out=$(TEST_SECRET=supersecret $kandor --env TEST_SECRET --dry-run 2>/dev/null)
check "dry-run redacts secrets" "! grep -q supersecret <<<\"\$secret_out\""
check "dry-run marks redacted values" "grep -q 'redacted' <<<\"\$secret_out\""

proxy_out=$(HTTPS_PROXY=http://user:proxypw@proxy:3128 $kandor --dry-run 2>/dev/null)
check "dry-run redacts credentials in URLs" "! grep -q proxypw <<<\"\$proxy_out\""

env_out=$(STRIPE_SECRET_KEY=x ANTHROPIC_API_KEY=y $kandor --dry-run 2>/dev/null)
check "provider keys are forwarded" "grep -q -- '--setenv ANTHROPIC_API_KEY' <<<\"\$env_out\""
check "unrelated secrets are not forwarded" "! grep -q STRIPE_SECRET_KEY <<<\"\$env_out\""

# ---------------------------------------------------------------------------
# Filesystem boundary
# ---------------------------------------------------------------------------
run_shell() {
  ( cd "$tmp/project" && "$kandor" -C "$tmp/project" --shell -- -c "$1" ) 2>&1
}

out=$(run_shell 'pwd; hostname')
check "project is the working directory" "grep -qx '$tmp/project' <<<\"\$out\""
check "sandbox hostname is set" "grep -qx 'kandor' <<<\"\$out\""

run_shell 'test ! -e "'"$tmp"'/secret/host-only"' >/dev/null 2>&1
check "secret outside the project is hidden" "[[ $? -eq 0 ]]"

run_shell 'test ! -e "$HOME/.ssh"' >/dev/null 2>&1
check "~/.ssh is hidden by default" "[[ $? -eq 0 ]]"

run_shell 'test ! -e "$HOME/.aws"' >/dev/null 2>&1
check "~/.aws is hidden by default" "[[ $? -eq 0 ]]"

run_shell 'test ! -e /run/dbus/system_bus_socket' >/dev/null 2>&1
check "host DBus socket is hidden" "[[ $? -eq 0 ]]"

run_shell 'test ! -e /run/user/'"$(id -u)"'/bus' >/dev/null 2>&1
check "host user runtime sockets are hidden" "[[ $? -eq 0 ]]"

run_shell 'touch /etc/kandor-should-fail 2>/dev/null' >/dev/null 2>&1
check "/etc is read-only" "[[ $? -ne 0 ]]"

run_shell 'touch "'"$tmp"'/project/created-inside"' >/dev/null 2>&1
check "project is writable" "[[ -f $tmp/project/created-inside ]]"

rm -f "$tmp/project/created-inside"

unique=/tmp/kandor-smoke-$$
run_shell "touch $unique" >/dev/null 2>&1
check "private /tmp does not leak to the host" "[[ ! -e $unique ]]"

home_marker=.kandor-smoke-$$
run_shell "touch \"\$HOME/$home_marker\"" >/dev/null 2>&1
check "sandbox HOME writes do not reach the host" "[[ ! -e $HOME/$home_marker ]]"

# ---------------------------------------------------------------------------
# service.json and the host service password
# ---------------------------------------------------------------------------
out=$(run_shell 'cat "$XDG_CONFIG_HOME/opencode/service.json"; cat "$XDG_STATE_HOME/opencode/service.json"')
check "config service.json has a private port" "grep -q '\"port\"' <<<\"\$out\""
check "state service.json starts empty" "grep -qx '{}' <<<\"\$out\""

host_service=$HOME/.config/opencode/service.json
if [[ -f $host_service ]]; then
  host_pw=$(sed -n 's/.*"password"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
              "$host_service" | head -1)
  if [[ -n $host_pw ]]; then
    check "host service password is not visible in the sandbox" \
      "! grep -qF \"\$host_pw\" <<<\"\$out\""
  fi

  before=$(sha256sum "$host_service" | cut -d' ' -f1)
  ( cd "$tmp/project" && "$kandor" --shared --shell -- -c 'true' ) >/dev/null 2>&1
  after=$(sha256sum "$host_service" | cut -d' ' -f1)
  check "shared mode does not modify the host service.json" "[[ $before == $after ]]"
fi

out=$($kandor -C "$tmp/project" --shared debug paths 2>&1)
check "shared mode uses a temporary config directory" "grep -q 'kandor-shared' <<<\"\$out\""

out=$($kandor -C "$tmp/project" --shared debug config 2>&1)
if [[ -f $HOME/.config/opencode/opencode.json ]]; then
  check "shared mode sees the host config" "grep -q 'opencode.json' <<<\"\$out\""
fi
check "shared mode does not hit a busy service.json" "! grep -q 'EBUSY\\|Busy:' <<<\"\$out\""

# ---------------------------------------------------------------------------
# Extra mounts
# ---------------------------------------------------------------------------
out=$($kandor -C "$tmp/project" --ro "$tmp/ro" --rw "$tmp/rw" --shell -- -c \
  "cat '$tmp/ro/file'; echo x > '$tmp/rw/file'; echo ro_rc=\$?; echo y > '$tmp/ro/nope' 2>/dev/null; echo ro_write_rc=\$?" 2>&1)
check "read-only extra mount is readable" "grep -qx 'read only' <<<\"\$out\""
check "read-write extra mount is writable" "[[ -f $tmp/rw/file ]]"
check "read-only extra mount rejects writes" "grep -qx 'ro_write_rc=1' <<<\"\$out\""

# ---------------------------------------------------------------------------
# Isolated profiles
# ---------------------------------------------------------------------------
$kandor --profile smoke --shell -- -c 'echo persisted > "$XDG_DATA_HOME/opencode/smoke-file"' >/dev/null 2>&1
$kandor --profile smoke --shell -- -c 'cat "$XDG_DATA_HOME/opencode/smoke-file"' >/dev/null 2>&1
check "isolated profile persists between runs" "[[ -f $tmp/profiles/smoke/data/opencode/smoke-file ]]"
check "isolated profile does not use the host data directory" \
  "[[ -f $tmp/profiles/smoke/data/opencode/smoke-file && ! -e $HOME/.local/share/opencode/smoke-file ]]"

# ---------------------------------------------------------------------------
# OpenCode only
# ---------------------------------------------------------------------------
printf 'agent omp\n' > "$tmp/omp-config"
out=$(KANDOR_CONFIG=$tmp/omp-config $kandor --dry-run 2>&1)
check "an old-format config file is reported" \
  "grep -q 'old configuration format' <<<\"\$out\""

dry_agent=$($kandor -C "$tmp/project" --dry-run --agent plan 2>/dev/null)
check "--agent is passed to OpenCode" \
  "grep -q -- '--standalone --agent plan' <<<\"\$dry_agent\""

# ---------------------------------------------------------------------------
# Process handling and unusual inputs
# ---------------------------------------------------------------------------
mkdir -p "$tmp/my proj"
out=$($kandor -C "$tmp/my proj" --shell -- -c 'pwd' 2>/dev/null)
check "project directories with spaces work" "[[ \$out == '$tmp/my proj' ]]"
dry_space=$($kandor -C "$tmp/my proj" --dry-run 2>/dev/null)
check "dry-run quotes paths with spaces" "grep -qF -- \"--bind '$tmp/my proj' '$tmp/my proj'\" <<<\"\$dry_space\""

out=$( (exec 9</etc/hostname; $kandor -C "$tmp/project" --shell -- -c 'test -e /proc/self/fd/9 && echo leaked') 2>/dev/null)
check "inherited file descriptors do not reach the sandbox" "[[ -z \$out ]]"

$kandor -C "$tmp/project" --shell -- -c 'kill -TERM $$' >/dev/null 2>&1
check "a sandbox killed by a signal gives 128+n" "[[ $? -eq 143 ]]"

$kandor -C "$tmp/project" --shell -- -c 'sleep 30' >/dev/null 2>&1 &
kpid=$!; sleep 1; kill -TERM "$kpid"; wait "$kpid"
check "SIGTERM removes the shared temporary profile" \
  "! grep -qsx '$kpid' ${TMPDIR:-/tmp}/kandor-shared-$(id -u).*/pid"

printf '[mounts]\nread_only = ["~/kandor-missing-%s"]\n' "$$" > "$tmp/missing.toml"
out=$(KANDOR_CONFIG=$tmp/missing.toml $kandor -C "$tmp/project" --dry-run 2>&1)
check "a missing configured path is a clear error" "grep -q 'ro path does not exist' <<<\"\$out\""

# ---------------------------------------------------------------------------
# Network isolation
# ---------------------------------------------------------------------------
$kandor --no-net --shell -- -c \
  "timeout 3 bash -c 'exec 3<>/dev/tcp/1.1.1.1/443'" >/dev/null 2>&1
check "--no-net blocks outbound connections" "[[ $? -ne 0 ]]"

# ---------------------------------------------------------------------------
# Git worktree validation
# ---------------------------------------------------------------------------
if command -v git >/dev/null 2>&1; then
  git init -q "$tmp/repo"
  ( cd "$tmp/repo" && git config user.email smoke@example.invalid &&
    git config user.name smoke && echo hi > f && git add f &&
    git commit -qm init && git worktree add -q "$tmp/wt" -b smoke-branch ) >/dev/null 2>&1
  out=$($kandor -C "$tmp/wt" --shell -- -c 'git log --oneline -1' 2>&1)
  check "git worktree is usable inside the sandbox" "grep -q 'init' <<<\"\$out\""

  git init -q "$tmp/otherrepo"
  ( cd "$tmp/otherrepo" && git config user.email smoke@example.invalid &&
    git config user.name smoke && echo secret > s && git add s &&
    git commit -qm other ) >/dev/null 2>&1
  mkdir -p "$tmp/evil"
  printf 'gitdir: %s\n' "$tmp/otherrepo/.git" > "$tmp/evil/.git"
  out=$($kandor -C "$tmp/evil" --shell -- -c 'test -e "'"$tmp"'/otherrepo/s"' 2>&1)
  check "untrusted .git files cannot expose another repository" "[[ $? -ne 0 ]]"

  out=$($kandor -C "$tmp/repo" --shell -- -c '
    git commit -q --allow-empty -m inside && echo commit-ok
    echo x > .git/hooks/pre-commit 2>/dev/null || echo hooks-ro
    git config core.fsmonitor evil 2>/dev/null || echo config-ro
    mv .git .git-moved 2>/dev/null || echo rename-blocked
    echo ../x > .git/commondir' 2>&1)
  check "git commits work with protected git" "grep -q commit-ok <<<\"\$out\""
  check "git hooks are read-only" "grep -q hooks-ro <<<\"\$out\""
  check "git config is read-only" "grep -q config-ro <<<\"\$out\""
  check ".git cannot be renamed away" "grep -q rename-blocked <<<\"\$out\""
  check "a planted commondir is quarantined" \
    "[[ ! -e $tmp/repo/.git/commondir ]] && grep -q quarantine <<<\"\$out\""

  out=$($kandor -C "$tmp/repo" --no-protect-git --shell -- -c \
    'git config kandor.test 1 && echo config-rw' 2>&1)
  check "--no-protect-git leaves git config writable" "grep -q config-rw <<<\"\$out\""
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
printf '\n%d passed, %d failed\n' "$pass" "$fail"
(( fail == 0 ))
