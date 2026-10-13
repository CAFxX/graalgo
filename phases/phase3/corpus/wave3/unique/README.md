# wave-3 `unique` corpus

Differential corpus for the P3-M9 synthetic `unique` package
(`internal/frontend/synth_unique.go`): exact gc 1.27.1 API
(`Handle[T comparable]`, `Make[T comparable](value T) Handle[T]`,
`(Handle[T]).Value() T`), real-Go canonicalization over a
mutex-guarded map.

## Scope boundary

In scope: canonicalization, handle equality, `Value`, use as map keys,
concurrent `Make`, pointer-identity (not pointee-equality) for pointer
values, `any`-typed values.

Out of scope (deliberate, documented in `synth_unique.go`):
- Weak-reference reclamation of interned values (deviation D1): the table
  never shrinks. Unobservable to any program (only to memory usage).
- The panic message for uncomparable dynamic values (e.g.
  `unique.Make(any([]int{1}))`): gc panics in the runtime map, ours panics
  in the Go map insert — same observable outcome (a panic), but the
  message text is backend-specific and is not pinned.
- Retest trigger: none needed — the adapter is complete for the v1 scope.

## Programs

- `00_canonical.go` — Make/Value/equality across int, string, struct,
  array, bool, uint64; handles as map keys.
- `01_concurrent.go` — 8 goroutines x 200 Makes over a 50-value domain;
  prints fixed count (3200) + all-ok bool (deterministic).
- `02_adversarial.go` — pointer identity vs pointee equality, `any`
  dynamics, zero values, 5000 distinct values, unexported struct fields,
  nested arrays/structs.
