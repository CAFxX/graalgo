# `testing/fstest` wave-3 corpus

Differential corpus for the testing/fstest package (L11, REVIEW-2026-11-18-05).

## Scope boundary

An in-memory file system.

## Programs

- `00_basic.go` — MapFS with files and a directory, read via the io/fs API (ReadFile, Glob, WalkDir, Stat, ReadDir, Sub); error paths.

## Retest trigger

Any change to testing/fstest MapFS behaviour.
