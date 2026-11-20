# `runtime/debug` wave-3 corpus

Differential corpus for the runtime/debug package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Runtime tuning knobs and build info.

## Programs

- `00_basic.go` — SetGCPercent/SetMaxStack/SetMaxThreads/SetMemoryLimit round-trips, FreeOSMemory, ReadBuildInfo, SetTraceback.

## Retest trigger

Any change to runtime/debug tuning or build-info reporting.
