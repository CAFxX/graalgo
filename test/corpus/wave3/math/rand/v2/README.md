# `math/rand/v2` wave-3 corpus

Differential corpus for the synthetic `math/rand/v2` package (full port).

## Scope boundary

Deterministic fixed seeds only. ChaCha8 verified byte-identically vs gc
(including Read and the Marshal/Unmarshal round-trip; see the
review23-chacha8 lane report and the gc harness it left behind).
Deviations (see `synth_mathrandv2.go` header):
- `runtime_rand` → composed from `runtime.fastrand`.
- `internal/chacha8rand` inlined as `__chacha8*` (unsafe-free rewrite).
- PCG.AppendBinary/MarshalBinary still dropped (no in-tree consumer).
  ChaCha8.Read/AppendBinary/MarshalBinary are carried since
  review23-chacha8 (the exporter pair-transforms them); the old
  scope-limit that kept `01_chacha8.go` disabled (`&` on SelectorExpr)
  is obsolete — the program is restored.

## Programs

- `00_pcg.go` — NewPCG streams, Seed determinism, Rand methods.
- `01_chacha8.go` — NewChaCha8 streams (byte-identical), Seed determinism.
- `02_adversarial.go` — Large-n rejection, range checks, Shuffle/Perm.

## Results

TBD (2026-10-09).
