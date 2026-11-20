# `index/suffixarray` wave-3 corpus

Differential corpus for the index/suffixarray package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Substring search over a fixed byte string.

## Programs

- `00_basic.go` — Lookup for present/absent needles with limits, FindAllIndex via regexp, and the Bytes accessor.

## Retest trigger

Any change to index/suffixarray lookup or indexing.
