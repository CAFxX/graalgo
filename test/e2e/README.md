# End-to-end: real published Go modules via graalgo

This directory holds end-to-end proofs: real published Go modules compiled
**as-published** (never patched), with output byte-identical to `go run`
(gc 1.27.1).

## Layout

- [uuid/](uuid/) — `github.com/google/uuid` probe cases (moved here from
  `modprobe/uuid/`, 2026-10-14). Each `caseNN/main.go` is a package-main
  program exercising the module; `go.mod` pins the module for the gc leg.
- [BUGS.md](BUGS.md) — module-probe bug log (moved here from
  `modprobe/bugs/`, 2026-10-14). Every entry names the general fix or the
  blocking graalgo gap; module sources are never patched.

## Status

> **RETIREMENT NOTICE (2026-10-16, review-r2-phases-sweep):**
> `run_module.sh` was deleted — it referenced the `phases/phase{1,2,3}`
> trees deleted in the Phase-4 restructure (dead on arrival) and was
> superseded by the live differential harness
> ([test/differential/](../differential/) + [`test/differential/cmd/diffharness`](../differential/cmd/diffharness)). The
> `uuid/` probe cases remain as data. Re-running module probes against the
> new layout is an open follow-up (see the driver-hardening track).
