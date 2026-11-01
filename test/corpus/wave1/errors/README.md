# P3-M7 wave-1 `errors` corpus

Go programs exercising the `errors` package, differential vs stock gc
1.27.1. Run via `run_differential.sh` (thin wrapper over
`corpus/wave1/run_wave.sh`; conventions: `corpus/wave1/README.md`).

Frontend note (2026-10-31, rem-review-20261031-p1-1): the phase-1
synthetic `errors` package (`internal/frontend/synth_errors.go`) is
retired — the graalgo leg now resolves real GOROOT `errors` via the disk
importer. The divergence notes below were written against the old
synthetic; re-test them via differential — notably `errors.As`, which is
a real implementation in the disk package at the frontend level (its
runtime behavior still depends on the reflect runtime).

## Programs

| Program | Covers |
|---|---|
| `er00_new.go` | `New`, `Error`, distinct values, `ErrUnsupported` |
| `er01_is_basic.go` | `Is`: direct match, miss, nil cases |
| `er02_is_tree.go` | `Is` over `Unwrap` chains, custom `Is` method, 200-deep nesting |
| `er03_is_adversarial.go` | uncomparable target/err (recover probe), nil `Unwrap`, `Join` tree |
| `er04_unwrap.go` | `Unwrap`: single, absent, `Join` (`Unwrap() []error` not unwrapped) |
| `er05_join.go` | `Join` formatting, nil filtering, all-nil/empty → nil, nesting |
| `er06_combined.go` | errors + sort: join failures, sort messages, `SearchStrings` |
| `er07_astype.go` | `AsType`: direct/wrapped/Join-tree match, miss, nil, interface E, `error` E, custom `As(any)` method, first-match-wins, nil `Unwrap` |

## Known divergences (fail-fast, never silent)

- `errors.As`: exact gc 1.27.1 signature, panic body — a real
  implementation needs `reflect` (arbitrary `*T` target validation and
  setting; the synthetic reflect package's `Implements`/`AssignableTo`/
  `Set` are still fail-fast intrinsics). No corpus program calls it;
  re-test when the reflect runtime lands.
- `errors.AsType`: real implementation (generics; needs no reflect) —
  covered by `er07_astype.go`.
- `errors.Is` with an uncomparable target returns false exactly like gc
  (comparability is probed with `recover` — `er03` covers it).
- v1 has no variadic argument packing (WS D): `errors.Join` must be
  called with explicit `...` spread.
