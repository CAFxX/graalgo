# P3-M7 encoding/csv + encoding/gob: red-green evidence

Non-tautological evidence that the synthetic packages are correct:
each "green" below was preceded by a "red" (a failing test that caught a
real bug), or the test was designed to fail on a plausible incorrect
implementation.

## csv: grammar port

**Red 1**: `TestCSVReadLoopDifferential` failed on input `"a,b\r"`:
`InputOffset: real=4 synth=3`. The synthetic `csvReadLine` measured
`readSize` AFTER dropping the trailing `\r` (gc measures it BEFORE).
Fixed; test green.

**Red 2**: `TestCSVParseErrorFields` used an input where the first `Read`
succeeded (test bug, not code bug). Fixed the test to use `"\"a\nb\"x,c\n"`
which errors on first read; verifies `StartLine`/`Line`/`Column`/`Unwrap`
parity. Green.

**Coverage**: ~50 inputs (quoted fields, embedded newlines, lazy quotes,
bare quotes, comments, empty lines, multibyte delimiters, invalid
delimiters) × 23 configs (Comma/Comment/FieldsPerRecord/LazyQuotes/
TrimLeadingSpace/ReuseRecord) × 2 (ReadAll + Read loop with FieldPos and
InputOffset after each record). Writer: 16 record shapes × 7 configs,
byte-identity. All green under gc 1.27.1.

## gob: wire format

**Red 3**: `TestGobEncodeByteIdentity` failed: synthetic allocated a NEW
type id for every `[]any` (65, 66, ...) while gc reused 64. Root cause:
`gobAllocID()` did not cache (it was a simple increment). Fixed with
`gobTypeIDCache` keyed by (class, width); multi-value streams now match.

**Red 4**: Nested `[]any{[]byte{9,9}}` failed: `gob: type not registered
for interface: []uint8`. gc's `registerBasics` pre-registers `[]byte`
(with interface name `"[]uint8"`, distinct from top-level `"bytes"`).
Fixed by pre-registering and using the correct name.

**Red 5**: Nested composites (`map[string]any{"a": []any{...}}`) failed:
gc sends the descriptor INLINE in the parent message and the value as a
SEPARATE top-level message (when first used); my code sent the descriptor
as a separate message and the value inline. Fixed with `pending` queue
(parent-before-child order) and `sliceAnyIDForInterface` /
`mapStringAnyIDForInterface`.

**Red 6**: `TestGobRegisterParity` initially expected a panic on duplicate
`RegisterName` with the SAME name+type. Probe revealed gc does NOT panic
in that case (it panics only when giving a PRE-REGISTERED type a custom
name, on the FIRST call). Fixed test to match gc; synthetic matches.

**Coverage**: 60+ values (all int/uint widths, floats incl. subnormals,
strings, bytes, nested composites) byte-identical; cross-decode both
directions; `Decode` into `*any` parity; error strings for overflow,
type mismatch, non-pointer, concrete-into-`*any`; truncated/corrupt input
nil-ness parity. All green under gc 1.27.1.

## Differential test locations

- `phases/phase3/hidden_files/m7-tmp/enc-csv-gob/synthtest/csv/csvdiff_test.go`
- `phases/phase3/hidden_files/m7-tmp/enc-csv-gob/synthtest/gob/gobdiff_test.go`

Run: `cd synthtest && go test ./...` (uses `TMPDIR`/`GOCACHE` discipline).
