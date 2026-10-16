// P3-M9 (RC-6): synthetic `internal/graalgo/fmtstate` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (same mechanism as the other synthetic packages).
//
// Provides the fmt.State implementation type used when boxing a
// PrinterState as fmt.State for fmt.Formatter dispatch: the PrinterState
// struct. (Historical: its methods were implemented in Java by
// graalgo.iface.FmtHandleMethods, removed with the real-fmt retirement in
// a527063d; the Go type remains so the descriptor name is stable.)
//
// Synthetic-only (not in the user import allow-list).

package main

// syntheticFmtstateSource is the P3-M9 synthetic `internal/graalgo/fmtstate` package.
const syntheticFmtstateSource = `package fmtstate

// PrinterState implements fmt.State. It carries no data. Its methods
// (Write, Width, Precision, Flag) are implemented in Java by
// graalgo.iface.FmtHandleMethods and installed into the method table at
// launcher startup (P3-M9, RC-6).
type PrinterState struct{}
`
