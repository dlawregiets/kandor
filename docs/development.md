# Development

## Tests

`make test` runs `go vet` and the Go tests. `make check` also builds a static
binary and runs the smoke tests:

```sh
make test
make check
# or: go test ./... && make build && ./tests/smoke.sh
```

The Go tests cover the command-line parser, configuration loading and
migration, the mount planner, git protection, environment filtering and the
OpenCode command builder without starting a sandbox. `tests/smoke.sh` runs
real sandboxes with the built `bin/kandor` and checks the sandbox boundary,
private `/tmp`, hidden home, read-only `/etc`, isolated profiles, extra
mounts, network isolation, host service-password isolation, git protection
and signal handling. Neither calls an LLM; the smoke tests need `bwrap` and
`opencode`.

## Continuous integration and releases

`.github/workflows/ci.yml` runs on every push and pull request. It checks
formatting, runs `go vet` and the Go tests with the race detector, and builds
static `linux/amd64` and `linux/arm64` binaries as artifacts. Feature and fix
branches can therefore be tested before they merge into `develop`, and release
or hotfix branches before they merge into `main`. The smoke tests are not run
there because they need a real `opencode` binary; run `make check` locally.

Releases follow GitFlow. Merge the release or hotfix branch into `main`, then
tag the merge commit:

```sh
git tag -a v0.4.0 -m "kandor 0.4.0"
git push origin v0.4.0
```

The pushed tag becomes the binary version; there is no version file to keep in
sync. Local and CI builds are stamped with `git describe` output instead: the
nearest tag plus any commits since it, or the commit SHA when no tag describes
the build.

`.github/workflows/release.yml` checks that the tag is a semantic version and
that stable tags are reachable from `main`; pre-release tags may also be on a
`release/*` or `hotfix/*` branch. It then runs the tests and publishes static
tarballs for both architectures plus `SHA256SUMS`. Tags with a pre-release
suffix such as `v0.4.0-rc.1` are marked as GitHub pre-releases.

