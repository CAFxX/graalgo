# `plugin` wave-3 corpus

Differential corpus for the plugin package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Building and loading a real plugin.

## Programs

- `00_basic.go` — go build -buildmode=plugin in a temp dir, Open, Lookup of a var and a func, missing-symbol/file error paths. Ships with 00_basic.scopelimited.

## Retest trigger

Frontend/runtime gains plugin support (see 00_basic.scopelimited).
