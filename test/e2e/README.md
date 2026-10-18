# End-to-end: real published Go modules via graalgo

This directory holds end-to-end proofs: real published Go modules compiled
**as-published** (never patched), with output byte-identical to `go run`
(gc 1.27.1).

## Layout

- [uuid/](uuid/) — `github.com/google/uuid` probe cases (moved here from
  `modprobe/uuid/`, 2026-10-14). Each `caseNN/main.go` is a package-main
  program exercising the module; `go.mod` pins the module for the gc leg.
- [run_module.sh](run_module.sh) — differential runner: `go build`+run (gc)
  vs graalgo, byte-compare stdout and exit codes. Moved here from
  `modprobe/harness/`, 2026-10-14; rewired 2026-10-18 to invoke
  [internal/backend/graalgo-sync-run](../../internal/backend/graalgo-sync-run)
  (the product launcher) with `--gmp=1`/`--gmp=4` legs and per-case
  `timeout`s.
- [BUGS.md](BUGS.md) — module-probe bug log (moved here from
  `modprobe/bugs/`, 2026-10-14). Every entry names the general fix or the
  blocking graalgo gap; module sources are never patched.

## Running

`make test-e2e` (repo root): builds the frontend and backend, primes the module cache
(`go mod download` — needs network the first time), then runs
`run_module.sh` over [uuid/](uuid/) — 8 cases, each compared on stdout,
exit code, and (for panics) the first stderr line, with `GR_TIMEOUT`
(default 300s) killing hung backend legs as TIMEOUT failures.

> Environment note: the graalgo legs shell out to `gofrontend` with its
> stdout redirected to a temp file. In sandboxes that deny regular-file
> writes to subprocesses, every graalgo leg fails with
> `internal error building AST: JSON parse error at line 1, col 1:
> unexpected end of input` while the gc leg stays green — that signature
> means the sandbox ate the frontend output, not that the backend is
> broken (verify: `gofrontend <file> | wc -c` prints ~100MB of JSON while
> `gofrontend <file> > out.json` yields a 0-byte file). Run `make test-e2e`
> in CI or an unfiltered environment instead.

## Status

`run_module.sh` is functional again (rewired 2026-10-18 against
[internal/backend/graalgo-sync-run](../../internal/backend/graalgo-sync-run);
per-case timeouts; module cache primed up front). It runs in CI's
`full-suite` job (weekly + manual dispatch, like the other long
differentials) via `make test-e2e`, not per-push.
