# Gate 1: `go test ./...` Trial Harness

Phase-3 Gate 1 conformance trial: run upstream Go stdlib tests via graalgo
and compare against gc 1.27.1.

## Design

Graalgo's frontend is single-file today (no package-mode `go test`).
The harness synthesizes single-file drivers from stdlib test sources:

1. **[discover.py](discover.py)** — extract `TestXxx` functions from `$GOROOT/src/<pkg>/*_test.go`
2. **[synthesize.py](synthesize.py)** — generate a `main`-package driver:
   - Inlines the testing shim (package main)
   - Hoists imports to the driver header
   - Rewrites `*testing.T` → `*T`, strips benchmarks
   - Handles dot-import vs qualified import styles
3. **[shim/shim.go](shim/shim.go)** — minimal `testing.T` replacement (`Errorf`, `Fatalf`,
   `Run`, `Skip`, etc.). Does NOT support: `Cleanup` (no-op), `Parallel`
   (no-op), benchmarks.
4. **[run.py](run.py)** — differential runner: builds driver via `go run` (gc) and
   `graalgo-run`, compares per-test PASS/FAIL/SKIP. Never reports PASS
   when both legs fail (verdict `ERROR`) or when the gc leg produced no
   parsable results.
5. **[test_run_verdict.py](test_run_verdict.py)** — self-test for the verdict
   logic (fail-open regression): drives [run.py](run.py) with fake
   toolchains. Run with `python3 tools/gate1/test_run_verdict.py`.

## Usage

```bash
# Single package
python3 tools/gate1/run.py strconv --out /tmp/gate1-strconv

# Via make (see root Makefile)
make gate1-pilot   # 5-package pilot
make gate1 PKG=sort # single package
```

## Pilot Results (2026-10-13, master @ 3af2572)

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

- Internal test packages (`package foo`) skipped — need unexported access
- Tests using `unsafe`, `slices`, `iter`, `internal/*` skipped
- `testing.Cleanup` is a no-op; `testing.Parallel` is a no-op
- Benchmarks stripped (only `TestXxx` run)

## Graalgo Findings (for the fix queue)

1. **COMPLEX128 -> COMPLEX64 conversion**: `illegal conversion` in
   ConvertNode. Blocks any test using `math/cmplx`.
2. **Generic method `errors.As`**: `AsType is not in M7 scope`.
3. **Method values**: `selector call ms.less is not supported in v1 scope`.
4. **Function variables**: `call of non-function variable cleanups is not
   supported` (worked around in shim by removing Cleanup support).
