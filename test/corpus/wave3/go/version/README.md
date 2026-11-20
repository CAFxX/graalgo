# `go/version` wave-3 corpus

Differential corpus for the go/version package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Version-string validity, comparison, and language-version extraction.

## Programs

- `00_basic.go` — IsValid, Compare, and Lang on valid, invalid, and pre-release versions; comparison pairs.

## Retest trigger

Any change to go/version validity rules, comparison ordering, or Lang extraction.
