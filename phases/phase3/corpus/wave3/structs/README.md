# wave-3 `structs` corpus

Differential corpus for the P3-M9 synthetic `structs` package
(`internal/frontend/synth_structs.go`): exact gc 1.27.1 API — the
single marker type `HostLayout`.

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
