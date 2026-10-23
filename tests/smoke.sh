#!/usr/bin/env bash
#
# Smoke tests for kandor.  These do not call an LLM and do not modify
# anything outside a temporary directory (except reading the host's
# OpenCode config/data directories).
#
set -uo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(cd -- "$here/.." && pwd)
kandor=$root/kandor

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
[[ -x $kandor ]] || { echo "missing $kandor" >&2; exit 1; }
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
KANDOR_CONFIG=$init_cfg $kandor --init-config --agent omp >/dev/null 2>&1
check "--init-config creates a config file" "[[ -f $init_cfg ]]"
check "--init-config writes the requested agent" "grep -qx 'agent omp' '$init_cfg'"

KANDOR_CONFIG=$init_cfg $kandor --set-agent pi >/dev/null 2>&1
check "--set-agent updates the saved default" \
  "grep -qx 'agent pi' '$init_cfg' && ! grep -qx 'agent omp' '$init_cfg'"

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
# Other agents (Pi and oh-my-pi)
# ---------------------------------------------------------------------------
check "unknown agent is rejected" \
  "! $kandor --agent nope --dry-run >/dev/null 2>&1"

if command -v pi >/dev/null 2>&1; then
  dry_pi=$($kandor --agent pi --isolated -C "$tmp/project" --dry-run 2>/dev/null)
  check "pi dry-run binds the profile agent directory" \
    "grep -q -- \"--bind $tmp/profiles/default/pi/agent $HOME/.pi/agent\" <<<\"\$dry_pi\""
  check "pi dry-run sets PI_CODING_AGENT_DIR" \
    "grep -q -- '--setenv PI_CODING_AGENT_DIR' <<<\"\$dry_pi\""
  check "pi profile does not seed stored credentials" \
    "[[ ! -e $tmp/profiles/default/pi/agent/auth.json ]]"
  check "pi profile does not seed host sessions" \
    "[[ ! -e $tmp/profiles/default/pi/agent/sessions ]]"

  dry_pi_shared=$($kandor --agent pi -C "$tmp/project" --dry-run 2>/dev/null)
  check "pi defaults to shared state, not a profile" \
    "! grep -q -- \"$tmp/profiles/default/pi/agent\" <<<\"\$dry_pi_shared\""
  check "pi shared mode binds the host agent directory" \
    "grep -q -- \"--bind $HOME/.pi/agent $HOME/.pi/agent\" <<<\"\$dry_pi_shared\""

  # A throwaway HOME proves that config written in shared mode lands in the
  # host ~/.pi/agent tree instead of a private copy.
  pi_home=$tmp/pi-home
  mkdir -p "$pi_home/.pi/agent"
  HOME=$pi_home $kandor --agent pi -C "$tmp/project" --shell -- -c \
    'echo persisted > "$HOME/.pi/agent/smoke-file"' >/dev/null 2>&1
  check "pi shared mode writes config to the host agent directory" \
    "[[ -f $pi_home/.pi/agent/smoke-file ]]"

  out=$($kandor --agent pi --isolated -C "$tmp/project" -- --version 2>&1)
  check "pi runs inside the sandbox" "grep -qE '^[0-9]+\\.[0-9]+' <<<\"\$out\""

  $kandor --agent pi --isolated -C "$tmp/project" --shell -- -c \
    'echo persisted > "$HOME/.pi/agent/smoke-file"' >/dev/null 2>&1
  check "pi profile persists between runs" \
    "[[ -f $tmp/profiles/default/pi/agent/smoke-file ]]"

  if [[ -d $HOME/.pi/agent/sessions ]]; then
    out=$($kandor --agent pi --shared -C "$tmp/project" --shell -- -c \
      'stat -c %d:%i "$HOME/.pi/agent/sessions"' 2>/dev/null)
    check "pi shared mode mounts the host session directory" \
      "[[ \$out == \$(stat -c %d:%i \"\$HOME/.pi/agent/sessions\") ]]"
  fi
fi

if command -v omp >/dev/null 2>&1; then
  dry_omp=$($kandor --agent omp --isolated -C "$tmp/project" --dry-run 2>/dev/null)
  check "omp dry-run binds the profile config root" \
    "grep -q -- \"--bind $tmp/profiles/default/omp $HOME/.omp\" <<<\"\$dry_omp\""
  check "omp profile does not seed the credential database" \
    "[[ ! -e $tmp/profiles/default/omp/agent/agent.db ]]"
  check "omp profile does not seed host sessions" \
    "[[ ! -e $tmp/profiles/default/omp/agent/sessions ]]"
  check "omp profile is seeded" \
    "[[ -f $tmp/profiles/default/omp/.kandor-seeded ]]"

  dry_omp_shared=$($kandor --agent omp -C "$tmp/project" --dry-run 2>/dev/null)
  check "omp defaults to the host config root" \
    "grep -q -- \"--bind $HOME/.omp $HOME/.omp\" <<<\"\$dry_omp_shared\""

  out=$($kandor --agent omp --isolated -C "$tmp/project" -- --version 2>&1)
  check "omp runs inside the sandbox" "grep -q '^omp/' <<<\"\$out\""

  $kandor --agent omp --isolated -C "$tmp/project" --shell -- -c \
    'echo persisted > "$HOME/.omp/smoke-file"' >/dev/null 2>&1
  check "omp profile persists between runs" \
    "[[ -f $tmp/profiles/default/omp/smoke-file ]]"

  out=$($kandor --agent omp --shared -C "$tmp/project" --shell -- -c \
    'stat -c %d:%i "$HOME/.omp"' 2>/dev/null)
  check "omp shared mode mounts the host config root" \
    "[[ \$out == \$(stat -c %d:%i \"\$HOME/.omp\") ]]"
fi

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
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
printf '\n%d passed, %d failed\n' "$pass" "$fail"
(( fail == 0 ))
