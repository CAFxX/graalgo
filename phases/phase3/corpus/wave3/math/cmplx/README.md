# `math/cmplx` wave-3 corpus

Differential corpus for the synthetic `math/cmplx` package (full port).

## Scope boundary

The whole package is in scope. Deviations (see `synth_mathcmplx.go` header):
- Complex arithmetic manualized (frontend has no complex wire nodes).
- `Polar` via Appendix M; `Sincos` desugars to `__sincos`.
- `sinhcosh` via `__cmplxSinhcosh` + pair.

## Programs

- `00_abs_phase.go` — Abs, Phase, Rect, Polar.
- `01_exp_log.go` — Exp, Log, Log10, Pow.
- `02_trig.go` — Sin, Cos, Tan, Sinh, Cosh, Tanh.
- `03_sqrt.go` — Sqrt.
- `04_inverse.go` — Asin, Acos, Atan, Asinh, Acosh, Atanh.
- `05_infnan.go` — Inf/NaN propagation, IsInf/IsNaN.

## Results

TBD (2026-10-09).
