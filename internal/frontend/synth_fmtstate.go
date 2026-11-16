// P3-M9 (RC-6): synthetic `internal/graalgo/fmtstate` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (same mechanism as the other synthetic packages).
//
// Provides only the PrinterState struct: an empty type carrying no data
// and declaring no methods. It does NOT implement fmt.State.
//
// History: in P3-M9 (RC-6) PrinterState was the fmt.State implementation
// type used by Fmt.handleMethods, with its methods (Write, Width,
// Precision, Flag) implemented in Java and installed into the method
// table at launcher startup. That Java implementation was deleted in
// real-fmt2 (a527063d / 86071cbb): real fmt now compiles from GOROOT and
// supplies its own fmt.State. No live code calls PrinterState methods or
// boxes a PrinterState as fmt.State; the package is retained as-is, its
// original purpose gone.
//
// Synthetic-only (not in the user import allow-list).

package main

// syntheticFmtstateSource is the P3-M9 synthetic `internal/graalgo/fmtstate` package.
const syntheticFmtstateSource = `package fmtstate

// PrinterState is an empty struct carrying no data and declaring no
// methods; it does not implement fmt.State. It is retained from P3-M9
// (RC-6), where it was the fmt.State implementation type whose methods
// were implemented in Java. That Java implementation was deleted in
// real-fmt2 (a527063d); fmt.State is now supplied by real fmt from GOROOT.
type PrinterState struct{}
`
