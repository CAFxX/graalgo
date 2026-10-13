# `math/rand` wave-3 corpus

Differential corpus for the synthetic `math/rand` package (full port,
supersedes the minimal stub).

## Scope boundary

Deterministic fixed seeds only (no time-based seeds). The global
auto-seeded top-level functions are nondeterministic under gc and are
not pinned. Deviations (see `synth_mathrand.go` header):
- `internal/godebug` → local stub (GODEBUG settings not observable).
- `atomic.Pointer[Rand]` → mutex-guarded lazy init.
- `runtime_rand` → composed from `runtime.fastrand`.

## Programs

- `00_source.go` — NewSource streams, Seed determinism, Uint32/Int31/Intn/Float64/Shuffle/Perm.
- `01_distributions.go` — ExpFloat64, NormFloat64, Zipf, Read.
- `02_adversarial.go` — Large-n rejection paths, range checks, interleave.
- `03_intn_panic.go` — Intn(0) panic (`.panic` marker).

## Results

TBD (2026-10-09).
