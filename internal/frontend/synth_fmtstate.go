// P3-M9 (RC-6): synthetic `internal/graalgo/fmtstate` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (same mechanism as the other synthetic packages).
//
// Provides the fmt.State implementation type (the PrinterState struct).
// Its methods (Write, Width, Precision, Flag) were implemented in Java by
// graalgo.iface.FmtHandleMethods and installed into the method table at
// launcher startup; that Java backing was deleted with bridge B-1
// (real-fmt2, 2026-10-16) and nothing installs those methods anymore —
// functional status of PrinterState is owned by the phase-3 fmt review.
// The Go type exists so the descriptor name is stable.
//
// Synthetic-only (not in the user import allow-list).

package main

// syntheticFmtstateSource is the P3-M9 synthetic `internal/graalgo/fmtstate` package.
const syntheticFmtstateSource = `package fmtstate

// PrinterState implements fmt.State. It carries no data. Its methods
// (Write, Width, Precision, Flag) were implemented in Java by
// graalgo.iface.FmtHandleMethods (deleted with bridge B-1, real-fmt2;
// nothing installs them anymore — see the phase-3 fmt review).
type PrinterState struct{}
`
