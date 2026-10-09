# wave-3 `internal/godebug` corpus

Scope-limited corpus for `internal/godebug`. There is no synthetic
adapter: the package needs the generated `internal/godebugs` table plus
runtime linknamed hooks (`setUpdate`, `registerMetric`,
`setNewIncNonDefault`), and there is no in-tree consumer.

- `00_godebug.go` is `.scopelimited` + `nogc` (external programs cannot
  import `internal/*` at all):
  `reason: external programs cannot import internal/* (go tool
  restriction); no synthetic adapter — needs the generated
  internal/godebugs table plus runtime linknamed hooks
  (setUpdate/registerMetric), and there is no in-tree consumer`,
  `retest: internal/godebug runtime integration lands (godebugs table +
  update/metric hooks)`.
- The program pins the pure API surface (`New`, `Name`, `Undocumented`);
  `Value()`/`String()` are deliberately not called (they panic for names
  not listed in the godebugs table, by design). Build-checked against the
  real package in the tier1d harness.
