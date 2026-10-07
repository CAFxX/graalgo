# P3-M7 wave 1 corpus: encoding/csv + encoding/gob

Differential tests for the synthetic `encoding/csv` and `encoding/gob`
packages (P3-M7, `phases/phase1/frontend/synth_enc_csv_gob.go`).

## Approach

The synthetic packages are **pure Go** (no `io`, `reflect`, or `unicode`
imports). They are validated by compiling the extracted synthetic sources
under stock gc 1.27.1 and differential-testing against the real
`encoding/csv` and `encoding/gob`:

- **csv**: `synthtest/csv/csvdiff_test.go` — ~50 inputs × 23 configs × 2
  (ReadAll + Read loop with FieldPos/InputOffset) + ParseError field
  checks + Writer byte-identity. All green.
- **gob**: `synthtest/gob/gobdiff_test.go` — byte-exact wire format for
  60+ values, multi-value streams, cross-decode (synthetic↔real),
  Decode-into-`*any` parity, error-path parity, Register semantics,
  scope fail-fast. All green.

The test sources live in
`phases/phase3/hidden_files/m7-tmp/enc-csv-gob/synthtest/` (not committed;
see `redgreen.md` for the evidence).

## Running under graalgo

Once the phase-3 backend supports the synthetic csv/gob exports, programs
in this directory can be run via the standard differential runner
(`run_differential.sh`, modeled on `corpus/timectx/run_differential.sh`):
each program runs under gc and under graalgo, comparing stdout bytes.

## M7 extensions (not in gc 1.27.1)

The synthetic packages provide in-memory constructors because `io` is out
of M7 scope (P3-M8 re-test trigger):

- csv: `NewReaderString(s string) *Reader`, `NewBufferWriter() *Writer`
  (+ `Bytes() []byte`)
- gob: `NewBufferEncoder() *Encoder` (+ `Bytes() []byte`),
  `NewBufferDecoder(data []byte) *Decoder`

Corpus programs use these; the gc leg uses shims (e.g.
`csv.NewReader(strings.NewReader(s))`).
