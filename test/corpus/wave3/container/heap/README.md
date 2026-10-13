# `container/heap` wave-3 corpus

Differential corpus for the synthetic `container/heap` package
(pure-Go port of the stdlib heap operations).

## Scope boundary

The whole package is in scope: `Interface`, `Init`, `Push`, `Pop`,
`Remove`, `Fix`. No `.scopelimited` programs — the package is pure Go
with no runtime, syscall, or arch dependencies.

## Programs

- `00_basic.go` — Init + Pop ordering, Push-after-drain, Len tracking.
- `01_fix_remove.go` — Fix after priority change (both directions),
  Remove by index, Remove-of-last-element branch.
- `02_adversarial.go` — duplicates, 300-value LCG bulk vs `sort.Ints`
  reference, Init on sorted/reverse inputs, Remove(0)==Pop equivalence,
  interleaved push/pop multiset preservation.
