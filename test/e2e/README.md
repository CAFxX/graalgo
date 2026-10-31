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
  `modprobe/harness/`, 2026-10-14.
- [BUGS.md](BUGS.md) — module-probe bug log (moved here from
  `modprobe/bugs/`, 2026-10-14). Every entry names the general fix or the
  blocking graalgo gap; module sources are never patched.

## Status: manual-only (not in `make all`, not in CI)

This suite is deliberately **not wired into the Makefile's `all` target
or any CI job** — and that is a recorded decision, not an oversight.
`run_module.sh` still targets the pre-restructure backend layout
(`phases/phase3` classpaths, `P3SyncMain --gmp` legs, default worktree
`m9-modprobe`): those paths no longer exist, so running it today fails
obscurely and wiring it into CI would red-gate every push. It needs
updating to invoke the current product launcher
([internal/backend/graalgo-run](../../internal/backend/graalgo-run) /
`graalgo-sync-run`) before it runs again — that update belongs to the
driver-hardening lane, which owns the runner; this lane does not rewrite
another lane's runner on spec.

What exists instead:

- `make test-e2e` — a target that fails fast (exit 2) with this
  explanation while the runner still references `phases/`, and runs
  `run_module.sh test/e2e/uuid` once the guard passes. It is NOT in
  `all` and NOT in CI until the runner update lands and the suite passes
  locally first.
- To unblock CI wiring, the driver-hardening lane must: (1) re-plumb
  `run_module.sh` to the current backend layout, (2) show a green local
  run over `uuid/case01-08`, (3) update this section and add the target
  to `all`/CI in the same change. Until then, treat any claim that the
  e2e suite "runs in CI" as false.
