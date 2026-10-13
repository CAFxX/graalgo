# `regexp/syntax` wave-3 corpus

Differential corpus for the synthetic `regexp/syntax` package.

## Scope boundary

SCOPE-LIMITED: Requires `unicode` tables (Scripts, Categories,
SimpleFold) from tier1a's `unicode` package, which is in progress.
The synthetic source is generated and registered, but will not
type-check until tier1a lands.

Retest trigger: `unicode` + `unicode/utf8` land in the frontend overlay
(tier1a).

## Programs

- `00_parse.go` — Parse, Simplify (`.scopelimited`).

## Results

0 passed, 1 scope-limited, 0 failed (2026-10-09).
