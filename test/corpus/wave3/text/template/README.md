# `text/template` wave-3 corpus

Differential corpus for the text/template (+text/template/parse) package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Parse + Execute to a Builder with funcs, if/range/with; text/template/parse tree walk with node-kind counts; missingkey=error. No .scopelimited programs. Execution against untrusted templates (sandboxing) is out of scope; only rendering + tree inspection are pinned.

## Programs

- `00_basic.go` — Parse+Execute with funcs/if/range/with, parse tree walk, missingkey=error.

## Retest trigger

A template feature (e.g. a new action) is added to the corpus surface.
