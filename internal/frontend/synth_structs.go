// P3-M9 (tier-1d): synthetic `structs` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (see synth_ascii85.go for the pattern).
//
// Exact gc 1.27.1 public API (go1.27: the package contains exactly one
// declared type): HostLayout, the marker type used as the type of a field
// named "_" at the beginning of a struct definition to request host (C
// ABI) memory layout.
//
// The marker has no API surface that reads it back — it is purely a
// compile-time annotation. The synthetic equivalent of gc's
// "struct { // Has unexported fields. }" is the empty struct: no program
// can observe the difference (there are no methods, no functions, no
// exported fields).
//
// Scope note: the host-layout *effect* (C-ABI field placement) is a
// backend layout property and is NOT honored by the P3 backend — but it is
// equally unobservable from Go code (no API exposes field offsets without
// unsafe, and unsafe.Offsetof on such structs is out of v1 scope). If a
// future milestone needs the layout effect, that is a backend feature with
// this file's re-test trigger, not a silent gap.
package main

// syntheticStructsSource is the P3-M9 synthetic `structs` package: exact gc
// 1.27.1 API (marker type only).
const syntheticStructsSource = `package structs

// HostLayout marks a struct as using host memory layout. By convention it
// is used as the type of a field named "_", placed at the beginning of the
// struct type definition.
type HostLayout struct{}
`
