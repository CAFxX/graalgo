# `math/rand/v2` wave-3 corpus

Differential corpus for the synthetic `math/rand/v2` package (full port).

## Scope boundary

Deterministic fixed seeds only. ChaCha8 verified byte-identically vs gc.
Deviations (see `synth_mathrandv2.go` header):
- `runtime_rand` → composed from `runtime.fastrand`.
- `internal/chacha8rand` inlined as `__chacha8*` (unsafe-free rewrite).
- Exported multi-result methods: PCG AppendBinary/MarshalBinary dropped;
  ChaCha8 Read/AppendBinary/MarshalBinary ported (review-1616 P1-5).

## Programs

- `00_pcg.go` — NewPCG streams, Seed determinism, Rand methods.
- `01_chacha8.go` — NewChaCha8 streams (byte-identical), Seed determinism,
  Read (varied lengths, determinism, interleaved Uint64/Read),
  Marshal/Unmarshal round-trip with pending readBuf.
- `02_adversarial.go` — Large-n rejection, range checks, Shuffle/Perm.

## Results

TBD (2026-10-09).
