# `math/rand/v2` wave-3 corpus

Differential corpus for the synthetic `math/rand/v2` package (full port).

## Scope boundary

Deterministic fixed seeds only. ChaCha8 verified byte-identically vs gc.
Deviations (see `synth_mathrandv2.go` header):
- `runtime_rand` → composed from `runtime.fastrand`.
- `internal/chacha8rand` inlined as `__chacha8*` (unsafe-free rewrite).
- Exported multi-result methods (Read/AppendBinary/MarshalBinary) dropped.

## Programs

- `00_pcg.go` — NewPCG streams, Seed determinism, Rand methods.
- `01_chacha8.go` — NewChaCha8 streams (byte-identical), Seed determinism.
- `02_adversarial.go` — Large-n rejection, range checks, Shuffle/Perm.

## Results

- 2026-11-09 (R10-10, base `d4936e9b`): `00_pcg.go` and `02_adversarial.go`
  byte-identical to gc (`math/rand/v2`, go1.27.1) under graalgo. The October
  PCG Uint64 mismatch was stale: the synthetic source was already correct
  under gc, and the transformation defect (missing UINT32 masking in
  `ConvertNode.extendLong`) was cured by `5d212af7` (2026-10-10); the
  scope-limits were never re-tested. `01_chacha8.go` remains scope-limited
  (`&` on SelectorExpr, WS D).
