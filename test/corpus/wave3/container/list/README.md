# `container/list` wave-3 corpus

Differential corpus for the synthetic `container/list` package
(pure-Go port of the stdlib doubly linked list).

## Scope boundary

The whole package is in scope: `Element`, `List`, `New`, and all
methods (`Front`/`Back`/`Len`/`Init`/`Push*`/`Insert*`/`Remove`/
`Move*`/`Push*List`). No `.scopelimited` programs.

## Programs

- `00_basic.go` — zero-value lazyInit, push/remove/iteration both
  directions, detachment of removed elements, Init clearing.
- `01_move_insert.go` — MoveToFront/Back/Before/After sequences,
  InsertBefore/After, foreign-mark no-ops (nil return), PushBackList/
  PushFrontList copies incl. self-copy.
- `02_adversarial.go` — remove-from-foreign-list no-op, 500-element
  stress with strided removals, boundary inserts, adjacent moves,
  element value reuse across lists.
