# `regexp` wave-3 corpus

Differential corpus for the regexp (+regexp/syntax) package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Compile/MatchString/FindAllString/ReplaceAll/Split, submatches and named groups; syntax.Parse + Simplify + Compile. No .scopelimited programs. regexp/syntax subtree coverage (already has 00_parse.go) is unaffected; this program covers the regexp package itself, which had zero corpus imports.

## Programs

- `00_basic.go` — Compile/MatchString/FindAllString/ReplaceAll/Split, submatches, named groups, syntax.Parse+Simplify.

## Retest trigger

regexp/syntax gains a dedicated corpus program of its own.
