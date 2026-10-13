# `container/ring` wave-3 corpus

Differential corpus for the synthetic `container/ring` package
(pure-Go port of the stdlib circular list).

## Scope boundary

The whole package is in scope: `Ring`, `New`, `Next`/`Prev`/`Move`,
`Link`/`Unlink`, `Len`, `Do`. No `.scopelimited` programs.

## Programs

- `00_basic.go` — construction, value fill, Next/Prev/Move wrap-around,
  Len, Do order, single-element ring.
- `01_link_unlink.go` — Link of two rings, Unlink subring removal,
  Unlink(0)/Unlink(n%len==0), Link(nil), self-link segment removal.
- `02_adversarial.go` — New(0)/New(-1), nil-ring Len/Do, zero-Ring
  lazy init, huge moves, Unlink(n>len), unlink-all-but-one.
