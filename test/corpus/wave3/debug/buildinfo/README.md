# `debug/buildinfo` wave-3 corpus

Differential corpus for the debug/buildinfo package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Build info of the running binary (/proc/self/exe): Go version, main
module path, sorted build settings, dependency count; error paths for a
non-Go binary and a missing file.

## Programs

- `00_basic.go` — Build info of the running binary: version, path, settings; error paths.

## Retest trigger

Any change to debug/buildinfo parsing, GoVersion/Path reporting, or
build-settings contents.
