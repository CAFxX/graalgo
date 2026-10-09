# unicode/utf8 corpus (P3-M9 wave 3, tier-1a)

Differential corpus for the synthetic `unicode/utf8` package (phases/phase1/frontend/synth_unicode_utf8.go — verbatim gc 1.27.1, minus the `word[T]` fast-path).

## Coverage
- `01_rune.go`: `RuneLen`, `Valid`, `RuneCount`

## Scope limits / adaptations
- The `word[T string|[]byte]` generic fast-path helper (and its `ptrSize`/`hiBits` consts) was removed. The v1 frontend does not enumerate the generic instantiation (internal error). The fast path is a pure performance optimization; `Valid`/`ValidString` use the slow path, which is correct. **Retest trigger:** v1 generic instantiation enumeration is fixed.
