# `io/ioutil` wave-3 corpus

Differential corpus for the io/ioutil package (L11, REVIEW-2026-11-18-05).

## Scope boundary

The file helpers (deprecated but present).

## Programs

- `00_basic.go` — WriteFile/ReadFile round-trip, TempDir/TempFile, ReadAll, ReadDir, NopCloser; paths never printed.

## Retest trigger

Any change to io/ioutil file helpers.
