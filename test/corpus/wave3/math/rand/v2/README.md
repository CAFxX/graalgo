# `math/rand/v2` wave-3 corpus

Differential corpus for the synthetic `math/rand/v2` package (full port).

## Scope boundary

Deterministic fixed seeds only. ChaCha8 verified byte-identically vs gc.
Deviations (see `synth_mathrandv2.go` header):
- `runtime_rand` → composed from `runtime.fastrand`.
- `internal/chacha8rand` inlined as `__chacha8*` (unsafe-free rewrite).
- ChaCha8.Read/AppendBinary/MarshalBinary keep their gc signatures as
  fail-fast stubs and are served by `__-helpers` via the Appendix-M
  desugar (R4); PCG.AppendBinary/MarshalBinary are still dropped.

## Programs

- `00_pcg.go` — NewPCG streams, Seed determinism, Rand methods.
- `01_chacha8.go` — NewChaCha8 Uint64/Read streams (byte-identical),
  Seed determinism, Marshal/Unmarshal round-trips (plain and with a
  partial read pending), interface dispatch through `io.Reader`.
  Restored from `01_chacha8.go.bak` by R4; the `.scopelimited`
  marker was deleted with it (the "& on SelectorExpr" scope limit
  that blocked it no longer holds).
- `02_adversarial.go` — Large-n rejection, range checks, Shuffle/Perm.

## Results

TBD (2026-10-09).
