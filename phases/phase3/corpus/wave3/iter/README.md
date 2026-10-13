# iter corpus (P3-M9 wave 3, tier-1a)

Differential corpus for the synthetic `iter` package (extended in internal/frontend/synth_reflect.go).

## Coverage
- `01_pull.go`: `iter.Pull` with `slices.Values`
- `02_pull2.go`: `iter.Pull2` with `slices.All`

## Scope limits / adaptations
- `iter.Pull`/`Pull2` use eager collection (materialize the sequence on first call) instead of the stdlib's coroutine-based laziness. **Correct for all finite sequences** (the only kind v1 programs can construct); infinite sequences would hang under gc but OOM under graalgo. This is a documented adapter semantic difference, not a scope limit for the corpus (all corpus sequences are finite).
- **Retest trigger:** if v1 ever supports runtime coroutines, re-verify laziness.
