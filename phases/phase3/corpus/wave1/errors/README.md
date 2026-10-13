# P3-M7 wave-1 `errors` corpus

Go programs exercising the wave-1 synthetic `errors` package
(`internal/frontend/synth_errors.go`: real-Go New/Is/Unwrap/Join;
As/AsType fail-fast — they need reflect), differential vs stock gc
1.27.1. Run via `run_differential.sh` (thin wrapper over
`corpus/wave1/run_wave.sh`; conventions: `corpus/wave1/README.md`).

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

## Known divergences (fail-fast, never silent)

- `errors.As` / `errors.AsType`: exact gc 1.27.1 signatures, panic
  bodies — a real implementation needs `reflect` (unavailable to v1
  synthetic packages). No corpus program calls them; re-test when
  reflect lands.
- `errors.Is` with an uncomparable target returns false exactly like gc
  (comparability is probed with `recover` — `er03` covers it).
- v1 has no variadic argument packing (WS D): `errors.Join` must be
  called with explicit `...` spread.
