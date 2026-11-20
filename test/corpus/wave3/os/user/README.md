# `os/user` wave-3 corpus

Differential corpus for the os/user package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Looking up users and groups.

## Programs

- `00_basic.go` — Current, Lookup by name, LookupId, LookupGroup, and the error paths on unknown names.

## Retest trigger

Any change to os/user lookup behaviour.
