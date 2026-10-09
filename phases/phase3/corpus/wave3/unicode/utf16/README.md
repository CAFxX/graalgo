# unicode/utf16 corpus (P3-M9 wave 3, tier-1a)

Differential corpus for the synthetic `unicode/utf16` package (phases/phase1/frontend/synth_unicode_utf16.go — verbatim gc 1.27.1).

## Coverage
- `01_encode.go`: `RuneLen`, `Encode`, `Decode`

## Scope limits
- `utf16.EncodeRune` (public API) returns `(r1, r2 rune)` — v1 cannot emit multi-result functions (P3-M5). Internal callers (`Encode`, `AppendRune`) use the single-result `encodeRunePair` helper returning `[2]rune`. User code calling `utf16.EncodeRune` directly will fail with "unknown function". **Retest trigger:** WS D (multi-result support) lands.
