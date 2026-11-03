# wave-3 `structs` corpus

Differential corpus for the `structs` package: exact gc 1.27.1 API — the
single marker type `HostLayout`. Since 2026-11-03 (P1-2 lane) `structs`
is the REAL GOROOT package resolved via the disk importer
(`internal/frontend/diskimport.go`); the synthetic package
(`internal/frontend/synth_structs.go`) is retired.

## Scope boundary

The marker has no API surface that reads it back; it is a compile-time
annotation. The corpus pins that a struct embedding `structs.HostLayout`
behaves exactly like the same struct without it (field access, zero
value, embedding/promotion, pointers, slices, maps). The host-layout
*effect* (C-ABI field placement) is a backend layout property and is not
honored by the P3 backend — equally unobservable from Go code (no API
exposes field offsets without unsafe, and `unsafe.Offsetof` on such
structs is out of v1 scope). Retest trigger if the layout effect is ever
needed: backend host-layout support for marked structs.

## Programs

- `00_marker.go` — embedding, field access, zero value, promotion
  through embedding, pointers, slices, maps, non-first marker position.
- `01_alias.go` — defined type aliasing `HostLayout`, embedding the
  alias, slices/maps of alias-marked structs, zero-value marker literal
  (the P1-2 conversion proof; the real `HostLayout` carries an
  unexported field, so `struct{}` does not convert to it, exactly as
  under gc).
