// P3-M9 (RC-6): synthetic `internal/graalgo/fmtstate` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (same mechanism as the other synthetic packages).
//
// Provides the fmt.State implementation type (PrinterState struct).
// NOTE (real-fmt2, 2026-10-16): the Java backing that used to implement
// PrinterState's methods (Write, Width, Precision, Flag) —
// graalgo.iface.FmtHandleMethods, installed into the method table at
// launcher startup — was deleted with the fmt bridge retirement. The Go
// type is retained so the descriptor name is stable; whether PrinterState
// methods are still routed anywhere is flagged for the phase-3 fmt review.
//
// Synthetic-only (not in the user import allow-list).

package main

// syntheticFmtstateSource is the P3-M9 synthetic `internal/graalgo/fmtstate` package.
const syntheticFmtstateSource = `package fmtstate

// PrinterState implements fmt.State. It carries no data.
// NOTE (real-fmt2, 2026-10-16): the Java backing that used to implement
// these methods (graalgo.iface.FmtHandleMethods) was deleted with the fmt
// bridge retirement; the Go type is retained so the descriptor name is
// stable.
type PrinterState struct{}
`
