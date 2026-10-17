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

## Running

`make build` at the repo root first (the runner never builds — the
[product launcher](../../internal/backend/graalgo-sync-run) needs the
truffle+runtime+interop classes), then:

```bash
./run_module.sh uuid            # all 8 uuid cases
./run_module.sh uuid case01     # one case
```

Each case runs under stock gc (built binary) and under graalgo at
`--gmp=1` and `--gmp=4`; stdout bytes, exit codes, and (for panics) the
first stderr line must match. Exit 0 iff every case passes every leg.

CI: the weekly [e2e-modules](../../.github/workflows/e2e-modules.yml)
workflow runs the uuid probe (schedule + manual dispatch); results feed
the as-published-modules milestone.

## Status

`run_module.sh` was rewired to the post-restructure layout (2026-10-17,
lane `rev-test-runners`): it now invokes the
[product launcher](../../internal/backend/graalgo-sync-run) (P3SyncMain,
same backend as the [diffharness smoke corpus](../differential/run.sh))
instead of the removed `phases/phase3` tree.
