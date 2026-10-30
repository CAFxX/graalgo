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

## Status

`run_module.sh` invokes [internal/backend/graalgo-run](../../internal/backend/graalgo-run)
(the phase-1 launcher; it takes no `--gmp` flag, so there is a single
graalgo leg per case) and builds the backend (`make build`, plus `make
generate` for gofrontend when missing) before running. Panic cases compare
the full normalised stderr via the traceback corpus's `normalize.py`, not
just the first line.
