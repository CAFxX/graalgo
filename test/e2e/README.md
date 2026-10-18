# End-to-end: real published Go modules via graalgo

This directory holds end-to-end proofs: real published Go modules compiled
**as-published** (never patched), with output byte-identical to `go run`
(gc 1.27.1).

## Layout

- [uuid/](uuid/) — `github.com/google/uuid` probe cases. Each
  `caseNN/main.go` is a package-main program exercising the module;
  `go.mod` pins the module for the gc leg. The module itself is never
  vendored here: it resolves unmodified from the local Go module cache.
- [run_module.sh](run_module.sh) — differential runner: gc leg (`go build`
  + run) vs the graalgo leg
  ([internal/backend/graalgo-sync-run](../../internal/backend/graalgo-sync-run),
  the product launcher), byte-compare stdout and exit codes. Rewired
  2026-10-18 to the current tree (was: `phases/phase{1,2,3}` classpaths,
  removed 2026-10-14).
- [BUGS.md](BUGS.md) — module-probe bug log. Every entry names the general
  fix or the blocking graalgo gap; module sources are never patched.

## Running

```sh
# prerequisites (checked by the script, never built by it):
make build      # at the repo root: backend classes
make generate   # at the repo root: internal/frontend/gofrontend

# the whole uuid probe (default probe dir):
test/e2e/run_module.sh

# a different probe dir, and/or a subset of cases:
test/e2e/run_module.sh /path/to/probe case01 case03
```

Environment overrides: `GR` (repo root; default derived from the script
location), `BACKEND` (`graalgo-sync-run` default, or `graalgo-run` for the
truffle-only launcher — single leg, no `--gmp` surface), `GO` (gc toolchain),
`GRAALVM_HOME`, `TMPDIR` (scratch; exported so the launcher's
`java.io.tmpdir` follows it).

## Hermeticity

The runner touches no network: `GOFLAGS=-mod=mod`, `GOPROXY=off`,
`GOTOOLCHAIN=local`. The probe module must already sit in the local module
cache — if the preflight fails, run `go mod download` in the probe dir once
(the one network step), then re-run.

Exit code is 0 iff every case passes every leg; the runner prints a
per-case table.

## Contract

- Module sources are **never patched** (hard rule). A graalgo failure on
  unmodified sources is a graalgo gap: add a [BUGS.md](BUGS.md) entry with a
  minimal reproducer and the general fix (or the blocking gap), and fix
  graalgo — never the module.
- Panic programs: the first stderr line is compared (wave3 convention);
  exit codes must match exactly.
