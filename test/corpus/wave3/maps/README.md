# maps corpus (P3-M9 wave 3, tier-1a)

Differential corpus for the synthetic `maps` package (internal/frontend/synth_maps.go — gc 1.27.1, `clone` replaced with pure-Go `Clone`).

## Coverage
- `01_keys_values.go`: `maps.Keys`, `maps.Values` iterators (sorted for determinism)
- `02_clone_equal.go`: `maps.Clone`, `maps.Equal` (including nil handling)

## Scope limits
None. All tested `maps` functions are single-result and supported.
