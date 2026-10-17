# wave-3 `internal/bytealg` corpus

Scope-limited corpus for the synthetic `internal/bytealg` package
(`internal/frontend/synth_bytealg.go`): portable real-Go
implementations of the gc 1.27.1 exported surface (Compare, CompareString,
Count, CountString, Equal, Index, IndexString, Cutover, IndexByte,
IndexByteString, LastIndexByte, LastIndexByteString, IndexRabinKarp,
LastIndexRabinKarp, MakeNoZero, MaxBruteForce, MaxLen).

The package is synthetic-only (like `internal/reflectlite`): no user
program can import `internal/*`, so `00_functions.go` is `.scopelimited` +
`nogc` — neither leg runs under `run_wave3.sh`.

## Correctness evidence (the tier1d harness)

The tier1d harness (research scratch, removed with the `phases/` layout)
extracts
`syntheticBytealgSource` to a GOPATH (`src/internal/bytealg/`) and runs
this exact corpus program under stock gc 1.27.1 against it. The program is
self-checking: every result is cross-checked against the stdlib wrappers
(`bytes`/`strings`) that consume bytealg, over empty/singleton/text/
binary/4KB inputs and a separator matrix incl. empty and longer-than-input
separators. Any `MISMATCH:` line panics → harness FAIL.

## Scope boundary

Deviations from gc (all unobservable, documented in `synth_bytealg.go`):
D1 Index/IndexString are portable brute force (gc's portable fallback
panics; real platforms use assembly); D2 MaxBruteForce=0, MaxLen=0 (the gc
portable values); D3 MakeNoZero returns zeroed memory (contract: len n,
cap >= n); D4 the `offset*` assembly consts are omitted (no assembly
consumer). Retest trigger: a synthetic `internal/cpu` landing (D4).
