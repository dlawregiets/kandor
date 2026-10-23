#!/usr/bin/env bash
#
# Unit tests for kandor's planners.  The script is sourced (the source guard
# keeps main() from running) and parse_args, load_config, the mount helpers,
# build_mounts and build_agent_command are exercised directly.  No sandbox is
# started and no agent is executed.
#
set -uo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(cd -- "$here/.." && pwd)
kandor=$root/kandor

tmp=$(mktemp -d "${TMPDIR:-/tmp}/kandor-unit.XXXXXX")
trap 'rm -rf -- "$tmp"' EXIT

mkdir -p "$tmp/home" "$tmp/home/.pi/agent" "$tmp/proj" "$tmp/run" "$tmp/ro" "$tmp/rw" \
         "$tmp/xdgconfig" "$tmp/xdgcache" \
         "$tmp/profile/omp" "$tmp/profile/pi/agent" \
         "$tmp/profile/config/opencode" "$tmp/profile/data/opencode" \
         "$tmp/profile/state/opencode" "$tmp/profile/cache/opencode"

pass=0
fail=0
ok()    { pass=$((pass + 1)); printf 'ok   %s\n' "$1"; }
bad()   { fail=$((fail + 1)); printf 'FAIL %s\n' "$1"; }
check() { if eval "$2"; then ok "$1"; else bad "$1"; fi; }

[[ -r $kandor ]] || { echo "missing $kandor" >&2; exit 1; }

# Each body runs in its own shell: sourcing kandor enables `set -e`, and a
# die() must not abort the rest of the harness.
snippet() {
  HOME=$tmp/home bash -c 'source "$1"; shift; eval "$*"' _ "$kandor" "$1"
}

# Print the state parse_args() leaves behind.
rst() {
  snippet "$1
printf '%s|%s|%s|%s|%s\n' \"\$agent\" \"\$isolated\" \"\$profile\" \"\$project_dir\" \"\${oc_args[*]}\""
}

# ---------------------------------------------------------------------------
# parse_args
# ---------------------------------------------------------------------------
out=$(rst "parse_args -C '$tmp/proj'")
check "parse_args: defaults" \
  "[[ \$out == 'opencode|0|default|$tmp/proj|' ]]"

out=$(rst "parse_args -C '$tmp/proj' --isolated")
check "parse_args: --isolated" \
  "[[ \$out == 'opencode|1|default|$tmp/proj|' ]]"

out=$(rst "parse_args -C '$tmp/proj' --profile work")
check "parse_args: --profile implies isolated" \
  "[[ \$out == 'opencode|1|work|$tmp/proj|' ]]"

out=$(rst "parse_args -C '$tmp/proj' --isolated --shared")
check "parse_args: --shared clears isolation" \
  "[[ \$out == 'opencode|0|default|$tmp/proj|' ]]"

out=$(rst "parse_args --agent omp --isolated -C '$tmp/proj'")
check "parse_args: --agent omp" \
  "[[ \$out == 'omp|1|default|$tmp/proj|' ]]"

out=$(rst "parse_args --agent=pi -C '$tmp/proj'")
check "parse_args: --agent=NAME form" \
  "[[ \$out == 'pi|0|default|$tmp/proj|' ]]"

out=$(rst "parse_args --agent pi -C '$tmp/proj' hello")
check "parse_args: pi positional stays an agent argument" \
  "[[ \$out == 'pi|0|default|$tmp/proj|hello' ]]"

out=$(rst "parse_args '$tmp/proj'")
check "parse_args: opencode positional directory is the project" \
  "[[ \$out == 'opencode|0|default|$tmp/proj|' ]]"

out=$(rst "parse_args -C '$tmp/proj' run -x")
check "parse_args: opencode subcommand is passed through" \
  "[[ \$out == 'opencode|0|default|$tmp/proj|run -x' ]]"

out=$(rst "parse_args -- --version")
check "parse_args: -- passes arguments through" \
  "[[ \$out == 'opencode|0|default|$PWD|--version' ]]"

out=$(snippet "parse_args --help --agent pi
printf '%s|%s\n' \"\$show_help\" \"\$agent\"")
check "parse_args: --help stops parsing" "[[ \$out == '1|opencode' ]]"

out=$(snippet "parse_args --version --agent pi
printf '%s|%s\n' \"\$show_version\" \"\$agent\"")
check "parse_args: --version stops parsing" "[[ \$out == '1|opencode' ]]"

out=$(snippet 'parse_args --agent nope' 2>&1)
rc=$?
check "parse_args: unknown agent is rejected" \
  "[[ $rc -eq 1 && \$out == *'unknown agent'* ]]"

# ---------------------------------------------------------------------------
# load_config
# ---------------------------------------------------------------------------
printf 'agent omp\n' > "$tmp/cfg-agent"
printf 'shared\n' > "$tmp/cfg-shared"
printf 'profile work\n' > "$tmp/cfg-profile"

out=$(snippet "load_config '$tmp/cfg-agent'; printf '%s\n' \"\$agent\"")
check "load_config: agent directive" "[[ \$out == omp ]]"

out=$(snippet "isolated=1; load_config '$tmp/cfg-shared'; printf '%s\n' \"\$isolated\"")
check "load_config: shared directive" "[[ \$out == 0 ]]"

out=$(snippet "load_config '$tmp/cfg-profile'; printf '%s|%s\n' \"\$isolated\" \"\$profile\"")
check "load_config: profile implies isolated" "[[ \$out == '1|work' ]]"

out=$(snippet "load_config '$tmp/cfg-agent'; parse_args -C '$tmp/proj'; printf '%s\n' \"\$agent\"")
check "config agent is used as the default" "[[ \$out == omp ]]"

out=$(snippet "load_config '$tmp/cfg-agent'; parse_args -C '$tmp/proj' --agent pi; printf '%s\n' \"\$agent\"")
check "--agent overrides the config agent" "[[ \$out == pi ]]"

out=$(snippet "export KANDOR_AGENT=pi; load_config '$tmp/cfg-agent'; parse_args -C '$tmp/proj'; printf '%s\n' \"\$agent\"")
check "KANDOR_AGENT overrides the config agent" "[[ \$out == pi ]]"

out=$(snippet "export KANDOR_AGENT=pi; parse_args -C '$tmp/proj' --agent omp; printf '%s\n' \"\$agent\"")
check "--agent overrides KANDOR_AGENT" "[[ \$out == omp ]]"

# ---------------------------------------------------------------------------
# Config generation helpers
# ---------------------------------------------------------------------------
generated=$tmp/generated/config
snippet "write_default_config '$generated' omp"
check "write_default_config: creates the file" "[[ -f $generated ]]"
check "write_default_config: writes the agent directive" "grep -qx 'agent omp' '$generated'"
check "write_default_config: includes commented examples" "grep -qx '# no-net' '$generated'"

printf 'no-net\nrw %s\n' "$tmp/rw" >> "$generated"
snippet "persist_agent '$generated' pi"
check "persist_agent: replaces the agent directive" \
  "grep -qx 'agent pi' '$generated' && ! grep -qx 'agent omp' '$generated'"
check "persist_agent: preserves other directives" \
  "grep -qx 'no-net' '$generated' && grep -qx 'rw $tmp/rw' '$generated'"

missing=$tmp/generated/missing/config
snippet "persist_agent '$missing' opencode"
check "persist_agent: creates a missing file" \
  "[[ -f $missing ]] && grep -qx 'agent opencode' '$missing'"

# ---------------------------------------------------------------------------
# Mount helpers
# ---------------------------------------------------------------------------
out=$(snippet 'BWRAP=(); mounted=()
add_rw /a /dest
add_ro /b /dest
printf "%s|" "${BWRAP[@]}"')
check "add_mount: a destination is mounted once" \
  "[[ \$out == '--bind|/a|/dest|' ]]"

mh="HOME=$tmp/home
runtime_dir=$tmp/run
uid=\$(id -u)"

for spec in "ro /" "ro \$HOME" "rw /usr"; do
  out=$(snippet "$mh
add_extra_mount $spec" 2>&1)
  rc=$?
  check "add_extra_mount: refuses $spec" \
    "[[ $rc -eq 1 && \$out == *refusing* ]]"
done

ro_real=$(realpath "$tmp/ro")
out=$(snippet "$mh
BWRAP=(); mounted=()
add_extra_mount ro '$tmp/ro'
add_extra_mount rw '$tmp/ro'
printf '%s\n' \"\${BWRAP[@]}\"")
check "add_extra_mount: later rw mount of the same path is kept" \
  "[[ \$out == \"--ro-bind
$ro_real
$ro_real
--bind
$ro_real
$ro_real\" ]]"

# ---------------------------------------------------------------------------
# build_mounts
# ---------------------------------------------------------------------------
# Environment shared by the mount tests.  BWRAP_HELP is only used for feature
# probes; an empty value simply drops the optional flags.
mount_env() {
  cat <<EOF
BWRAP_HELP=\$(bwrap --help 2>&1 || true)
HOME=$tmp/home
project_dir=$tmp/proj
runtime_dir=$tmp/run
uid=\$(id -u)
xdg_config=$tmp/xdgconfig
xdg_cache=$tmp/xdgcache
allow_ssh=0
allow_ssh_agent=0
allow_gitconfig=1
allow_git_credentials=0
allow_netrc=0
allow_npmrc=0
shell_mode=0
shell_bin=/bin/bash
oc_bin=/bin/true
extra_ro=()
extra_rw=()
extra_env=()
EOF
}

out=$(snippet "$(mount_env)
agent=omp
isolated=1
profile_root=$tmp/profile
omp_root=$tmp/profile/omp
build_mounts
printf '%s\n' \"\${BWRAP[*]}\"")
check "build_mounts: omp isolated binds the profile tree" \
  "[[ \$out == *'--bind $tmp/profile/omp $tmp/home/.omp'* ]]"

out=$(snippet "$(mount_env)
agent=omp
isolated=0
omp_root=\$HOME/.omp
build_mounts
printf '%s\n' \"\${BWRAP[*]}\"")
check "build_mounts: omp shared binds the host tree" \
  "[[ \$out == *'--bind $tmp/home/.omp $tmp/home/.omp'* ]]"

out=$(snippet "$(mount_env)
agent=pi
isolated=1
pi_agent_dir=$tmp/profile/pi/agent
build_mounts
printf '%s\n' \"\${BWRAP[*]}\"")
check "build_mounts: pi isolated binds the profile agent directory" \
  "[[ \$out == *'--bind $tmp/profile/pi/agent $tmp/home/.pi/agent'* ]]"

out=$(snippet "$(mount_env)
agent=pi
isolated=0
pi_agent_dir=\$HOME/.pi/agent
build_mounts
printf '%s\n' \"\${BWRAP[*]}\"")
check "build_mounts: pi shared binds the host agent directory" \
  "[[ \$out == *'--bind $tmp/home/.pi/agent $tmp/home/.pi/agent'* ]]"

out=$(snippet "$(mount_env)
agent=opencode
isolated=1
config_dir=$tmp/profile/config/opencode
data_dir=$tmp/profile/data/opencode
state_dir=$tmp/profile/state/opencode
cache_dir=$tmp/profile/cache/opencode
build_mounts
printf '%s\n' \"\${BWRAP[*]}\"")
for d in config data state cache; do
  check "build_mounts: opencode isolated binds the $d directory" \
    "[[ \$out == *'--bind $tmp/profile/$d/opencode $tmp/profile/$d/opencode'* ]]"
done

# ---------------------------------------------------------------------------
# build_agent_command
# ---------------------------------------------------------------------------
agent_cmd() {
  snippet "shell_mode=0
oc_bin=/bin/true
agent=$1
oc_args=($2)
build_agent_command
printf '%s\n' \"\${cmd[*]}\""
}

out=$(agent_cmd pi '--version')
check "build_agent_command: pi arguments are verbatim" \
  "[[ \$out == '/bin/true --version' ]]"

out=$(agent_cmd omp 'login anthropic')
check "build_agent_command: omp arguments are verbatim" \
  "[[ \$out == '/bin/true login anthropic' ]]"

out=$(agent_cmd opencode '')
check "build_agent_command: opencode TUI gets --standalone" \
  "[[ \$out == '/bin/true --standalone' ]]"

out=$(agent_cmd opencode 'run -x')
check "build_agent_command: --standalone follows the subcommand" \
  "[[ \$out == '/bin/true run --standalone -x' ]]"

out=$(agent_cmd opencode '--standalone run')
check "build_agent_command: leading --standalone is relocated" \
  "[[ \$out == '/bin/true run --standalone' ]]"

out=$(agent_cmd omp 'update' 2>&1)
rc=$?
check "build_agent_command: omp update is refused" \
  "[[ $rc -eq 1 && \$out == *refusing* ]]"

out=$(agent_cmd pi 'update self' 2>&1)
rc=$?
check "build_agent_command: pi update self is refused" \
  "[[ $rc -eq 1 && \$out == *refusing* ]]"

out=$(agent_cmd pi 'update' 2>&1)
rc=$?
check "build_agent_command: bare pi update is allowed" \
  "[[ $rc -eq 0 && \$out == '/bin/true update' ]]"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
printf '\n%d passed, %d failed\n' "$pass" "$fail"
(( fail == 0 ))
