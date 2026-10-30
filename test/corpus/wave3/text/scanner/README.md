# `text/scanner` wave-3 corpus

Differential corpus for the synthetic `text/scanner` package.

## Scope boundary

Deviations (see `synth_textscanner.go` header):
- `unicode/utf8` inlined as `__ts*` helpers.
- Former deviation, retired 2026-10-30 (review R0-3):
  `unicode.IsLetter`/`IsDigit` were ASCII-only. They now come verbatim
  from the synthetic `unicode` package (full gc tables), so non-ASCII
  letters/digits are recognised; see `01_unicode.go`.

## Programs

- `00_basic.go` — Scan Go-like source, TokenText, Position.
- `01_unicode.go` — Non-ASCII idents (`café`, `éclair`, `naïve`, `ß`,
  `αβγ`) and a non-ASCII digit continuation (`a٣`) scan as whole idents.

## Status (2026-10-30, R0-3 lane)

Token classification is fixed: under graalgo each input above now scans
as one ident, matching gc (verified per-input via `graalgo-sync-run`).
But the program as a whole does NOT yet match gc end-to-end: every
scan loop ends with an EOF `Scan()`, and that panics under graalgo with
`panic: runtime error: negative shift amount`. That panic is a
PRE-EXISTING backend bug, not a scanner regression: a pure-ASCII
`scanIdents("abc")` probe panics identically, and a direct probe of
`ws & (1<<uint(rune(-1)))` panics under graalgo while gc yields 0 — the
scanner's whitespace loop (`s.Whitespace&(1<<uint(ch))`, gc scanner.go)
evaluates the shift with `ch == EOF == -1`, and graalgo's shift
semantics mishandle the resulting huge/negative count. Until that
backend shift bug is fixed (separate lane; it is outside the scanner
package), this program is a known-failing differential probe: correct
tokens, failing tail. Do not mark it passing or scopelimited — it is
neither; it is blocked on the shift fix.

## Results

TBD (2026-10-09).
