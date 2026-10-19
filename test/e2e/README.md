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

Requires `make generate build` at the repo root first (the gofrontend
binary and the backend classes). Then:

```bash
make e2e        # all probes
# or directly:
test/e2e/run_module.sh test/e2e/uuid
test/e2e/run_module.sh test/e2e/uuid case01 case02   # selected cases
```

`go.sum` in each probe directory is **tracked**: it pins the gc leg's
module checksums so the probe is reproducible without network access.
(Recorded decision, 2026-10-18: keep-and-track, not delete.)
