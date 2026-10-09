# wave-3 `internal/bisect` corpus

Scope-limited corpus for `internal/bisect`. There is no synthetic
adapter: the matcher's stack-pattern core needs `runtime.Callers` /
`runtime.CallersFrames`, and there is no in-tree consumer.

- `00_bisect.go` is `.scopelimited` + `nogc` (external programs cannot
  import `internal/*` at all):
  `reason: external programs cannot import internal/* (go tool
  restriction); no synthetic adapter — the matcher's stack-pattern core
  needs runtime.Callers/CallersFrames, and there is no in-tree consumer`,
  `retest: internal/bisect runtime integration lands (stack-capture
  intrinsics)`.
- The program pins the pure functions (`New` incl. the empty-pattern
  `(nil, nil)` rule, `Marker`/`CutMarker` round-trip with the one-space
  removal rule, `Hash` determinism, `AppendMarker`). Build-checked against
  the real package in the tier1d harness.
