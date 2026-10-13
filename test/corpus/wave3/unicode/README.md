# unicode corpus (P3-M9 wave 3, tier-1a)

Differential corpus for the synthetic `unicode` package (internal/frontend/synth_unicode.go — gc 1.27.1 tables + letter/graphic/digit/case).

## Coverage
- `01_isletter.go`: `IsLetter`, `IsDigit`, `IsSpace`
- `02_case.go`: `ToUpper`, `ToLower`, `ToTitle`

## Scope limits
None for the tested functions. Internal helpers refactored for v1:
- `&ranges[i]` → value copy (v1: no `&` on index expr)
- `lookupCaseRange` returns index (not `*CaseRange`); `to` returns `rune` (not `(rune, bool)`)
