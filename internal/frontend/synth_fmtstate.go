// P3-M9 (RC-6): synthetic `internal/graalgo/fmtstate` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (same mechanism as the other synthetic packages).
//
// Provides the fmt.State implementation type used by Fmt.handleMethods:
// the PrinterState struct. Its methods (Write, Width, Precision, Flag)
// are implemented in Java by graalgo.iface.FmtHandleMethods and installed
// into the method table at launcher startup. The Go type exists so the
// descriptor name is stable.
//
// Synthetic-only (not in the user import allow-list).

// Retirement: see ../../docs/arch/synth-retirement-tracker.md#internalgraalgofmtstate for this package's root cause and retires-when trigger (internal/graalgo/fmtstate).
package main

// syntheticFmtstateSource is the P3-M9 synthetic `internal/graalgo/fmtstate` package.
const syntheticFmtstateSource = `package fmtstate

// PrinterState implements fmt.State. It carries no data. Its methods
// (Write, Width, Precision, Flag) are implemented in Java by
// graalgo.iface.FmtHandleMethods and installed into the method table at
// launcher startup (P3-M9, RC-6).
type PrinterState struct{}
`
