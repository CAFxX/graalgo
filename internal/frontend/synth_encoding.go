// P3-M7 (wave 1): synthetic top-level `encoding` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (self-contained: no imports). The resulting
// *types.Package is what overlayImporter.Import("encoding") returns.
//
// The package is nearly empty in gc 1.27.1 (encoding.go): exactly the
// six marshaling interfaces, no code, no vars, no funcs. The synthetic
// mirrors that exactly — there is nothing to implement and nothing to
// fail fast on.
// Retirement: see ../../docs/arch/synth-retirement-tracker.md#encoding for this package's root cause and retires-when trigger (encoding).
package main

// syntheticEncodingSource is the P3-M7 synthetic `encoding` package:
// the six gc 1.27.1 marshaling interfaces, verbatim.
const syntheticEncodingSource = `package encoding

// BinaryMarshaler is the interface implemented by an object that can
// marshal itself into a binary form.
type BinaryMarshaler interface {
	MarshalBinary() (data []byte, err error)
}

// BinaryUnmarshaler is the interface implemented by an object that can
// unmarshal a binary representation of itself.
type BinaryUnmarshaler interface {
	UnmarshalBinary(data []byte) error
}

// BinaryAppender is the interface implemented by an object that can
// append the binary representation of itself.
type BinaryAppender interface {
	AppendBinary(b []byte) ([]byte, error)
}

// TextMarshaler is the interface implemented by an object that can
// marshal itself into a textual form.
type TextMarshaler interface {
	MarshalText() (text []byte, err error)
}

// TextUnmarshaler is the interface implemented by an object that can
// unmarshal a textual representation of itself.
type TextUnmarshaler interface {
	UnmarshalText(text []byte) error
}

// TextAppender is the interface implemented by an object that can
// append the textual representation of itself.
type TextAppender interface {
	AppendText(b []byte) ([]byte, error)
}
`
