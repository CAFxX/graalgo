# Gate 1: `go test ./...` Trial Harness

Phase-3 Gate 1 conformance trial: run upstream Go stdlib tests via graalgo
and compare against gc 1.27.1.

## Design

The harness compares the two toolchains on the same package:

1. **Synthesize the driver** with the graalgo driver itself:
   `graalgo test -emit-driver` (see [cmd/graalgo/testsynth.go](../../cmd/graalgo/testsynth.go)).
   Test-driver synthesis has exactly one implementation — the Go one
   used by `graalgo test`. This harness never synthesizes drivers
   itself: the old Python synthesizer (`synthesize.py`, `discover.py`,
   `shim/`) was deleted in the review-1105-gate1-falsepass lane so the
   two implementations could not drift apart.
2. **Run via gc**: real `go test -v` on the package — the reference.
   (The driver is *not* run under gc: for packages with internal test
   files the driver inlines the package's own sources, which import
   `internal/*` packages that plain `go run` cannot resolve outside
   GOROOT. The old harness papered over the same gap with hard-coded
   per-package replicas; the reference side now needs no synthesis.)
3. **Run via graalgo**: the synthesized driver through
   `graalgo-sync-run` — the same launcher `graalgo test` uses — with
   `--test-package-dir` so the frontend grants internal visibility.
4. **run.py** — differential runner: compares per-test
   go-test-format verdicts (`--- PASS/FAIL/SKIP` lines) from both
   sides and reports the verdict.

## Verdicts

The verdict is evidence-driven, over the reported per-test results:

| Verdict | Meaning |
|---------|---------|
| PASS | Both sides reported the same per-test verdicts. (A test failing identically under both is agreement — the gate compares graalgo against gc, it does not require the tests themselves to pass.) |
| DIVERGE | Per-test verdicts differ, or graalgo reported nothing while gc reported results (graalgo-side crash). |
| SKIP | gc reported nothing (build/test failure on the reference side); the harness cannot validate this package. |
| ERROR | Neither side reported any test result, or a side exited 0 while reporting nothing. No evidence is never a PASS. |

## Usage

```bash
# Single package
python3 tools/gate1/run.py strconv --out /tmp/gate1-strconv

# Via make (see root Makefile)
make gate1-pilot   # 5-package pilot
make gate1 PKG=sort # single package
```

## Pilot Results (2026-10-13, master @ 3af2572, OLD harness)

The table below is the old harness's pilot (Python synthesis,
same-driver-under-both-toolchains). It is kept as history; the
harness has since changed (single synthesis via
`graalgo test -emit-driver`, gc side is real `go test`), so re-run
the pilot to refresh it.

| Package | Tests | gc | graalgo | Verdict | Root cause |
|---------|-------|----|---------|---------|------------|
| strconv | 37 | 37 pass | frontend crash | DIVERGE | `illegal conversion: COMPLEX128 -> COMPLEX64` (math/cmplx) |
| errors | 10 | 10 pass | frontend error | DIVERGE | `errors.AsType is not in M7 scope` (generics) |
| sort | 0* | — | frontend error | DIVERGE | `selector call ms.less not supported` (method values) |
| strings | 0* | — | — | SKIP | All tests need `unsafe`/`slices`/`iter` |
| bytes | — | — | — | not run | — |

*0 tests: all test files require unsupported imports (harness limitation,
not graalgo).

## Harness Limitations (not graalgo bugs)

- `TestMain` is not run by the synthesized driver (inherited from
  `graalgo test`: the driver calls each `TestXxx` directly).
- The `graalgo test` shim has no `Cleanup`/`Parallel`: a test calling
  them fails to build the driver (same limitation as `graalgo test`
  itself; the old Python shim no-oped them instead).
- Benchmarks are not run (only `TestXxx`).

## Graalgo Findings (for the fix queue)

1. **COMPLEX128 -> COMPLEX64 conversion**: `illegal conversion` in
   ConvertNode. Blocks any test using `math/cmplx`.
2. **Generic method `errors.As`**: `AsType is not in M7 scope`.
3. **Method values**: `selector call ms.less is not supported in v1 scope`.
4. **Function variables**: `call of non-function variable cleanups is not
   supported` (worked around in shim by removing Cleanup support).
