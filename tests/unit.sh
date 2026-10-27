#!/usr/bin/env bash
#
# Unit tests for kandor's planners.  The script is sourced (the source guard
# keeps main() from running) and parse_args, load_config, the mount helpers,
# build_mounts and build_agent_command are exercised directly.  No sandbox is
# started and OpenCode is not executed.
#
set -uo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(cd -- "$here/.." && pwd)
kandor=$root/kandor

tmp=$(mktemp -d "${TMPDIR:-/tmp}/kandor-unit.XXXXXX")
trap 'rm -rf -- "$tmp"' EXIT

mkdir -p "$tmp/home" "$tmp/proj" "$tmp/run" "$tmp/ro" "$tmp/rw" \
         "$tmp/xdgconfig" "$tmp/xdgcache" \
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
printf '%s|%s|%s|%s\n' \"\$isolated\" \"\$profile\" \"\$project_dir\" \"\${oc_args[*]}\""
}

# ---------------------------------------------------------------------------
# parse_args
# ---------------------------------------------------------------------------
out=$(rst "parse_args -C '$tmp/proj'")
check "parse_args: defaults" \
  "[[ \$out == '0|default|$tmp/proj|' ]]"

out=$(rst "parse_args -C '$tmp/proj' --isolated")
check "parse_args: --isolated" \
  "[[ \$out == '1|default|$tmp/proj|' ]]"

out=$(rst "parse_args -C '$tmp/proj' --profile work")
check "parse_args: --profile implies isolated" \
  "[[ \$out == '1|work|$tmp/proj|' ]]"

out=$(rst "parse_args -C '$tmp/proj' --isolated --shared")
check "parse_args: --shared clears isolation" \
  "[[ \$out == '0|default|$tmp/proj|' ]]"

out=$(rst "parse_args '$tmp/proj'")
check "parse_args: opencode positional directory is the project" \
  "[[ \$out == '0|default|$tmp/proj|' ]]"

out=$(rst "parse_args -C '$tmp/proj' run -x")
check "parse_args: opencode subcommand is passed through" \
  "[[ \$out == '0|default|$tmp/proj|run -x' ]]"

out=$(rst "parse_args -- --version")
check "parse_args: -- passes arguments through" \
  "[[ \$out == '0|default|$PWD|--version' ]]"

out=$(snippet "parse_args --help --no-net
printf '%s|%s\n' \"\$show_help\" \"\$network\"")
check "parse_args: --help stops parsing" "[[ \$out == '1|1' ]]"

out=$(snippet "parse_args --version --no-net
printf '%s|%s\n' \"\$show_version\" \"\$network\"")
check "parse_args: --version stops parsing" "[[ \$out == '1|1' ]]"

out=$(rst "parse_args -C '$tmp/proj' --agent plan")
check "parse_args: --agent is passed to OpenCode" \
  "[[ \$out == '0|default|$tmp/proj|--agent plan' ]]"

out=$(rst "parse_args -C '$tmp/proj' run --agent plan hi")
check "parse_args: subcommand options are passed to OpenCode" \
  "[[ \$out == '0|default|$tmp/proj|run --agent plan hi' ]]"

# ---------------------------------------------------------------------------
# load_config
# ---------------------------------------------------------------------------
printf 'agent opencode\n' > "$tmp/cfg-agent"
printf 'agent omp\n' > "$tmp/cfg-agent-omp"
printf 'shared\n' > "$tmp/cfg-shared"
printf 'profile work\n' > "$tmp/cfg-profile"

out=$(snippet "load_config '$tmp/cfg-agent' 2>&1; echo loaded")
check "load_config: a legacy 'agent opencode' directive is accepted" \
  "[[ \$out == loaded ]]"

out=$(snippet "load_config '$tmp/cfg-agent-omp'" 2>&1)
rc=$?
check "load_config: another agent is rejected" \
  "[[ $rc -eq 1 && \$out == *'only opencode is supported'* ]]"

out=$(snippet "isolated=1; load_config '$tmp/cfg-shared'; printf '%s\n' \"\$isolated\"")
check "load_config: shared directive" "[[ \$out == 0 ]]"

out=$(snippet "load_config '$tmp/cfg-profile'; printf '%s|%s\n' \"\$isolated\" \"\$profile\"")
check "load_config: profile implies isolated" "[[ \$out == '1|work' ]]"

# ---------------------------------------------------------------------------
# Config generation helpers
# ---------------------------------------------------------------------------
generated=$tmp/generated/config
snippet "write_default_config '$generated'"
check "write_default_config: creates the file" "[[ -f $generated ]]"
check "write_default_config: has no agent directive" "! grep -q '^agent' '$generated'"
check "write_default_config: includes commented examples" "grep -qx '# no-net' '$generated'"

out=$(snippet "write_default_config '$generated'" 2>&1)
rc=$?
check "write_default_config: never overwrites" \
  "[[ $rc -eq 1 && \$out == *'already exists'* ]]"

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

# A symlink in a tool directory that points at a parent of the project would
# stack a read-only mount over the project.
mkdir -p "$tmp/home/.local/bin" "$tmp/work/proj"
ln -s "$tmp/work" "$tmp/home/.local/bin/parent"
out=$(snippet "$mh
project_dir=$tmp/work/proj
BWRAP=(); mounted=()
add_ro_symlink_targets '$tmp/home/.local/bin' 2>&1
echo \"\${#BWRAP[@]}\"")
check "add_ro_symlink_targets: skips a parent of the project" \
  "[[ \$out == *'parent of the project'* && \$out == *0 ]]"
rm -rf "$tmp/home/.local/bin/parent" "$tmp/work"

# git_common_allowed: a .git file pointing at a repository rooted at HOME (a
# dotfiles repository) or at an unrelated ancestor's .git is refused.
mkdir -p "$tmp/home/.git" "$tmp/home/code/evil" "$tmp/home/code/plain"
printf 'gitdir: %s\n' "$tmp/home/.git" > "$tmp/home/code/evil/.git"
out=$(snippet "$mh
project_dir=$tmp/home/code/evil
git_common_allowed '$tmp/home/.git' && echo allowed || echo refused")
check "git_common_allowed: refuses an ancestor .git named by a .git file" \
  "[[ \$out == refused ]]"
out=$(snippet "$mh
project_dir=$tmp/home/code/plain
git_common_allowed '$tmp/home/.git' && echo allowed || echo refused")
check "git_common_allowed: refuses a repository rooted at HOME" \
  "[[ \$out == refused ]]"
mkdir -p "$tmp/super/.git/modules/sub" "$tmp/super/sub"
printf 'gitdir: ../.git/modules/sub\n' > "$tmp/super/sub/.git"
git config --file "$tmp/super/.git/modules/sub/config" core.worktree ../../../sub
out=$(snippet "$mh
project_dir=$tmp/super/sub
git_common_allowed '$tmp/super/.git/modules/sub' && echo allowed || echo refused")
check "git_common_allowed: accepts a genuine submodule" "[[ \$out == allowed ]]"
git config --file "$tmp/super/.git/modules/sub/config" core.worktree ../../../elsewhere
out=$(snippet "$mh
project_dir=$tmp/super/sub
git_common_allowed '$tmp/super/.git/modules/sub' && echo allowed || echo refused")
check "git_common_allowed: refuses a submodule whose worktree differs" \
  "[[ \$out == refused ]]"
rm -rf "$tmp/home/.git" "$tmp/home/code"

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
oc_args=($1)
build_agent_command
printf '%s\n' \"\${cmd[*]}\""
}

out=$(agent_cmd '')
check "build_agent_command: the TUI gets --standalone" \
  "[[ \$out == '/bin/true --standalone' ]]"

out=$(agent_cmd 'run -x')
check "build_agent_command: --standalone follows the subcommand" \
  "[[ \$out == '/bin/true run --standalone -x' ]]"

out=$(agent_cmd '--standalone run')
check "build_agent_command: leading --standalone is relocated" \
  "[[ \$out == '/bin/true run --standalone' ]]"

out=$(agent_cmd 'auth login')
check "build_agent_command: nested subcommands get --standalone" \
  "[[ \$out == '/bin/true auth login --standalone' ]]"

for sub in service upgrade pair; do
  out=$(agent_cmd "$sub" 2>&1)
  rc=$?
  check "build_agent_command: opencode $sub is refused" \
    "[[ $rc -eq 1 && \$out == *refusing* ]]"
done

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
printf '\n%d passed, %d failed\n' "$pass" "$fail"
(( fail == 0 ))
