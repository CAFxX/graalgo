# `runtime/metrics` wave-3 corpus

Differential corpus for the runtime/metrics package (L11, REVIEW-2026-11-18-05).

## Scope boundary

The metrics registry.

## Programs

- `00_basic.go` — Every descriptor's name, kind, and cumulative flag (metadata only — values are live); one sample's kind pinned.

## Retest trigger

Any change to runtime/metrics descriptors.
