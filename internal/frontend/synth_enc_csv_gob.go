// P3-M7 (wave 1): synthetic `encoding/csv` and `encoding/gob` packages.
//
// REAL Go sources, embedded as text and type-checked on demand with the
// overlay importer (csv/gob import only the synthetic `errors` package —
// no io, no reflect, no unicode, no strings/bytes/strconv). The resulting
// *types.Package instances are what overlayImporter.Import returns, so
// the user's type-check and the synthetic sources' own type-check share
// identical *types.Func/*types.TypeName objects; the exporter then emits
// the synthetic declarations' bodies into the same wire document (shared
// type/object tables) via exportSyntheticPackages.
//
// Value model:
//   - csv: the FULL RFC-4180 grammar (Reader with every gc 1.27.1 field,
//     Read/ReadAll/FieldPos/InputOffset, ParseError with exact gc error
//     strings, Writer with quoting rules) operating on an in-memory
//     string/[]byte. io.Reader/io.Writer constructors cannot be declared
//     (io is out of M7 scope): NewReader/NewWriter are ABSENT —
//     referencing them is a type-check error (fail fast, never silent) —
//     and the M7 extensions NewReaderString/NewBufferWriter/Bytes provide
//     the in-memory entry points. The multi-result methods
//     (Read/ReadAll/FieldPos) keep exact gc 1.27.1 signatures with
//     fail-fast bodies; their 2-target call sites desugar (Appendix M
//     pattern, mrdesugar_m5.go) to the single-result __csvRead/
//     __csvReadAll/__csvFieldPos helpers. Every other position fails fast.
//   - gob: Encoder/Decoder over the closed dynamic `any` model (nil, bool,
//     all int/uint widths, float32/float64, string, []byte, []any,
//     map[string]any). The wire format is byte-identical to gc 1.27.1 for
//     the supported subset (builtin ids 1-8, user ids from 64 allocated in
//     first-use order, two-state uint encoding, zigzag ints, byte-reversed
//     float bits via exact soft-float). Struct/array/complex/interface-
//     nesting beyond the model fail fast ("not in M7 scope (needs
//     reflect, P3-M8)"). io-based constructors are absent (fail fast);
//     NewBufferEncoder/Bytes/NewBufferDecoder are the M7 extensions.
//     gob.Register/RegisterName are real (needed for nested []any /
//     map[string]any inside interfaces, exactly like gc).
//
// Out-of-scope members are either absent (unnameable without io/reflect)
// or declared with exact gc signatures and panic bodies reporting "not in
// M7 scope" — fail fast, NEVER silent.
//
// The sources use only frontend/v1-supported constructs (no generics, no
// multi-result calls — every internal helper is single-result; the parse
// loops are restructured around outcome structs).
package main

// syntheticCsvSource is the P3-M7 synthetic `encoding/csv` package: the
// complete gc 1.27.1 CSV grammar (reader.go/writer.go) ported to
// single-result style with no imports beyond the synthetic `errors`
// package. Exact gc public API signatures for the in-scope surface;
// M7 extensions (NewReaderString, NewBufferWriter, Bytes) are marked.
const syntheticCsvSource = `package csv

import (
	"errors"
	"io"
)

// A ParseError is returned for parsing errors.
// Line and column numbers are 1-indexed.
type ParseError struct {
	StartLine int   // Line where the record starts
	Line      int   // Line where the error occurred
	Column    int   // Column (1-based byte index) where the error occurred
	Err       error // The actual error
}

func (e *ParseError) Error() string {
	if e.Err == ErrFieldCount {
		return "record on line " + csvItoa(e.Line) + ": " + e.Err.Error()
	}
	if e.StartLine != e.Line {
		return "record on line " + csvItoa(e.StartLine) + "; parse error on line " + csvItoa(e.Line) + ", column " + csvItoa(e.Column) + ": " + e.Err.Error()
	}
	return "parse error on line " + csvItoa(e.Line) + ", column " + csvItoa(e.Column) + ": " + e.Err.Error()
}

func (e *ParseError) Unwrap() error { return e.Err }

// These are the errors that can be returned in ParseError.Err.
var (
	ErrBareQuote  = errors.New("bare \" in non-quoted-field")
	ErrQuote      = errors.New("extraneous or missing \" in quoted-field")
	ErrFieldCount = errors.New("wrong number of fields")

	// Deprecated: ErrTrailingComma is no longer used.
	ErrTrailingComma = errors.New("extra delimiter at end of line")
)

var csvErrInvalidDelim = errors.New("csv: invalid field or comment delimiter")

// csvEOF is the M7 in-memory counterpart of io.EOF (io is out of scope):
// returned by Read when no data is left. Its message matches io.EOF.
var csvEOF = errors.New("EOF")

func csvValidRune(r rune) bool {
	// utf8.ValidRune without the unicode/utf8 import.
	if r < 0 {
		return false
	}
	if r < 0xD800 || r > 0xDFFF {
		return r <= 0x10FFFF
	}
	return false
}

func csvValidDelim(r rune) bool {
	return r != 0 && r != '"' && r != '\r' && r != '\n' && csvValidRune(r) && r != 0xFFFD
}

// csvItoa formats an int in decimal (no strconv in M7 scope).
func csvItoa(v int) string {
	neg := v < 0
	var u uint64
	if neg {
		u = uint64(-(v + 1)) + 1
	} else {
		u = uint64(v)
	}
	var arr [24]byte
	w := len(arr)
	if u == 0 {
		w--
		arr[w] = '0'
	} else {
		for u > 0 {
			w--
			arr[w] = byte(u%10) + '0'
			u /= 10
		}
	}
	if neg {
		w--
		arr[w] = '-'
	}
	return string(arr[w:])
}

// csvDecodeRune decodes the first rune of b (utf8.DecodeRune without the
// unicode/utf8 import): returns (rune, size).
// P4 (driver-harden): returns a struct (not 2 values) — the synthetic
// exporter cannot desugar package-internal multi-result calls (M7 table
// covers only public API); struct return avoids 2-target positions.
type csvRuneResult struct {
	r    rune
	size int
}

func csvDecodeRune(b []byte) csvRuneResult {
	if len(b) == 0 {
		return csvRuneResult{0xFFFD, 0}
	}
	c0 := b[0]
	if c0 < 0x80 {
		return csvRuneResult{rune(c0), 1}
	}
	// Multi-byte sequences; accept only the shortest valid form.
	if c0 < 0xC2 || c0 > 0xF4 || len(b) < 2 {
		return csvRuneResult{0xFFFD, 1}
	}
	c1 := b[1]
	if c1 < 0x80 || c1 > 0xBF {
		return csvRuneResult{0xFFFD, 1}
	}
	if c0 < 0xE0 {
		return csvRuneResult{rune(c0&0x1F)<<6 | rune(c1&0x3F), 2}
	}
	if len(b) < 3 {
		return csvRuneResult{0xFFFD, 1}
	}
	c2 := b[2]
	if c2 < 0x80 || c2 > 0xBF {
		return csvRuneResult{0xFFFD, 1}
	}
	if c0 < 0xF0 {
		r := rune(c0&0x0F)<<12 | rune(c1&0x3F)<<6 | rune(c2&0x3F)
		if r < 0x800 || (r >= 0xD800 && r <= 0xDFFF) {
			return csvRuneResult{0xFFFD, 1}
		}
		return csvRuneResult{r, 3}
	}
	if len(b) < 4 {
		return csvRuneResult{0xFFFD, 1}
	}
	c3 := b[3]
	if c3 < 0x80 || c3 > 0xBF {
		return csvRuneResult{0xFFFD, 1}
	}
	r := rune(c0&0x07)<<18 | rune(c1&0x3F)<<12 | rune(c2&0x3F)<<6 | rune(c3&0x3F)
	if r < 0x10000 || r > 0x10FFFF {
		return csvRuneResult{0xFFFD, 1}
	}
	return csvRuneResult{r, 4}
}

// csvDecodeRuneString is csvDecodeRune for strings (writer path).
// P4 (driver-harden): returns csvRuneResult (see csvDecodeRune).
func csvDecodeRuneString(s string) csvRuneResult {
	if len(s) == 0 {
		return csvRuneResult{0xFFFD, 0}
	}
	c0 := s[0]
	if c0 < 0x80 {
		return csvRuneResult{rune(c0), 1}
	}
	var tmp [4]byte
	n := len(s)
	if n > 4 {
		n = 4
	}
	for i := 0; i < n; i++ {
		tmp[i] = s[i]
	}
	return csvDecodeRune(tmp[:n])
}

// csvNextRune returns the next rune in b or 0xFFFD.
func csvNextRune(b []byte) rune {
	return csvDecodeRune(b).r
}

// csvRuneLen is utf8.RuneLen for validated runes.
func csvRuneLen(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r < 0x800:
		return 2
	case r < 0x10000:
		return 3
	}
	return 4
}

// csvLengthNL reports the number of bytes for the trailing \n.
func csvLengthNL(b []byte) int {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		return 1
	}
	return 0
}

// csvIndexByte is bytes.IndexByte without the bytes import.
func csvIndexByte(b []byte, c byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// csvIndexRune is bytes.IndexRune without the bytes import.
func csvIndexRune(b []byte, r rune) int {
	size := csvRuneLen(r)
	var enc [4]byte
	switch size {
	case 1:
		enc[0] = byte(r)
	case 2:
		enc[0] = byte(0xC0 | (r >> 6))
		enc[1] = byte(0x80 | (r & 0x3F))
	case 3:
		enc[0] = byte(0xE0 | (r >> 12))
		enc[1] = byte(0x80 | ((r >> 6) & 0x3F))
		enc[2] = byte(0x80 | (r & 0x3F))
	case 4:
		enc[0] = byte(0xF0 | (r >> 18))
		enc[1] = byte(0x80 | ((r >> 12) & 0x3F))
		enc[2] = byte(0x80 | ((r >> 6) & 0x3F))
		enc[3] = byte(0x80 | (r & 0x3F))
	}
	needle := enc[:size]
	for i := 0; i+size <= len(b); i++ {
		match := true
		for j := 0; j < size; j++ {
			if b[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// csvIsSpace is unicode.IsSpace (White_Space property) without the
// unicode import. The table is stable Unicode data, verified against
// go1.27.1's unicode.IsSpace.
func csvIsSpace(r rune) bool {
	switch {
	case r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r' || r == ' ':
		return true
	case r == 0x85 || r == 0xA0 || r == 0x1680:
		return true
	case r >= 0x2000 && r <= 0x200A:
		return true
	case r == 0x2028 || r == 0x2029 || r == 0x202F || r == 0x205F || r == 0x3000:
		return true
	}
	return false
}

// csvStringIndexAny is strings.IndexAny without the strings import.
func csvStringIndexAny(s, chars string) int {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return i
			}
		}
	}
	return -1
}

// csvStringContainsRune is strings.ContainsRune without the strings import.
func csvStringContainsRune(s string, r rune) bool {
	size := csvRuneLen(r)
	var enc [4]byte
	switch size {
	case 1:
		enc[0] = byte(r)
	case 2:
		enc[0] = byte(0xC0 | (r >> 6))
		enc[1] = byte(0x80 | (r & 0x3F))
	case 3:
		enc[0] = byte(0xE0 | (r >> 12))
		enc[1] = byte(0x80 | ((r >> 6) & 0x3F))
		enc[2] = byte(0x80 | (r & 0x3F))
	case 4:
		enc[0] = byte(0xF0 | (r >> 18))
		enc[1] = byte(0x80 | ((r >> 12) & 0x3F))
		enc[2] = byte(0x80 | ((r >> 6) & 0x3F))
		enc[3] = byte(0x80 | (r & 0x3F))
	}
	needle := string(enc[:size])
	for i := 0; i+size <= len(s); i++ {
		if s[i:i+size] == needle {
			return true
		}
	}
	return false
}

// csvStringContainsAny is strings.ContainsAny without the strings import.
func csvStringContainsAny(s, chars string) bool {
	return csvStringIndexAny(s, chars) >= 0
}

// A Reader reads records from CSV-encoded data.
//
// As returned by NewReader, a Reader expects input conforming to
// RFC 4180. The exported fields can be changed to customize the details
// before the first call to Read or ReadAll.
//
// M7 divergence: gc's NewReader reads lazily from the io.Reader; the
// synthetic drains the reader eagerly at construction (the parse core is
// in-memory) and replays any read error from the first Read/ReadAll, so
// the failure is never silent and never misattributed. NewReaderString
// (M7 extension) remains the allocation-free in-memory constructor.
type Reader struct {
	// Comma is the field delimiter.
	// It is set to comma (',') by NewReader and NewReaderString.
	// Comma must be a valid rune and must not be \r, \n,
	// or the Unicode replacement character (0xFFFD).
	Comma rune

	// Comment, if not 0, is the comment character. Lines beginning with the
	// Comment character without preceding whitespace are ignored.
	// With leading whitespace the Comment character becomes part of the
	// field, even if TrimLeadingSpace is true.
	// Comment must be a valid rune and must not be \r, \n,
	// or the Unicode replacement character (0xFFFD).
	// It must also not be equal to Comma.
	Comment rune

	// FieldsPerRecord is the number of expected fields per record.
	// If FieldsPerRecord is positive, Read requires each record to
	// have the given number of fields. If FieldsPerRecord is 0, Read sets it to
	// the number of fields in the first record, so that future records must
	// have the same field count. If FieldsPerRecord is negative, no check is
	// made and records may have a variable number of fields.
	FieldsPerRecord int

	// If LazyQuotes is true, a quote may appear in an unquoted field and a
	// non-doubled quote may appear in a quoted field.
	LazyQuotes bool

	// If TrimLeadingSpace is true, leading white space in a field is ignored.
	// This is done even if the field delimiter, Comma, is white space.
	TrimLeadingSpace bool

	// ReuseRecord controls whether calls to Read may return a slice sharing
	// the backing array of the previous call's returned slice for performance.
	// By default, each call to Read returns newly allocated memory owned by the caller.
	ReuseRecord bool

	// Deprecated: TrailingComma is no longer used.
	TrailingComma bool

	// data/pos are the M7 in-memory input (replacing gc's bufio.Reader).
	data []byte
	pos  int

	// numLine is the current line being read in the CSV data.
	numLine int

	// offset is the input byte offset of the current reader position.
	offset int64

	// recordBuffer holds the unescaped fields, one after another.
	// The fields can be accessed by using the indexes in fieldIndexes.
	recordBuffer []byte

	// fieldIndexes is an index of fields inside recordBuffer.
	// The i'th field ends at offset fieldIndexes[i] in recordBuffer.
	fieldIndexes []int

	// fieldPositions is an index of field positions for the
	// last record returned by Read.
	fieldPositions []csvPosition

	// lastRecord is a record cache and only used when ReuseRecord == true.
	lastRecord []string

	// readErr is the sticky error from NewReader's eager drain of the
	// io.Reader. It is replayed by the first Read/ReadAll (and every
	// subsequent one until data is consumed, which never happens once
	// the drain failed).
	readErr error
}

// NewReader returns a new Reader that reads from r.
//
// The reader is drained eagerly (see the Reader doc); a drain failure is
// replayed as the Read/ReadAll error rather than panicking at
// construction, matching gc's observable behavior (gc surfaces the read
// error on first Read, and NewReader itself never fails).
func NewReader(r io.Reader) *Reader {
	data, readErr := io.ReadAll(r)
	return &Reader{
		Comma:   ',',
		data:    data,
		readErr: readErr,
	}
}

// NewReaderString returns a new Reader that reads from s.
//
// M7 EXTENSION (not in gc 1.27.1): the allocation-free in-memory
// constructor. NewReader (exact gc signature) is also available.
func NewReaderString(s string) *Reader {
	return &Reader{
		Comma: ',',
		data:  []byte(s),
	}
}

// csvLineResult is the single-result carrier for csvReadLine (v1 cannot
// call multi-result functions, WS D).
type csvLineResult struct {
	line []byte
	err  error
}

// csvReadLine reads the next line (with the trailing endline).
// If the end of data is hit without a trailing endline, it will be omitted.
// If some bytes were read, then the error is never csvEOF.
// The result is only valid until the next call to csvReadLine.
func (r *Reader) csvReadLine() csvLineResult {
	if r.pos >= len(r.data) {
		r.numLine++
		return csvLineResult{err: csvEOF}
	}
	rest := r.data[r.pos:]
	i := csvIndexByte(rest, '\n')
	var line []byte
	var err error
	var readSize int
	if i < 0 {
		// No trailing newline: consume to end of data (not an EOF error
		// when bytes were read, matching gc's readLine).
		line = rest
		r.pos = len(r.data)
		readSize = len(line)
		// For backwards compatibility, drop trailing \r before EOF.
		// (readSize is measured before the drop, like gc.)
		if readSize > 0 && line[readSize-1] == '\r' {
			line = line[:readSize-1]
		}
	} else {
		line = rest[:i+1]
		r.pos += i + 1
		readSize = len(line)
	}
	r.numLine++
	r.offset += int64(readSize)
	// Normalize \r\n to \n on all input lines.
	if n := len(line); n >= 2 && line[n-2] == '\r' && line[n-1] == '\n' {
		line[n-2] = '\n'
		line = line[:n-1]
	}
	return csvLineResult{line: line, err: err}
}

// csvPosition holds the position of a field in the current line.
type csvPosition struct {
	line, col int
}

// csvReadOutcome is the single-result carrier for csvReadRecord (v1
// cannot call multi-result functions, WS D).
type csvReadOutcome struct {
	rec []string
	err error
}

func (r *Reader) csvReadRecord(dst []string) csvReadOutcome {
	if r.Comma == r.Comment || !csvValidDelim(r.Comma) || (r.Comment != 0 && !csvValidDelim(r.Comment)) {
		return csvReadOutcome{err: csvErrInvalidDelim}
	}

	// Read line (automatically skipping past empty lines and any comments).
	var line []byte
	var errRead error
	for errRead == nil {
		lr := r.csvReadLine()
		line, errRead = lr.line, lr.err
		if r.Comment != 0 && csvNextRune(line) == r.Comment {
			line = nil
			continue // Skip comment lines
		}
		if errRead == nil && len(line) == csvLengthNL(line) {
			line = nil
			continue // Skip empty lines
		}
		break
	}
	if errRead == csvEOF {
		return csvReadOutcome{err: errRead}
	}

	// Parse each field in the record.
	var err error
	const quoteLen = len("\"")
	commaLen := csvRuneLen(r.Comma)
	recLine := r.numLine // Starting line for record
	r.recordBuffer = r.recordBuffer[:0]
	r.fieldIndexes = r.fieldIndexes[:0]
	r.fieldPositions = r.fieldPositions[:0]
	pos := csvPosition{line: r.numLine, col: 1}
parseField:
	for {
		if r.TrimLeadingSpace {
			i := 0
			for i < len(line) {
				rr := csvDecodeRune(line[i:])
				c, size := rr.r, rr.size
				if !csvIsSpace(c) {
					break
				}
				i += size
			}
			if i >= len(line) {
				pos.col -= csvLengthNL(line)
			}
			line = line[i:]
			pos.col += i
		}
		if len(line) == 0 || line[0] != '"' {
			// Non-quoted string field
			i := csvIndexRune(line, r.Comma)
			field := line
			if i >= 0 {
				field = field[:i]
			} else {
				field = field[:len(field)-csvLengthNL(field)]
			}
			// Check to make sure a quote does not appear in field.
			if !r.LazyQuotes {
				if j := csvIndexByte(field, '"'); j >= 0 {
					col := pos.col + j
					err = &ParseError{StartLine: recLine, Line: r.numLine, Column: col, Err: ErrBareQuote}
					break parseField
				}
			}
			r.recordBuffer = append(r.recordBuffer, field...)
			r.fieldIndexes = append(r.fieldIndexes, len(r.recordBuffer))
			r.fieldPositions = append(r.fieldPositions, pos)
			if i >= 0 {
				line = line[i+commaLen:]
				pos.col += i + commaLen
				continue parseField
			}
			break parseField
		} else {
			// Quoted string field
			fieldPos := pos
			line = line[quoteLen:]
			pos.col += quoteLen
			for {
				i := csvIndexByte(line, '"')
				if i >= 0 {
					// Hit next quote.
					r.recordBuffer = append(r.recordBuffer, line[:i]...)
					line = line[i+quoteLen:]
					pos.col += i + quoteLen
					switch rn := csvNextRune(line); {
					case rn == '"':
						// '""' sequence (append quote).
						r.recordBuffer = append(r.recordBuffer, '"')
						line = line[quoteLen:]
						pos.col += quoteLen
					case rn == r.Comma:
						// '",' sequence (end of field).
						line = line[commaLen:]
						pos.col += commaLen
						r.fieldIndexes = append(r.fieldIndexes, len(r.recordBuffer))
						r.fieldPositions = append(r.fieldPositions, fieldPos)
						continue parseField
					case csvLengthNL(line) == len(line):
						// '"\n' sequence (end of line).
						r.fieldIndexes = append(r.fieldIndexes, len(r.recordBuffer))
						r.fieldPositions = append(r.fieldPositions, fieldPos)
						break parseField
					case r.LazyQuotes:
						// '"' sequence (bare quote).
						r.recordBuffer = append(r.recordBuffer, '"')
					default:
						// '"*' sequence (invalid non-escaped quote).
						err = &ParseError{StartLine: recLine, Line: r.numLine, Column: pos.col - quoteLen, Err: ErrQuote}
						break parseField
					}
				} else if len(line) > 0 {
					// Hit end of line (copy all data so far).
					r.recordBuffer = append(r.recordBuffer, line...)
					if errRead != nil {
						break parseField
					}
					pos.col += len(line)
					lr := r.csvReadLine()
					line, errRead = lr.line, lr.err
					if len(line) > 0 {
						pos.line++
						pos.col = 1
					}
					if errRead == csvEOF {
						errRead = nil
					}
				} else {
					// Abrupt end of data (EOF or error).
					if !r.LazyQuotes && errRead == nil {
						err = &ParseError{StartLine: recLine, Line: pos.line, Column: pos.col, Err: ErrQuote}
						break parseField
					}
					r.fieldIndexes = append(r.fieldIndexes, len(r.recordBuffer))
					r.fieldPositions = append(r.fieldPositions, fieldPos)
					break parseField
				}
			}
		}
	}
	if err == nil {
		err = errRead
	}

	// Create a single string and create slices out of it.
	// This pins the memory of the fields together, but allocates once.
	str := string(r.recordBuffer) // Convert to string once to batch allocations
	dst = dst[:0]
	if cap(dst) < len(r.fieldIndexes) {
		dst = make([]string, len(r.fieldIndexes))
	}
	dst = dst[:len(r.fieldIndexes)]
	var preIdx int
	for i, idx := range r.fieldIndexes {
		dst[i] = str[preIdx:idx]
		preIdx = idx
	}

	// Check or update the expected fields per record.
	if r.FieldsPerRecord > 0 {
		if len(dst) != r.FieldsPerRecord && err == nil {
			err = &ParseError{
				StartLine: recLine,
				Line:      recLine,
				Column:    1,
				Err:       ErrFieldCount,
			}
		}
	} else if r.FieldsPerRecord == 0 {
		r.FieldsPerRecord = len(dst)
	}
	return csvReadOutcome{rec: dst, err: err}
}

// csvReadPair is the Appendix M single-result carrier for Read.
type csvReadPair struct {
	rec []string
	err error
}

// __csvRead is the Appendix M desugar target behind Reader.Read.
func __csvRead(r *Reader) csvReadPair {
	if r.readErr != nil {
		return csvReadPair{err: r.readErr}
	}
	if r.ReuseRecord {
		out := r.csvReadRecord(r.lastRecord)
		r.lastRecord = out.rec
		return csvReadPair{rec: out.rec, err: out.err}
	}
	out := r.csvReadRecord(nil)
	return csvReadPair{rec: out.rec, err: out.err}
}

// Read reads one record (a slice of fields) from r.
// If the record has an unexpected number of fields,
// Read returns the record along with the error ErrFieldCount.
// If the record contains a field that cannot be parsed,
// Read returns a partial record along with the parse error.
// The partial record contains all fields read before the error.
// If there is no data left to be read, Read returns nil, csvEOF.
// If Reader.ReuseRecord is true, the returned slice may be shared
// between multiple calls to Read.
//
// M7: the body is fail-fast; the exporter desugars 2-target call sites
// (rec, err := r.Read()) to __csvRead (Appendix M pattern — v1 cannot
// call multi-result functions, WS D). Every other position fails here.
func (r *Reader) Read() ([]string, error) {
	panic("csv: Reader.Read is not in M7 scope as a direct call (only the rec, err := r.Read() 2-target form desugars; only the 2-target form is desugared)")
}

// csvReadAllPair is the Appendix M single-result carrier for ReadAll.
type csvReadAllPair struct {
	recs [][]string
	err  error
}

// __csvReadAll is the Appendix M desugar target behind Reader.ReadAll.
func __csvReadAll(r *Reader) csvReadAllPair {
	if r.readErr != nil {
		return csvReadAllPair{err: r.readErr}
	}
	var records [][]string
	for {
		out := r.csvReadRecord(nil)
		if out.err == csvEOF {
			return csvReadAllPair{recs: records}
		}
		if out.err != nil {
			return csvReadAllPair{err: out.err}
		}
		records = append(records, out.rec)
	}
}

// ReadAll reads all the remaining records from r.
// Each record is a slice of fields.
// A successful call returns err == nil, not err == csvEOF. Because ReadAll is
// defined to read until EOF, it does not treat end of data as an error to be
// reported.
//
// M7: fail-fast body like Read; 2-target call sites desugar to __csvReadAll.
func (r *Reader) ReadAll() ([][]string, error) {
	panic("csv: Reader.ReadAll is not in M7 scope as a direct call (only the recs, err := r.ReadAll() 2-target form desugars; only the 2-target form is desugared)")
}

// csvFieldPosPair is the Appendix M single-result carrier for FieldPos.
type csvFieldPosPair struct {
	line   int
	column int
}

// __csvFieldPos is the Appendix M desugar target behind Reader.FieldPos.
func __csvFieldPos(r *Reader, field int) csvFieldPosPair {
	if field < 0 || field >= len(r.fieldPositions) {
		panic("out of range index passed to FieldPos")
	}
	p := r.fieldPositions[field]
	return csvFieldPosPair{line: p.line, column: p.col}
}

// FieldPos returns the line and column corresponding to
// the start of the field with the given index in the slice most recently
// returned by Reader.Read. Numbering of lines and columns starts at 1;
// columns are counted in bytes, not runes.
//
// If this is called with an out-of-bounds index, it panics.
//
// M7: fail-fast body like Read; 2-target call sites desugar to __csvFieldPos.
func (r *Reader) FieldPos(field int) (line, column int) {
	panic("csv: Reader.FieldPos is not in M7 scope as a direct call (only the line, column := r.FieldPos(i) 2-target form desugars; only the 2-target form is desugared)")
}

// InputOffset returns the input byte offset of the current reader
// position. The offset gives the location of the end of the most recently
// read row and the beginning of the next row.
func (r *Reader) InputOffset() int64 {
	return r.offset
}

// A Writer writes records using CSV encoding.
//
// As returned by NewWriter, a Writer writes records terminated by a
// newline and uses ',' as the field delimiter. The exported fields can be
// changed to customize the details before the first call to Write or
// WriteAll.
//
// Writer.Comma is the field delimiter.
//
// If Writer.UseCRLF is true,
// the Writer ends each output line with \r\n instead of \n.
//
// M7 divergence: gc buffers through a bufio.Writer and Flush pushes the
// buffer to the io.Writer; the synthetic accumulates in the same in-memory
// buffer and Flush writes it to the io.Writer directly. Write itself never
// touches the io.Writer (errors surface at Flush, reported by Error),
// exactly like gc.
type Writer struct {
	Comma   rune // Field delimiter (set to ',' by NewWriter)
	UseCRLF bool // True to use \r\n as the line terminator
	buf     []byte
	w       io.Writer // nil for NewBufferWriter (in-memory only)
	werr    error     // sticky error from Flush's write to w
}

// NewWriter returns a new Writer that writes to w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{
		Comma: ',',
		w:     w,
	}
}

// NewBufferWriter returns a new Writer that accumulates output in memory;
// Bytes returns the accumulated output.
//
// M7 EXTENSION (not in gc 1.27.1): the in-memory constructor. NewWriter
// (exact gc signature) is also available.
func NewBufferWriter() *Writer {
	return &Writer{
		Comma: ',',
	}
}

// Bytes returns the accumulated output.
//
// M7 EXTENSION (not in gc 1.27.1): see NewBufferWriter.
func (w *Writer) Bytes() []byte {
	return w.buf
}

// Write writes a single CSV record to w along with any necessary quoting.
// A record is a slice of strings with each string being one field.
func (w *Writer) Write(record []string) error {
	if !csvValidDelim(w.Comma) {
		return csvErrInvalidDelim
	}

	for n, field := range record {
		if n > 0 {
			w.buf = append(w.buf, csvCommaBytes(w.Comma)...)
		}

		// If we don't have to have a quoted field then just
		// write out the field and continue to the next field.
		if !w.csvFieldNeedsQuotes(field) {
			w.buf = append(w.buf, field...)
			continue
		}

		w.buf = append(w.buf, '"')
		for len(field) > 0 {
			// Search for special characters.
			i := csvStringIndexAny(field, "\"\r\n")
			if i < 0 {
				i = len(field)
			}

			// Copy verbatim everything before the special character.
			w.buf = append(w.buf, field[:i]...)
			field = field[i:]

			// Encode the special character.
			if len(field) > 0 {
				switch field[0] {
				case '"':
					w.buf = append(w.buf, '"', '"')
				case '\r':
					if !w.UseCRLF {
						w.buf = append(w.buf, '\r')
					}
				case '\n':
					if w.UseCRLF {
						w.buf = append(w.buf, '\r', '\n')
					} else {
						w.buf = append(w.buf, '\n')
					}
				}
				field = field[1:]
			}
		}
		w.buf = append(w.buf, '"')
	}
	if w.UseCRLF {
		w.buf = append(w.buf, '\r', '\n')
	} else {
		w.buf = append(w.buf, '\n')
	}
	return nil
}

// csvCommaBytes encodes the Comma rune as UTF-8 bytes.
func csvCommaBytes(r rune) []byte {
	if r < 0x80 {
		return []byte{byte(r)}
	}
	var enc [4]byte
	n := 0
	switch {
	case r < 0x800:
		enc[0] = byte(0xC0 | (r >> 6))
		enc[1] = byte(0x80 | (r & 0x3F))
		n = 2
	case r < 0x10000:
		enc[0] = byte(0xE0 | (r >> 12))
		enc[1] = byte(0x80 | ((r >> 6) & 0x3F))
		enc[2] = byte(0x80 | (r & 0x3F))
		n = 3
	default:
		enc[0] = byte(0xF0 | (r >> 18))
		enc[1] = byte(0x80 | ((r >> 12) & 0x3F))
		enc[2] = byte(0x80 | ((r >> 6) & 0x3F))
		enc[3] = byte(0x80 | (r & 0x3F))
		n = 4
	}
	return enc[:n]
}

// Flush writes any buffered data to the underlying writer.
// Over a NewBufferWriter (in-memory) this is a no-op.
// To check if an error occurred during Flush, call Error.
func (w *Writer) Flush() {
	if w.w == nil || w.werr != nil {
		return
	}
	// 2-target interface call: desugars via the m8 io.Write entry,
	// dispatching through the itab to the concrete writer's pair helper.
	n, ferr := w.w.Write(w.buf)
	if ferr != nil {
		w.werr = ferr
		return
	}
	w.buf = w.buf[n:]
}

// Error reports any error that has occurred during
// a previous Write or Flush.
func (w *Writer) Error() error {
	return w.werr
}

// WriteAll writes multiple CSV records to w using Write and
// then calls Flush, returning any error from Write.
func (w *Writer) WriteAll(records [][]string) error {
	for _, record := range records {
		err := w.Write(record)
		if err != nil {
			return err
		}
	}
	w.Flush()
	return nil
}

// csvFieldNeedsQuotes reports whether our field must be enclosed in quotes.
// (gc's unexported method, inlined as a private helper since the Writer
// methods are real Go here.)
func (w *Writer) csvFieldNeedsQuotes(field string) bool {
	if field == "" {
		return false
	}

	if field == "\\." {
		return true
	}

	if w.Comma < 0x80 {
		for i := 0; i < len(field); i++ {
			c := field[i]
			if c == '\n' || c == '\r' || c == '"' || c == byte(w.Comma) {
				return true
			}
		}
	} else {
		if csvStringContainsRune(field, w.Comma) || csvStringContainsAny(field, "\"\r\n") {
			return true
		}
	}

	r1 := csvDecodeRuneString(field).r
	return csvIsSpace(r1)
}
`

// syntheticGobSource is the P3-M7 synthetic `encoding/gob` package:
// Encoder/Decoder over the closed dynamic any-model (nil, bool, all
// int/uint widths, float32/float64, string, []byte, []any,
// map[string]any), with a byte-identical wire format to gc 1.27.1 for
// the supported subset. Imports only the synthetic `errors` package.
//
// Wire summary (gc 1.27.1, verified by probe):
//   - builtin type ids: bool=1 int=2 uint=3 float=4 bytes=5 string=6
//     complex=7 interface=8; user types allocate from 64 in first-use
//     order (process-global, like gc).
//   - uints: <=0x7F direct, else big-endian preceded by negated length.
//   - ints: zigzag (low bit = complement flag).
//   - floats: bits byte-reversed; exact soft-float (no math import).
//     NaN canonicalizes (payload unobservable without math/unsafe).
//   - messages: [count][payload]; type definitions: [count][encodeInt(-id)]
//   - wireType struct; values: [count][encodeInt(id)][delta=0][value].
//   - interface values: [namelen][name][typeid][vcount][delta][value].
//   - composite descriptors: SliceT/MapT with empty Name, Key=6/Elem=8.
const syntheticGobSource = `package gob

import "errors"

// gobEncodeUint writes an encoded unsigned integer (gc encode.go's
// two-state encoding).
func gobEncodeUint(b *[]byte, x uint64) {
	if x <= 0x7F {
		*b = append(*b, byte(x))
		return
	}
	var be [8]byte
	xx := x
	for i := 7; i >= 0; i-- {
		be[i] = byte(xx)
		xx >>= 8
	}
	start := 0
	for start < 7 && be[start] == 0 {
		start++
	}
	byteLen := 8 - start
	*b = append(*b, byte(-byteLen))
	*b = append(*b, be[start:]...)
}

// gobEncodeInt writes an encoded signed integer (zigzag).
func gobEncodeInt(b *[]byte, i int64) {
	var x uint64
	if i < 0 {
		x = uint64(^i<<1) | 1
	} else {
		x = uint64(i << 1)
	}
	gobEncodeUint(b, x)
}

// gobReverseBytes64 is bits.ReverseBytes64 without math/bits.
func gobReverseBytes64(x uint64) uint64 {
	var r uint64
	for i := 0; i < 8; i++ {
		r = r<<8 | (x & 0xFF)
		x >>= 8
	}
	return r
}

var gobZero = 0.0
var gobPosInf = 1.0 / gobZero
var gobNaN = gobZero / gobZero

// gobPow2 returns 2^e exactly (e in [-1074, 971]); every needed power is
// exactly representable, and power-of-2 scaling is exact.
func gobPow2(e int) float64 {
	p := 1.0
	if e >= 0 {
		for i := 0; i < e; i++ {
			p *= 2
		}
	} else {
		for i := 0; i < -e; i++ {
			p /= 2
		}
	}
	return p
}

// gobFloat64Bits is math.Float64bits without the math import: exact for
// all finite values, +-0 and +-Inf. NaN canonicalizes to
// 0x7ff8000000000000 (the payload is unobservable without math/unsafe;
// documented M7 divergence, re-test when math lands).
func gobFloat64Bits(f float64) uint64 {
	if f == 0 {
		if 1/f < 0 {
			return 1 << 63
		}
		return 0
	}
	sign := uint64(0)
	if f < 0 {
		sign = 1 << 63
		f = -f
	}
	if f != f {
		return sign | 0x7ff8000000000000
	}
	if f == gobPosInf {
		return sign | 0x7ff0000000000000
	}
	// Normalize: f = m * 2^e with m in [1,2). Power-of-2 scaling is exact.
	e := 0
	for f < 1 {
		f *= 2
		e--
	}
	for f >= 2 {
		f /= 2
		e++
	}
	// m = M / 2^52 with integer M in [2^52, 2^53). (m-1) is exact by
	// Sterbenz, and (m-1)*2^52 < 2^52 is exactly representable.
	m := uint64((f - 1) * 4503599627370496.0)
	biased := e + 1023
	if biased >= 1 {
		return sign | uint64(biased)<<52 | m
	}
	// Subnormal: value = M * 2^(e-52) = mantOut * 2^-1074 with
	// mantOut = M >> shift, shift = -(e+1022) > 0. The shift is exact:
	// the true value is an integer multiple of 2^-1074, so the
	// discarded low bits are zero.
	shift := -(e + 1022)
	mantOut := (m + 4503599627370496) >> uint(shift)
	return sign | mantOut
}

// gobFloat64FromBits is math.Float64frombits without the math import:
// exact for every bit pattern (NaN payloads canonicalize, see
// gobFloat64Bits).
func gobFloat64FromBits(b uint64) float64 {
	sign := b >> 63
	bexp := (b >> 52) & 0x7ff
	mant := b & 0xFFFFFFFFFFFFF
	neg := sign == 1
	switch {
	case bexp == 0x7ff:
		if mant == 0 {
			if neg {
				return -gobPosInf
			}
			return gobPosInf
		}
		if neg {
			return -gobNaN
		}
		return gobNaN
	case bexp == 0:
		var f float64
		if mant == 0 {
			f = 0
		} else {
			// float64(mant) exact (mant < 2^52); scaling by 2^-1074
			// is exact: the product is the representable subnormal.
			f = float64(mant) * gobPow2(-1074)
		}
		if neg {
			return -f
		}
		return f
	default:
		// (2^52+mant) < 2^53 exact; scaling by 2^(bexp-1075) exact:
		// the product is the representable normal value.
		f := (4503599627370496.0 + float64(mant)) * gobPow2(int(bexp)-1075)
		if neg {
			return -f
		}
		return f
	}
}

// gobDelta tracks delta-encoded field numbers inside one struct.
type gobDelta struct {
	last int
}

func (t *gobDelta) emit(b *[]byte, f int) {
	gobEncodeUint(b, uint64(f-t.last))
	t.last = f
}

// gobClass classifies dynamic values in the M7 any-model.
const (
	gobClassBool = iota + 1
	gobClassInt
	gobClassUint
	gobClassFloat
	gobClassString
	gobClassBytes
	gobClassSliceAny
	gobClassMapStringAny
)

// gobIfaceInfo is the single-result carrier for gobClassify (v1 cannot
// call multi-result functions, WS D).
type gobIfaceInfo struct {
	name       string
	class      int
	width      int // int/uint/float width in bits
	registered bool
}

// gobClassify maps a dynamic value to its M7 any-model class and wire
// name. class == 0 means outside the M7 model (needs reflect).
func gobClassify(v any) gobIfaceInfo {
	switch t := v.(type) {
	case bool:
		return gobIfaceInfo{name: "bool", class: gobClassBool, registered: true}
	case int:
		_ = t
		return gobIfaceInfo{name: "int", class: gobClassInt, width: 64, registered: true}
	case int8:
		_ = t
		return gobIfaceInfo{name: "int8", class: gobClassInt, width: 8, registered: true}
	case int16:
		_ = t
		return gobIfaceInfo{name: "int16", class: gobClassInt, width: 16, registered: true}
	case int32:
		_ = t
		return gobIfaceInfo{name: "int32", class: gobClassInt, width: 32, registered: true}
	case int64:
		_ = t
		return gobIfaceInfo{name: "int64", class: gobClassInt, width: 64, registered: true}
	case uint:
		_ = t
		return gobIfaceInfo{name: "uint", class: gobClassUint, width: 64, registered: true}
	case uint8:
		_ = t
		return gobIfaceInfo{name: "uint8", class: gobClassUint, width: 8, registered: true}
	case uint16:
		_ = t
		return gobIfaceInfo{name: "uint16", class: gobClassUint, width: 16, registered: true}
	case uint32:
		_ = t
		return gobIfaceInfo{name: "uint32", class: gobClassUint, width: 32, registered: true}
	case uint64:
		_ = t
		return gobIfaceInfo{name: "uint64", class: gobClassUint, width: 64, registered: true}
	case uintptr:
		_ = t
		return gobIfaceInfo{name: "uintptr", class: gobClassUint, width: 64, registered: true}
	case float32:
		_ = t
		return gobIfaceInfo{name: "float32", class: gobClassFloat, width: 32, registered: true}
	case float64:
		_ = t
		return gobIfaceInfo{name: "float64", class: gobClassFloat, width: 64, registered: true}
	case string:
		_ = t
		return gobIfaceInfo{name: "string", class: gobClassString, registered: true}
	case []byte:
		_ = t
		return gobIfaceInfo{name: "[]uint8", class: gobClassBytes, registered: true}
	case []any:
		_ = t
		return gobIfaceInfo{name: "[]interface {}", class: gobClassSliceAny, registered: true}
	case map[string]any:
		_ = t
		return gobIfaceInfo{name: "map[string]interface {}", class: gobClassMapStringAny, registered: true}
	}
	return gobIfaceInfo{}
}

// gobEncodeScalar encodes a bool/int/uint/float/string scalar value
// (no type id, no field delta — the caller frames it).
func gobEncodeScalar(b *[]byte, v any) {
	switch t := v.(type) {
	case bool:
		if t {
			gobEncodeUint(b, 1)
		} else {
			gobEncodeUint(b, 0)
		}
	case int:
		gobEncodeInt(b, int64(t))
	case int8:
		gobEncodeInt(b, int64(t))
	case int16:
		gobEncodeInt(b, int64(t))
	case int32:
		gobEncodeInt(b, int64(t))
	case int64:
		gobEncodeInt(b, t)
	case uint:
		gobEncodeUint(b, uint64(t))
	case uint8:
		gobEncodeUint(b, uint64(t))
	case uint16:
		gobEncodeUint(b, uint64(t))
	case uint32:
		gobEncodeUint(b, uint64(t))
	case uint64:
		gobEncodeUint(b, t)
	case uintptr:
		gobEncodeUint(b, uint64(t))
	case float32:
		gobEncodeUint(b, gobReverseBytes64(gobFloat64Bits(float64(t))))
	case float64:
		gobEncodeUint(b, gobReverseBytes64(gobFloat64Bits(t)))
	case string:
		gobEncodeUint(b, uint64(len(t)))
		*b = append(*b, t...)
	case []byte:
		gobEncodeUint(b, uint64(len(t)))
		*b = append(*b, t...)
	}
}

// gobRegEntry is a registered concrete-type record.
type gobRegEntry struct {
	class int
	width int
}

// gobNameToKind mirrors gc's nameToConcreteType; gobKindToName mirrors
// concreteTypeToName (keyed by class+width, like gc's reflect.Type key).
type gobKindKey struct {
	class int
	width int
}

var gobNameToKind = map[string]gobRegEntry{}
var gobKindToName = map[gobKindKey]string{}
var gobRegInitDone = false

// gobEnsureReg mirrors gc's registerBasics: the scalar concrete types
// are pre-registered for interface transmission.
func gobEnsureReg() {
	if gobRegInitDone {
		return
	}
	gobRegInitDone = true
	gobNameToKind["bool"] = gobRegEntry{class: gobClassBool}
	gobNameToKind["int"] = gobRegEntry{class: gobClassInt, width: 64}
	gobNameToKind["int8"] = gobRegEntry{class: gobClassInt, width: 8}
	gobNameToKind["int16"] = gobRegEntry{class: gobClassInt, width: 16}
	gobNameToKind["int32"] = gobRegEntry{class: gobClassInt, width: 32}
	gobNameToKind["int64"] = gobRegEntry{class: gobClassInt, width: 64}
	gobNameToKind["uint"] = gobRegEntry{class: gobClassUint, width: 64}
	gobNameToKind["uint8"] = gobRegEntry{class: gobClassUint, width: 8}
	gobNameToKind["uint16"] = gobRegEntry{class: gobClassUint, width: 16}
	gobNameToKind["uint32"] = gobRegEntry{class: gobClassUint, width: 32}
	gobNameToKind["uint64"] = gobRegEntry{class: gobClassUint, width: 64}
	gobNameToKind["uintptr"] = gobRegEntry{class: gobClassUint, width: 64}
	gobNameToKind["float32"] = gobRegEntry{class: gobClassFloat, width: 32}
	gobNameToKind["float64"] = gobRegEntry{class: gobClassFloat, width: 64}
	gobNameToKind["string"] = gobRegEntry{class: gobClassString}
	// []byte inside an interface uses name "[]uint8" (gc parity, verified
	// by probe); at top level it uses the builtin "bytes" id.
	gobNameToKind["[]uint8"] = gobRegEntry{class: gobClassBytes}
	// M7 CLOSED MODEL: the composite types are pre-registered (gc requires
	// explicit Register for these; M7 pre-registers them so the any-model
	// "just works". Documented divergence; wire format is identical).
	gobNameToKind["[]interface {}"] = gobRegEntry{class: gobClassSliceAny}
	gobNameToKind["map[string]interface {}"] = gobRegEntry{class: gobClassMapStringAny}
	// Populate the reverse map (keyed by class+width, like gc's
	// concreteTypeToName keyed by reflect.Type).
	for name, e := range gobNameToKind {
		gobKindToName[gobKindKey{class: e.class, width: e.width}] = name
	}
}

// gobQuote renders a Go-syntax double-quoted string (for %q-style error
// messages) without the fmt import.
func gobQuote(s string) string {
	var b []byte
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		default:
			if c < 0x20 || c >= 0x7f {
				b = append(b, '\\', 'x')
				b = append(b, "0123456789abcdef"[c>>4], "0123456789abcdef"[c&0xf])
			} else {
				b = append(b, c)
			}
		}
	}
	b = append(b, '"')
	return string(b)
}

// Register records a type, identified by a value for that type, under its
// internal type name. That name will identify the concrete type of a value
// sent or received as an interface variable.
//
// M7: all types in the closed any-model are pre-registered (like gc's
// registerBasics); re-registering one under its default name is a no-op.
// Anything else fails fast (needs reflect, P3-M8).
func Register(value any) {
	gobEnsureReg()
	info := gobClassify(value)
	if info.class == 0 {
		panic("gob: Register: type not in M7 scope (needs reflect, P3-M8)")
	}
	// Pre-registered; gc's Register of an already-registered type is a no-op.
}

// RegisterName is like Register but uses the provided name rather than
// the type's default. M7: all closed-model types are pre-registered, so
// giving one a custom name panics like gc's "duplicate names" (the type
// already has a name).
func RegisterName(name string, value any) {
	gobEnsureReg()
	if name == "" {
		panic("attempt to register empty name")
	}
	info := gobClassify(value)
	if info.class == 0 {
		panic("gob: RegisterName: type not in M7 scope (needs reflect, P3-M8)")
	}
	gobRegisterName(name, info.class, info.width)
}

// gobRegisterName records the name<->class mapping with gc's duplicate
// panics (RegisterName error strings verified against gc 1.27.1).
func gobRegisterName(name string, class, width int) {
	key := gobKindKey{class: class, width: width}
	if e, ok := gobNameToKind[name]; ok {
		if e.class != class || e.width != width {
			panic("gob: registering duplicate types for " + gobQuote(name) + ": " + gobRegName(e.class) + " != " + gobRegName(class))
		}
		return
	}
	if n, ok := gobKindToName[key]; ok {
		if n != name {
			panic("gob: registering duplicate names for " + gobRegName(class) + ": " + gobQuote(n) + " != " + gobQuote(name))
		}
		return
	}
	gobNameToKind[name] = gobRegEntry{class: class, width: width}
	gobKindToName[key] = name
}

// gobRegName is the display name of a registered class (gc's rt.String()).
func gobRegName(class int) string {
	switch class {
	case gobClassBool:
		return "bool"
	case gobClassInt:
		return "int"
	case gobClassUint:
		return "uint"
	case gobClassFloat:
		return "float"
	case gobClassString:
		return "string"
	case gobClassBytes:
		return "[]uint8"
	case gobClassSliceAny:
		return "[]interface {}"
	case gobClassMapStringAny:
		return "map[string]interface {}"
	}
	return "unknown"
}

// gobNextID is the process-global user type id allocator (gc's
// process-global idToTypeSlice for single-threaded programs; M7 encoders
// are not safe for concurrent use — documented divergence).
var gobNextID = 64

// gobIDKey identifies a composite type for id allocation. In the M7
// closed model, []any and map[string]any are the only user types, so
// (class, width) uniquely identifies the type (like gc's reflect.Type
// key in its process-global registry).
type gobIDKey struct {
	class int
	width int
}

var gobTypeIDCache = map[gobIDKey]int{}

// gobAllocID allocates (or reuses) the process-global type id for a
// composite type, matching gc's first-use-order allocation from 64.
func gobAllocID(class, width int) int {
	gobEnsureReg()
	key := gobIDKey{class: class, width: width}
	if id, ok := gobTypeIDCache[key]; ok {
		return id
	}
	id := gobNextID
	gobNextID++
	gobTypeIDCache[key] = id
	return id
}

// An Encoder manages the transmission of type and data information.
// M7 divergence: gc's NewEncoder takes an io.Writer (io is out of M7
// scope, P3-M8 re-test trigger); use NewBufferEncoder and Bytes.
type Encoder struct {
	out []byte
	err error
	// sentIDs records transmitted composite type ids (gc's enc.sent):
	// index gobClassSliceAny/gobClassMapStringAny.
	sentIDs [gobClassMapStringAny + 1]int
	// pending queues value messages for nested composites whose
	// descriptor was just sent inline; they are emitted after the
	// parent message, in order (matching gc's wire order).
	pending [][]byte
}

// NewBufferEncoder returns a new Encoder that accumulates output in
// memory; Bytes returns the accumulated output.
//
// M7 EXTENSION (not in gc 1.27.1): gc's NewEncoder takes an io.Writer;
// io is out of M7 scope, so this in-memory constructor is provided
// instead. Re-test trigger: when io lands (P3-M8), add NewEncoder with
// the exact gc signature.
func NewBufferEncoder() *Encoder {
	gobEnsureReg()
	return &Encoder{}
}

// Bytes returns the accumulated encoded output.
//
// M7 EXTENSION (not in gc 1.27.1): see NewBufferEncoder.
func (enc *Encoder) Bytes() []byte {
	return enc.out
}

func (enc *Encoder) fail(msg string) {
	if enc.err == nil {
		enc.err = errors.New(msg)
	}
}

// writeMsg frames payload as a count-delimited message.
func (enc *Encoder) writeMsg(payload []byte) {
	var c []byte
	gobEncodeUint(&c, uint64(len(payload)))
	enc.out = append(enc.out, c...)
	enc.out = append(enc.out, payload...)
}

// gobSliceAnyDescriptor returns the (undelimited) type descriptor bytes
// for []any: [encodeInt(-id)] + wireType struct.
func gobSliceAnyDescriptor(id int) []byte {
	var d []byte
	gobEncodeInt(&d, -int64(id))
	// wireType{ SliceT: &sliceType{ CommonType{ Name:"", Id:id }, Elem: 8 } }
	tr := gobDelta{last: -1}
	tr.emit(&d, 1) // SliceT
	tr2 := gobDelta{last: -1}
	tr2.emit(&d, 0) // CommonType
	tr3 := gobDelta{last: -1}
	// Name is "" — zero strings are not transmitted.
	tr3.emit(&d, 1) // Id
	gobEncodeInt(&d, int64(id))
	gobEncodeUint(&d, 0) // CommonType terminator
	tr2.emit(&d, 1)      // Elem
	gobEncodeInt(&d, 8)  // interface
	gobEncodeUint(&d, 0) // sliceType terminator
	gobEncodeUint(&d, 0) // wireType terminator
	return d
}

// sliceAnyID returns the type id for []any. If the descriptor hasn't been
// sent by this encoder yet, it is written as a separate message (top-level
// use, like gc's sendTypeDescriptor to the top-level writer).
func (enc *Encoder) sliceAnyID() int {
	id := gobAllocID(gobClassSliceAny, 0)
	if enc.sentIDs[gobClassSliceAny] == id {
		return id
	}
	enc.sentIDs[gobClassSliceAny] = id
	enc.writeMsg(gobSliceAnyDescriptor(id))
	return id
}

// sliceAnyIDForInterface returns the type id for []any as an interface
// element. It also reports whether the value must go as a separate
// top-level message (true when the descriptor was just written inline,
// matching gc's wire format, verified by probe).
func (enc *Encoder) sliceAnyIDForInterface(b *[]byte) (int64, bool) {
	id := gobAllocID(gobClassSliceAny, 0)
	if enc.sentIDs[gobClassSliceAny] == id {
		return int64(id), false
	}
	enc.sentIDs[gobClassSliceAny] = id
	*b = append(*b, gobSliceAnyDescriptor(id)...)
	return int64(id), true
}

// gobMapStringAnyDescriptor returns the (undelimited) type descriptor
// bytes for map[string]any: [encodeInt(-id)] + wireType struct.
func gobMapStringAnyDescriptor(id int) []byte {
	var d []byte
	gobEncodeInt(&d, -int64(id))
	// wireType{ MapT: &mapType{ CommonType{ Name:"", Id:id }, Key:6, Elem:8 } }
	tr := gobDelta{last: -1}
	tr.emit(&d, 3) // MapT
	tr2 := gobDelta{last: -1}
	tr2.emit(&d, 0) // CommonType
	tr3 := gobDelta{last: -1}
	tr3.emit(&d, 1) // Id
	gobEncodeInt(&d, int64(id))
	gobEncodeUint(&d, 0) // CommonType terminator
	tr2.emit(&d, 1)      // Key
	gobEncodeInt(&d, 6)  // string
	tr2.emit(&d, 2)      // Elem
	gobEncodeInt(&d, 8)  // interface
	gobEncodeUint(&d, 0) // mapType terminator
	gobEncodeUint(&d, 0) // wireType terminator
	return d
}

// mapStringAnyID returns the type id for map[string]any, transmitting
// the type descriptor on first use.
func (enc *Encoder) mapStringAnyID() int {
	id := gobAllocID(gobClassMapStringAny, 0)
	if enc.sentIDs[gobClassMapStringAny] == id {
		return id
	}
	enc.sentIDs[gobClassMapStringAny] = id
	enc.writeMsg(gobMapStringAnyDescriptor(id))
	return id
}

// mapStringAnyIDForInterface returns the type id for map[string]any as an
// interface element, with the separate-message flag (see
// sliceAnyIDForInterface).
func (enc *Encoder) mapStringAnyIDForInterface(b *[]byte) (int64, bool) {
	id := gobAllocID(gobClassMapStringAny, 0)
	if enc.sentIDs[gobClassMapStringAny] == id {
		return int64(id), false
	}
	enc.sentIDs[gobClassMapStringAny] = id
	*b = append(*b, gobMapStringAnyDescriptor(id)...)
	return int64(id), true
}

// Encode transmits the data item represented by the empty interface value,
// guaranteeing that all necessary type information has been transmitted first.
// Passing nil fails with "gob: cannot encode nil value" (like gc).
func (enc *Encoder) Encode(v any) error {
	gobEnsureReg()
	enc.err = nil
	if v == nil {
		return errors.New("gob: cannot encode nil value")
	}
	info := gobClassify(v)
	switch info.class {
	case gobClassBool, gobClassInt, gobClassUint, gobClassFloat, gobClassString:
		enc.encodeSingle(gobClassTypeID(info.class), v)
	case gobClassBytes:
		enc.encodeSingle(5, v)
	case gobClassSliceAny:
		enc.encodeSliceAny(v.([]any))
	case gobClassMapStringAny:
		enc.encodeMapStringAny(v.(map[string]any))
	default:
		return errors.New("gob: cannot encode value: type not in M7 scope (needs reflect, P3-M8)")
	}
	// Flush queued nested-composite value messages, in order.
	for _, p := range enc.pending {
		enc.writeMsg(p)
		if enc.err != nil {
			break
		}
	}
	enc.pending = nil
	return enc.err
}

// gobClassTypeID maps scalar classes to builtin type ids.
func gobClassTypeID(class int) int64 {
	switch class {
	case gobClassBool:
		return 1
	case gobClassInt:
		return 2
	case gobClassUint:
		return 3
	case gobClassFloat:
		return 4
	case gobClassBytes:
		return 5
	case gobClassString:
		return 6
	}
	return 0
}

// encodeSingle encodes a top-level scalar/bytes value (gc's encodeSingle:
// field delta 0, then the value; zero values are transmitted).
func (enc *Encoder) encodeSingle(typeID int64, v any) {
	var b []byte
	gobEncodeInt(&b, typeID)
	gobEncodeUint(&b, 0)
	if typeID == 5 {
		s := v.([]byte)
		gobEncodeUint(&b, uint64(len(s)))
		b = append(b, s...)
	} else {
		gobEncodeScalar(&b, v)
	}
	enc.writeMsg(b)
}

// encodeSliceAny encodes a top-level []any (gc's encodeSingle +
// encodeArray: field delta 0, count, interface elements).
func (enc *Encoder) encodeSliceAny(s []any) {
	id := enc.sliceAnyID()
	if enc.err != nil {
		return
	}
	var b []byte
	gobEncodeInt(&b, int64(id))
	gobEncodeUint(&b, 0)
	gobEncodeUint(&b, uint64(len(s)))
	for _, e := range s {
		enc.encodeInterface(&b, e)
		if enc.err != nil {
			return
		}
	}
	enc.writeMsg(b)
}

// encodeMapStringAny encodes a top-level map[string]any.
func (enc *Encoder) encodeMapStringAny(m map[string]any) {
	id := enc.mapStringAnyID()
	if enc.err != nil {
		return
	}
	var b []byte
	gobEncodeInt(&b, int64(id))
	gobEncodeUint(&b, 0)
	gobEncodeUint(&b, uint64(len(m)))
	for k, e := range m {
		gobEncodeUint(&b, uint64(len(k)))
		b = append(b, k...)
		enc.encodeInterface(&b, e)
		if enc.err != nil {
			return
		}
	}
	enc.writeMsg(b)
}

// encodeInterface encodes an interface element value: the concrete type
// name, its type id (defining the type first if needed), and the
// count-delimited concrete value. A nil value is the empty name.
func (enc *Encoder) encodeInterface(b *[]byte, v any) {
	if v == nil {
		gobEncodeUint(b, 0)
		return
	}
	info := gobClassify(v)
	if info.class == 0 {
		enc.fail("gob: cannot encode interface element: type not in M7 scope (needs reflect, P3-M8)")
		return
	}
	if !info.registered {
		enc.fail("gob: type not registered for interface: " + info.name)
		return
	}
	gobEncodeUint(b, uint64(len(info.name)))
	*b = append(*b, info.name...)
	// For composites, gc's behavior depends on whether the descriptor was
	// just sent: if first use, the descriptor goes inline in the parent
	// and the value goes as a separate top-level message; otherwise the
	// value is inline. (Verified by probe.)
	var id int64
	separateMsg := false
	switch info.class {
	case gobClassSliceAny:
		id, separateMsg = enc.sliceAnyIDForInterface(b)
	case gobClassMapStringAny:
		id, separateMsg = enc.mapStringAnyIDForInterface(b)
	default:
		id = gobClassTypeID(info.class)
	}
	if enc.err != nil {
		return
	}
	// Build the value payload: [typeid][delimited [delta][value]].
	var payload []byte
	gobEncodeInt(&payload, id)
	var vb []byte
	gobEncodeUint(&vb, 0) // field delta
	switch info.class {
	case gobClassSliceAny:
		s := v.([]any)
		gobEncodeUint(&vb, uint64(len(s)))
		for _, e := range s {
			enc.encodeInterface(&vb, e)
			if enc.err != nil {
				return
			}
		}
	case gobClassMapStringAny:
		m := v.(map[string]any)
		gobEncodeUint(&vb, uint64(len(m)))
		for k, e := range m {
			gobEncodeUint(&vb, uint64(len(k)))
			vb = append(vb, k...)
			enc.encodeInterface(&vb, e)
			if enc.err != nil {
				return
			}
		}
	default:
		gobEncodeScalar(&vb, v)
	}
	gobEncodeUint(&payload, uint64(len(vb)))
	payload = append(payload, vb...)
	if separateMsg {
		// First use: value goes as a separate top-level message,
		// queued to preserve parent-before-child order.
		enc.pending = append(enc.pending, payload)
	} else {
		// Already sent: value goes inline in the parent.
		*b = append(*b, payload...)
	}
}

// gobErrEOF mirrors io.EOF; gobErrUnexpectedEOF mirrors io.ErrUnexpectedEOF.
var gobErrEOF = errors.New("EOF")
var gobErrUnexpectedEOF = errors.New("unexpected EOF")

// gobDecBuf is a cursor over one message's payload (gc's decoderState +
// decBuffer, single-result style: errors accumulate on dec.err).
type gobDecBuf struct {
	dec  *Decoder
	data []byte
	off  int
}

func (b *gobDecBuf) rest() int {
	return len(b.data) - b.off
}

func (b *gobDecBuf) u8() byte {
	if b.off >= len(b.data) {
		b.dec.fail("unexpected EOF")
		return 0
	}
	c := b.data[b.off]
	b.off++
	return c
}

// uint decodes an encoded unsigned integer (gc's decodeUint).
func (b *gobDecBuf) uint() uint64 {
	c := b.u8()
	if b.dec.err != nil {
		return 0
	}
	if c <= 0x7f {
		return uint64(c)
	}
	n := -int(int8(c))
	if n > 8 {
		b.dec.fail("gob: encoded unsigned integer out of range")
		return 0
	}
	var x uint64
	for i := 0; i < n; i++ {
		d := b.u8()
		if b.dec.err != nil {
			return 0
		}
		x = x<<8 | uint64(d)
	}
	return x
}

// int decodes an encoded signed integer (gc's decodeInt: zigzag).
func (b *gobDecBuf) int() int64 {
	x := b.uint()
	if b.dec.err != nil {
		return 0
	}
	if x&1 != 0 {
		return ^int64(x >> 1)
	}
	return int64(x >> 1)
}

func (b *gobDecBuf) bytes(n int) []byte {
	if n < 0 || b.off+n > len(b.data) {
		b.dec.fail("unexpected EOF")
		return nil
	}
	s := b.data[b.off : b.off+n]
	b.off += n
	return s
}

// A Decoder manages the receipt of type and data information.
// M7 divergence: gc's NewDecoder takes an io.Reader (io is out of M7
// scope, P3-M8 re-test trigger); use NewBufferDecoder.
type Decoder struct {
	data []byte
	off  int
	// wire maps remote user type ids to composite kinds
	// (1 = []any, 2 = map[string]any), like gc's dec.wireType.
	wire map[int]int
	err  error
	// valBuf is the current value's payload cursor, set by typeSeq.
	valBuf *gobDecBuf
}

// NewBufferDecoder returns a Decoder that reads from data.
//
// M7 EXTENSION (not in gc 1.27.1): gc's NewDecoder takes an io.Reader;
// io is out of M7 scope, so this in-memory constructor is provided
// instead. Re-test trigger: when io lands (P3-M8), add NewDecoder with
// the exact gc signature.
func NewBufferDecoder(data []byte) *Decoder {
	gobEnsureReg()
	return &Decoder{data: data, wire: make(map[int]int)}
}

func (dec *Decoder) fail(msg string) {
	if dec.err == nil {
		dec.err = errors.New(msg)
	}
}

// readMsg reads one count-delimited message payload (gc's recvMessage).
func (dec *Decoder) readMsg() []byte {
	if dec.err != nil {
		return nil
	}
	if dec.off >= len(dec.data) {
		dec.err = gobErrEOF
		return nil
	}
	buf := &gobDecBuf{dec: dec, data: dec.data, off: dec.off}
	n := buf.uint()
	if dec.err != nil {
		return nil
	}
	if n >= 1<<33 {
		dec.fail("invalid message length")
		return nil
	}
	ni := int(n)
	if buf.off+ni > len(dec.data) {
		dec.err = gobErrUnexpectedEOF
		return nil
	}
	msg := dec.data[buf.off : buf.off+ni]
	dec.off = buf.off + ni
	return msg
}

// typeSeq parses the type sequence before a value (gc's
// decodeTypeSequence): processes (-id, wireType) definition messages and
// returns the value's type id (>= 0), leaving the value payload in
// dec.valBuf. Returns -1 on error/EOF.
func (dec *Decoder) typeSeq(isInterface bool) int64 {
	first := true
	for dec.err == nil {
		msg := dec.readMsg()
		if dec.err != nil {
			if !first && dec.err == gobErrEOF {
				dec.err = gobErrUnexpectedEOF
			}
			return -1
		}
		first = false
		buf := &gobDecBuf{dec: dec, data: msg}
		id := buf.int()
		if dec.err != nil {
			return -1
		}
		if id >= 0 {
			dec.valBuf = buf
			return id
		}
		dec.recvType(-id, buf)
		if dec.err != nil {
			return -1
		}
		if buf.rest() > 0 {
			if !isInterface {
				dec.fail("extra data in buffer")
				return -1
			}
			buf.uint() // skip delimited value count
		}
	}
	return -1
}

// recvType records a type definition (gc's recvType).
func (dec *Decoder) recvType(id int64, buf *gobDecBuf) {
	if id < 64 || dec.wire[int(id)] != 0 {
		dec.fail("gob: duplicate type received")
		return
	}
	kind := dec.parseWireType(buf)
	if dec.err != nil {
		return
	}
	dec.wire[int(id)] = kind
}

// parseWireType parses a wireType struct value; returns 1 for []any,
// 2 for map[string]any, failing on anything else.
func (dec *Decoder) parseWireType(buf *gobDecBuf) int {
	kind := 0
	fieldnum := -1
	for dec.err == nil {
		d := buf.uint()
		if dec.err != nil {
			return 0
		}
		if d == 0 {
			break
		}
		fieldnum += int(d)
		switch fieldnum {
		case 1: // SliceT
			if dec.parseSliceType(buf) {
				kind = 1
			} else if dec.err == nil {
				dec.fail("gob: M7: unsupported slice descriptor (needs reflect, P3-M8)")
				return 0
			}
		case 3: // MapT
			if dec.parseMapType(buf) {
				kind = 2
			} else if dec.err == nil {
				dec.fail("gob: M7: unsupported map descriptor (needs reflect, P3-M8)")
				return 0
			}
		default:
			dec.fail("gob: M7: unsupported type descriptor (needs reflect, P3-M8)")
			return 0
		}
	}
	if dec.err == nil && kind == 0 {
		dec.fail("gob: M7: empty type descriptor (needs reflect, P3-M8)")
	}
	return kind
}

// parseCommonType consumes a CommonType struct value (Name, Id).
func (dec *Decoder) parseCommonType(buf *gobDecBuf) {
	fieldnum := -1
	for dec.err == nil {
		d := buf.uint()
		if dec.err != nil {
			return
		}
		if d == 0 {
			break
		}
		fieldnum += int(d)
		switch fieldnum {
		case 0: // Name
			n := buf.uint()
			if dec.err != nil {
				return
			}
			buf.bytes(int(n))
		case 1: // Id
			buf.int()
		default:
			dec.fail("gob: M7: unsupported type descriptor (needs reflect, P3-M8)")
			return
		}
	}
}

// parseSliceType parses a sliceType struct; reports whether Elem is the
// interface type id (8).
func (dec *Decoder) parseSliceType(buf *gobDecBuf) bool {
	elemIsIface := false
	fieldnum := -1
	for dec.err == nil {
		d := buf.uint()
		if dec.err != nil {
			return false
		}
		if d == 0 {
			break
		}
		fieldnum += int(d)
		switch fieldnum {
		case 0: // CommonType
			dec.parseCommonType(buf)
		case 1: // Elem
			if buf.int() == 8 {
				elemIsIface = true
			}
		default:
			dec.fail("gob: M7: unsupported slice descriptor (needs reflect, P3-M8)")
			return false
		}
	}
	return elemIsIface
}

// parseMapType parses a mapType struct; reports whether Key is string
// (6) and Elem is interface (8).
func (dec *Decoder) parseMapType(buf *gobDecBuf) bool {
	keyOK := false
	elemOK := false
	fieldnum := -1
	for dec.err == nil {
		d := buf.uint()
		if dec.err != nil {
			return false
		}
		if d == 0 {
			break
		}
		fieldnum += int(d)
		switch fieldnum {
		case 0: // CommonType
			dec.parseCommonType(buf)
		case 1: // Key
			if buf.int() == 6 {
				keyOK = true
			}
		case 2: // Elem
			if buf.int() == 8 {
				elemOK = true
			}
		default:
			dec.fail("gob: M7: unsupported map descriptor (needs reflect, P3-M8)")
			return false
		}
	}
	return keyOK && elemOK
}

// gobBuiltinTypeName is the gc typeString table for builtin ids
// (verified against gc 1.27.1 error output).
func gobBuiltinTypeName(id int64) (string, bool) {
	switch id {
	case 1:
		return "bool", true
	case 2:
		return "int", true
	case 3:
		return "uint", true
	case 4:
		return "float", true
	case 5:
		return "bytes", true
	case 6:
		return "string", true
	case 7:
		return "complex", true
	case 8:
		return "interface", true
	case 9:
		return "_reserved7", true
	case 10:
		return "_reserved6", true
	case 11:
		return "_reserved5", true
	case 12:
		return "_reserved4", true
	case 13:
		return "_reserved3", true
	case 14:
		return "_reserved2", true
	case 15:
		return "_reserved1", true
	case 16:
		return "wireType = struct { ArrayT arrayType = struct { CommonType CommonType = struct { Name string; Id int; }; Elem int; Len int; }; SliceT sliceType = struct { CommonType CommonType; Elem int; }; StructT structType = struct { CommonType CommonType; Field []fieldType = struct { Name string; Id int; }; }; MapT mapType = struct { CommonType CommonType; Key int; Elem int; }; GobEncoderT gobEncoderType = struct { CommonType CommonType; }; BinaryMarshalerT gobEncoderType; TextMarshalerT gobEncoderType; }", true
	case 17:
		return "arrayType = struct { CommonType CommonType = struct { Name string; Id int; }; Elem int; Len int; }", true
	case 18:
		return "CommonType = struct { Name string; Id int; }", true
	case 19:
		return "sliceType = struct { CommonType CommonType = struct { Name string; Id int; }; Elem int; }", true
	case 20:
		return "structType = struct { CommonType CommonType = struct { Name string; Id int; }; Field []fieldType = struct { Name string; Id int; }; }", true
	case 21:
		return "fieldType = struct { Name string; Id int; }", true
	case 22:
		return "[]fieldType = struct { Name string; Id int; }", true
	case 23:
		return "mapType = struct { CommonType CommonType = struct { Name string; Id int; }; Key int; Elem int; }", true
	}
	return "", false
}

// remoteTypeString names a remote type id for error messages (gc's
// typeString: builtins, then "[]interface"/"map[string]interface").
func (dec *Decoder) remoteTypeString(id int64) string {
	if s, ok := gobBuiltinTypeName(id); ok {
		return s
	}
	switch dec.wire[int(id)] {
	case 1:
		return "[]interface"
	case 2:
		return "map[string]interface"
	}
	return "unknown type"
}

// checkSingle reads the type sequence, requires the expected type id,
// and consumes the singleton field delta. Returns the value cursor.
func (dec *Decoder) checkSingle(localName string, wantID int64) *gobDecBuf {
	id := dec.typeSeq(false)
	if dec.err != nil {
		return nil
	}
	if id != wantID {
		dec.fail("gob: decoding into local type " + localName + ", received remote type " + dec.remoteTypeString(id))
		return nil
	}
	buf := dec.valBuf
	if buf.uint() != 0 {
		dec.fail("gob: decode: corrupted data: non-zero delta for singleton")
		return nil
	}
	return buf
}

// Decode reads the next value from the input and stores it in the data
// represented by the empty interface value. If e is nil, the value is
// discarded. Otherwise e must be a pointer to a supported M7 type
// (*any, *bool, *int..., *uint..., *float32/64, *string, *[]byte,
// *[]any, *map[string]any). If the input is at EOF, Decode returns
// csvEOF-analogous EOF ("EOF") and does not modify e.
func (dec *Decoder) Decode(e any) error {
	gobEnsureReg()
	dec.err = nil
	dec.valBuf = nil
	if e == nil {
		dec.decodeIgnored()
		return dec.err
	}
	switch t := e.(type) {
	case *any:
		dec.decodeAny(t)
	case *bool:
		buf := dec.checkSingle("*bool", 1)
		if buf != nil {
			*t = buf.uint() != 0
		}
	case *int:
		dec.decodeIntPtr(t, 64, "*int")
	case *int8:
		dec.decodeIntPtr8(t, "*int8")
	case *int16:
		dec.decodeIntPtr16(t, "*int16")
	case *int32:
		dec.decodeIntPtr32(t, "*int32")
	case *int64:
		dec.decodeIntPtr64(t, "*int64")
	case *uint:
		dec.decodeUintPtr(t, 64, "*uint")
	case *uint8:
		dec.decodeUintPtr8(t, "*uint8")
	case *uint16:
		dec.decodeUintPtr16(t, "*uint16")
	case *uint32:
		dec.decodeUintPtr32(t, "*uint32")
	case *uint64:
		dec.decodeUintPtr64(t, "*uint64")
	case *uintptr:
		var u uint
		dec.decodeUintPtr(&u, 64, "*uintptr")
		if dec.err == nil {
			*t = uintptr(u)
		}
	case *float32:
		buf := dec.checkSingle("*float32", 4)
		if buf != nil {
			v := gobFloat64FromBits(gobReverseBytes64(buf.uint()))
			if dec.err == nil {
				av := v
				if av < 0 {
					av = -av
				}
				if av > 3.4028234663852886e+38 && av != gobPosInf {
					dec.fail("value for \"*float32\" out of range")
				} else {
					*t = float32(v)
				}
			}
		}
	case *float64:
		buf := dec.checkSingle("*float64", 4)
		if buf != nil {
			*t = gobFloat64FromBits(gobReverseBytes64(buf.uint()))
		}
	case *string:
		buf := dec.checkSingle("*string", 6)
		if buf != nil {
			*t = dec.decodeString(buf)
		}
	case *[]byte:
		buf := dec.checkSingle("*[]uint8", 5)
		if buf != nil {
			*t = dec.decodeBytes(buf)
		}
	case *[]any:
		dec.decodeSliceAnyPtr(t)
	case *map[string]any:
		dec.decodeMapStringAnyPtr(t)
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64, string, []byte, []any, map[string]any:
		dec.fail("gob: attempt to decode into a non-pointer")
	default:
		dec.fail("gob: decode target type not in M7 scope (needs reflect, P3-M8)")
	}
	return dec.err
}

// decodeString decodes a length-delimited string (no field delta — the
// caller consumed it).
func (dec *Decoder) decodeString(buf *gobDecBuf) string {
	n := buf.uint()
	if dec.err != nil {
		return ""
	}
	if n > uint64(buf.rest()) {
		dec.fail("gob: string length exceeds input")
		return ""
	}
	return string(buf.bytes(int(n)))
}

// decodeBytes decodes a length-delimited byte string; empty decodes to
// nil (gc parity, verified by probe).
func (dec *Decoder) decodeBytes(buf *gobDecBuf) []byte {
	n := buf.uint()
	if dec.err != nil {
		return nil
	}
	if n > uint64(buf.rest()) {
		dec.fail("gob: byte string length exceeds input")
		return nil
	}
	if n == 0 {
		return nil
	}
	b := make([]byte, n)
	copy(b, buf.bytes(int(n)))
	return b
}

func (dec *Decoder) decodeIntPtr(t *int, width int, localName string) {
	buf := dec.checkSingle(localName, 2)
	if buf == nil {
		return
	}
	x := buf.int()
	if dec.err == nil {
		*t = int(x)
	}
}

func (dec *Decoder) decodeIntPtr8(t *int8, localName string) {
	buf := dec.checkSingle(localName, 2)
	if buf == nil {
		return
	}
	x := buf.int()
	if dec.err != nil {
		return
	}
	if x < -128 || x > 127 {
		dec.fail("value for \"*int8\" out of range")
		return
	}
	*t = int8(x)
}

func (dec *Decoder) decodeIntPtr16(t *int16, localName string) {
	buf := dec.checkSingle(localName, 2)
	if buf == nil {
		return
	}
	x := buf.int()
	if dec.err != nil {
		return
	}
	if x < -32768 || x > 32767 {
		dec.fail("value for \"*int16\" out of range")
		return
	}
	*t = int16(x)
}

func (dec *Decoder) decodeIntPtr32(t *int32, localName string) {
	buf := dec.checkSingle(localName, 2)
	if buf == nil {
		return
	}
	x := buf.int()
	if dec.err != nil {
		return
	}
	if x < -2147483648 || x > 2147483647 {
		dec.fail("value for \"*int32\" out of range")
		return
	}
	*t = int32(x)
}

func (dec *Decoder) decodeIntPtr64(t *int64, localName string) {
	buf := dec.checkSingle(localName, 2)
	if buf == nil {
		return
	}
	x := buf.int()
	if dec.err == nil {
		*t = x
	}
}

func (dec *Decoder) decodeUintPtr(t *uint, width int, localName string) {
	buf := dec.checkSingle(localName, 3)
	if buf == nil {
		return
	}
	x := buf.uint()
	if dec.err == nil {
		*t = uint(x)
	}
}

func (dec *Decoder) decodeUintPtr8(t *uint8, localName string) {
	buf := dec.checkSingle(localName, 3)
	if buf == nil {
		return
	}
	x := buf.uint()
	if dec.err != nil {
		return
	}
	if x > 255 {
		dec.fail("value for \"*uint8\" out of range")
		return
	}
	*t = uint8(x)
}

func (dec *Decoder) decodeUintPtr16(t *uint16, localName string) {
	buf := dec.checkSingle(localName, 3)
	if buf == nil {
		return
	}
	x := buf.uint()
	if dec.err != nil {
		return
	}
	if x > 65535 {
		dec.fail("value for \"*uint16\" out of range")
		return
	}
	*t = uint16(x)
}

func (dec *Decoder) decodeUintPtr32(t *uint32, localName string) {
	buf := dec.checkSingle(localName, 3)
	if buf == nil {
		return
	}
	x := buf.uint()
	if dec.err != nil {
		return
	}
	if x > 4294967295 {
		dec.fail("value for \"*uint32\" out of range")
		return
	}
	*t = uint32(x)
}

func (dec *Decoder) decodeUintPtr64(t *uint64, localName string) {
	buf := dec.checkSingle(localName, 3)
	if buf == nil {
		return
	}
	x := buf.uint()
	if dec.err == nil {
		*t = x
	}
}

// decodeAny decodes into *any: only remote interface values are
// accepted (gc parity — concrete remote types fail with the interface
// error, verified by probe).
func (dec *Decoder) decodeAny(t *any) {
	id := dec.typeSeq(false)
	if dec.err != nil {
		return
	}
	if id != 8 {
		dec.fail("gob: local interface type *interface {} can only be decoded from remote interface type; received concrete type " + dec.remoteTypeString(id))
		return
	}
	buf := dec.valBuf
	if buf.uint() != 0 {
		dec.fail("gob: decode: corrupted data: non-zero delta for singleton")
		return
	}
	*t = dec.decodeInterfaceBody(buf)
}

// decodeInterfaceBody decodes an interface value body (no field delta;
// the caller consumed it): [namelen][name][typeid][vcount][value].
func (dec *Decoder) decodeInterfaceBody(buf *gobDecBuf) any {
	nr := buf.uint()
	if dec.err != nil {
		return nil
	}
	if nr > 1<<31 {
		dec.fail("gob: invalid type name length")
		return nil
	}
	if nr > uint64(buf.rest()) {
		dec.fail("gob: invalid type name length: exceeds input size")
		return nil
	}
	name := string(buf.bytes(int(nr)))
	if dec.err != nil {
		return nil
	}
	if len(name) == 0 {
		return nil
	}
	if len(name) > 1024 {
		dec.fail("gob: name too long")
		return nil
	}
	entry, ok := gobNameToKind[name]
	if !ok {
		dec.fail("gob: name not registered for interface: " + gobQuote(name))
		return nil
	}
	cid := buf.int()
	if dec.err != nil {
		return nil
	}
	if cid < 0 {
		dec.fail("gob: M7: unexpected type definition inside value")
		return nil
	}
	buf.uint() // delimited value count; ignored like gc
	if dec.err != nil {
		return nil
	}
	return dec.decodeConcrete(entry, name, cid, buf)
}

// decodeConcrete decodes the concrete value of an interface body.
func (dec *Decoder) decodeConcrete(entry gobRegEntry, localName string, cid int64, buf *gobDecBuf) any {
	switch entry.class {
	case gobClassBool, gobClassInt, gobClassUint, gobClassFloat, gobClassString, gobClassBytes:
		if cid != gobClassTypeID(entry.class) {
			dec.fail("gob: decoding into local type " + localName + ", received remote type " + dec.remoteTypeString(cid))
			return nil
		}
	case gobClassSliceAny:
		if dec.wire[int(cid)] != 1 {
			dec.fail("gob: decoding into local type " + localName + ", received remote type " + dec.remoteTypeString(cid))
			return nil
		}
	case gobClassMapStringAny:
		if dec.wire[int(cid)] != 2 {
			dec.fail("gob: decoding into local type " + localName + ", received remote type " + dec.remoteTypeString(cid))
			return nil
		}
	default:
		dec.fail("gob: M7: unsupported interface concrete type")
		return nil
	}
	if buf.uint() != 0 {
		dec.fail("gob: decode: corrupted data: non-zero delta for singleton")
		return nil
	}
	switch entry.class {
	case gobClassBool:
		return buf.uint() != 0
	case gobClassInt:
		x := buf.int()
		if dec.err != nil {
			return nil
		}
		return gobIntOfWidth(x, entry.width)
	case gobClassUint:
		x := buf.uint()
		if dec.err != nil {
			return nil
		}
		return gobUintOfWidth(x, entry.width)
	case gobClassFloat:
		v := gobFloat64FromBits(gobReverseBytes64(buf.uint()))
		if dec.err != nil {
			return nil
		}
		if entry.width == 32 {
			return float32(v)
		}
		return v
	case gobClassString:
		return dec.decodeString(buf)
	case gobClassBytes:
		return dec.decodeBytes(buf)
	case gobClassSliceAny:
		return dec.decodeSliceAny(buf)
	case gobClassMapStringAny:
		return dec.decodeMapStringAny(buf)
	}
	return nil
}

// gobIntOfWidth converts to the named int width (no overflow check:
// interface concrete values are always in range by construction).
func gobIntOfWidth(x int64, width int) any {
	switch width {
	case 8:
		return int8(x)
	case 16:
		return int16(x)
	case 32:
		return int32(x)
	}
	return int(x)
}

// gobUintOfWidth converts to the named uint width.
func gobUintOfWidth(x uint64, width int) any {
	switch width {
	case 8:
		return uint8(x)
	case 16:
		return uint16(x)
	case 32:
		return uint32(x)
	}
	return uint(x)
}

// decodeSliceAny decodes a []any value body: [count][interface...].
// Empty decodes to nil (gc parity, verified by probe).
func (dec *Decoder) decodeSliceAny(buf *gobDecBuf) []any {
	n := buf.uint()
	if dec.err != nil {
		return nil
	}
	var s []any
	for i := uint64(0); i < n; i++ {
		v := dec.decodeInterfaceBody(buf)
		if dec.err != nil {
			return nil
		}
		s = append(s, v)
	}
	return s
}

// decodeMapStringAny decodes a map[string]any value body:
// [count]([key][interface])* . Empty decodes to an empty non-nil map
// (gc parity, verified by probe).
func (dec *Decoder) decodeMapStringAny(buf *gobDecBuf) map[string]any {
	n := buf.uint()
	if dec.err != nil {
		return nil
	}
	m := make(map[string]any)
	for i := uint64(0); i < n; i++ {
		k := dec.decodeString(buf)
		if dec.err != nil {
			return nil
		}
		v := dec.decodeInterfaceBody(buf)
		if dec.err != nil {
			return nil
		}
		m[k] = v
	}
	return m
}

// decodeSliceAnyPtr decodes a top-level []any into *[]any.
func (dec *Decoder) decodeSliceAnyPtr(t *[]any) {
	id := dec.typeSeq(false)
	if dec.err != nil {
		return
	}
	if dec.wire[int(id)] != 1 {
		dec.fail("gob: decoding into local type *[]interface {}, received remote type " + dec.remoteTypeString(id))
		return
	}
	buf := dec.valBuf
	if buf.uint() != 0 {
		dec.fail("gob: decode: corrupted data: non-zero delta for singleton")
		return
	}
	*t = dec.decodeSliceAny(buf)
}

// decodeMapStringAnyPtr decodes a top-level map[string]any into
// *map[string]any.
func (dec *Decoder) decodeMapStringAnyPtr(t *map[string]any) {
	id := dec.typeSeq(false)
	if dec.err != nil {
		return
	}
	if dec.wire[int(id)] != 2 {
		dec.fail("gob: decoding into local type *map[string]interface {}, received remote type " + dec.remoteTypeString(id))
		return
	}
	buf := dec.valBuf
	if buf.uint() != 0 {
		dec.fail("gob: decode: corrupted data: non-zero delta for singleton")
		return
	}
	*t = dec.decodeMapStringAny(buf)
}

// decodeIgnored decodes and discards one value (Decode(nil)).
func (dec *Decoder) decodeIgnored() {
	id := dec.typeSeq(false)
	if dec.err != nil {
		return
	}
	dec.skipValue(id, dec.valBuf)
}

// skipValue discards one value of the given remote type id.
func (dec *Decoder) skipValue(id int64, buf *gobDecBuf) {
	if buf.uint() != 0 {
		dec.fail("gob: decode: corrupted data: non-zero delta for singleton")
		return
	}
	switch id {
	case 1, 2, 3, 4:
		buf.uint()
	case 5, 6:
		n := buf.uint()
		if dec.err != nil {
			return
		}
		buf.bytes(int(n))
	case 8:
		dec.skipInterface(buf)
	default:
		switch dec.wire[int(id)] {
		case 1: // []any
			n := buf.uint()
			if dec.err != nil {
				return
			}
			for i := uint64(0); i < n; i++ {
				dec.skipInterface(buf)
				if dec.err != nil {
					return
				}
			}
		case 2: // map[string]any
			n := buf.uint()
			if dec.err != nil {
				return
			}
			for i := uint64(0); i < n; i++ {
				kn := buf.uint()
				if dec.err != nil {
					return
				}
				buf.bytes(int(kn))
				if dec.err != nil {
					return
				}
				dec.skipInterface(buf)
				if dec.err != nil {
					return
				}
			}
		default:
			dec.fail("gob: M7: cannot skip value of unknown type")
		}
	}
}

// skipInterface discards one interface value body.
func (dec *Decoder) skipInterface(buf *gobDecBuf) {
	nr := buf.uint()
	if dec.err != nil {
		return
	}
	buf.bytes(int(nr))
	if dec.err != nil {
		return
	}
	cid := buf.int()
	if dec.err != nil {
		return
	}
	if cid < 0 {
		dec.fail("gob: M7: unexpected type definition inside value")
		return
	}
	vc := buf.uint()
	if dec.err != nil {
		return
	}
	if vc > uint64(buf.rest()) {
		dec.fail("unexpected EOF")
		return
	}
	sub := &gobDecBuf{dec: dec, data: buf.data[buf.off : buf.off+int(vc)]}
	dec.skipValue(cid, sub)
	if dec.err != nil {
		return
	}
	buf.off += int(vc)
}
`
