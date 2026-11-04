# End-to-end: real published Go modules via graalgo

This directory holds end-to-end proofs: real published Go modules compiled
**as-published** (never patched), with output byte-identical to `go run`
(gc 1.27.1).

## Layout

- [uuid/](uuid/) — `github.com/google/uuid` probe cases (moved here from
  `modprobe/uuid/`, 2026-10-14). Each `caseNN/main.go` is a package-main
  program exercising the module; `go.mod` pins the module for the gc leg.
- [run_module.sh](run_module.sh) — differential runner: `go run` (gc) vs
  graalgo, byte-compare stdout and exit codes. Moved here from
  `modprobe/harness/`, 2026-10-14. Rewired 2026-11-04 (review NEW-P1-9)
  from the deleted `phases/phase*` layout to the current backend: it
  invokes [internal/backend/graalgo-sync-run](../../internal/backend/graalgo-sync-run)
  (P3SyncMain, `--gmp=1` and `--gmp=4` legs) with the gofrontend binary
  at [internal/frontend/gofrontend](../../internal/frontend/gofrontend).
- [BUGS.md](BUGS.md) — module-probe bug log (moved here from
  `modprobe/bugs/`, 2026-10-14). Every entry names the general fix or the
  blocking graalgo gap; module sources are never patched.

## Running it

Prerequisite at the repo root (builds the gofrontend binary and the
backend classes the launcher needs; the script itself never builds):

```
make generate build
```

Then run one probe (or all its cases) — wired into the root Makefile as
`make test-e2e`, which runs the uuid probe:

```
make test-e2e
test/e2e/run_module.sh test/e2e/uuid           # all uuid cases
test/e2e/run_module.sh test/e2e/uuid case01    # a single case
```

The script fails fast (exit 2) with an actionable message if the
gofrontend binary or the backend classes are missing. Module imports
resolve from the local module cache via gofrontend's disk importer; the
gc leg runs first and populates it, so no network is needed when the
module version is already cached.
