// Retirement: see ../../docs/arch/synth-retirement-tracker.md#nettextproto for this package's root cause and retires-when trigger (net/textproto).
package main

// P3-M9 (tier-4: net): synthetic `net/textproto` package (pure parts).
//
// Real-Go source: adapted from gc go1.27.1 ($GOROOT/src/net/textproto/header.go,
// reader.go, textproto.go). Ports MIMEHeader, CanonicalMIMEHeaderKey,
// TrimString and their unexported helpers, plus the full Reader type
// (ReadLine, ReadContinuedLine, ReadMIMEHeader and unexported helpers,
// including readMIMEHeader which net/http links via //go:linkname).
// Writer/Conn/Pipeline/DotReader are NOT ported (re-test when needed).
//
// Adaptations for the v1 frontend (no multi-result functions, WS D):
// canonicalMIMEHeaderKey (_ string, ok bool) becomes
// __canonicalMIMEHeaderKey returning canonPair; call sites use the __-form.
// The sync.Once-guarded commonHeader optimization is omitted (pure fast
// path; canonicalization is complete without it).
//
// Only frontend/v1-supported constructs (no generics, no reflect, no unsafe).
const syntheticTextprotoSource = `// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package textproto

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
)

// A MIMEHeader represents a MIME-style header mapping
// keys to sets of values.
type MIMEHeader map[string][]string

// Add adds the key, value pair to the header.
// It appends to any existing values associated with key.
func (h MIMEHeader) Add(key, value string) {
	key = CanonicalMIMEHeaderKey(key)
	h[key] = append(h[key], value)
}

// Set sets the header entries associated with key to
// the single element value. It replaces any existing
// values associated with key.
func (h MIMEHeader) Set(key, value string) {
	h[CanonicalMIMEHeaderKey(key)] = []string{value}
}

// Get gets the first value associated with the given key.
// It is case insensitive; [CanonicalMIMEHeaderKey] is used
// to canonicalize the provided key.
// If there are no values associated with the key, Get returns "".
// To use non-canonical keys, access the map directly.
func (h MIMEHeader) Get(key string) string {
	if h == nil {
		return ""
	}
	v := h[CanonicalMIMEHeaderKey(key)]
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// Values returns all values associated with the given key.
// It is case insensitive; [CanonicalMIMEHeaderKey] is
// used to canonicalize the provided key. To use non-canonical
// keys, access the map directly.
// The returned slice is not a copy.
func (h MIMEHeader) Values(key string) []string {
	if h == nil {
		return nil
	}
	return h[CanonicalMIMEHeaderKey(key)]
}

// Del deletes the values associated with key.
func (h MIMEHeader) Del(key string) {
	delete(h, CanonicalMIMEHeaderKey(key))
}

// canonPair is the single-result form of canonicalMIMEHeaderKey.
type canonPair struct {
	s  string
	ok bool
}

// CanonicalMIMEHeaderKey returns the canonical format of the
// MIME header key s. The canonicalization converts the first
// letter and any letter following a hyphen to upper case;
// the rest are converted to lower case. For example, the
// canonical key for "accept-encoding" is "Accept-Encoding".
// MIME header keys are assumed to be ASCII only.
// If s contains a space or invalid header field bytes, it is
// returned without modifications.
func CanonicalMIMEHeaderKey(s string) string {
	// Quick check for canonical encoding.
	upper := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !validHeaderFieldByte(c) {
			return s
		}
		if upper && 'a' <= c && c <= 'z' {
			__tmpC := __canonicalMIMEHeaderKey([]byte(s))
			s, _ = __tmpC.s, __tmpC.ok
			return s
		}
		if !upper && 'A' <= c && c <= 'Z' {
			__tmpC := __canonicalMIMEHeaderKey([]byte(s))
			s, _ = __tmpC.s, __tmpC.ok
			return s
		}
		upper = c == '-'
	}
	return s
}

const toLower = 'a' - 'A'

func __canonicalMIMEHeaderKey(a []byte) canonPair {
	if len(a) == 0 {
		return canonPair{"", false}
	}

	// See if a looks like a header key. If not, return it unchanged.
	noCanon := false
	for _, c := range a {
		if validHeaderFieldByte(c) {
			continue
		}
		// Don't canonicalize.
		if c == ' ' {
			// We accept invalid headers with a space before the
			// colon, but must not canonicalize them.
			// See https://go.dev/issue/34540.
			noCanon = true
			continue
		}
		return canonPair{string(a), false}
	}
	if noCanon {
		return canonPair{string(a), true}
	}

	upper := true
	for i, c := range a {
		// Canonicalize: first letter upper case
		// and upper case after each dash.
		// (Host, User-Agent, If-Modified-Since).
		// MIME headers are ASCII only, so no Unicode issues.
		if upper && 'a' <= c && c <= 'z' {
			c -= toLower
		} else if !upper && 'A' <= c && c <= 'Z' {
			c += toLower
		}
		a[i] = c
		upper = c == '-' // for next time
	}
	// NOTE: the commonHeader sync.Once optimization is omitted (M9);
	// the canonical form above is complete without it.
	return canonPair{string(a), true}
}

func validHeaderFieldByte(c byte) bool {
	// mask is a 128-bit bitmap with 1s for allowed bytes,
	// so that the byte c can be tested with a shift and an and.
	// If c >= 128, then 1<<c and 1<<(c-64) will both be zero,
	// and this function will return false.
	const mask = 0 |
		(1<<(10)-1)<<'0' |
		(1<<(26)-1)<<'a' |
		(1<<(26)-1)<<'A' |
		1<<'!' |
		1<<'#' |
		1<<'$' |
		1<<'%' |
		1<<'&' |
		1<<'\'' |
		1<<'*' |
		1<<'+' |
		1<<'-' |
		1<<'.' |
		1<<'^' |
		1<<'_' |
		1<<0x60 | // backtick
		1<<'|' |
		1<<'~'
	return ((uint64(1)<<c)&(mask&(1<<64-1)) |
		(uint64(1)<<(c-64))&(mask>>64)) != 0
}

// TrimString returns s without leading and trailing ASCII space.
func TrimString(s string) string {
	for len(s) > 0 && isASCIISpace(s[0]) {
		s = s[1:]
	}
	for len(s) > 0 && isASCIISpace(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	return s
}

func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// errMessageTooLarge is a sentinel error for the messageTooLarge limit.
var errMessageTooLarge = errors.New("message too large")

// A Reader implements convenience methods for reading requests
// or responses from a text protocol network connection.
type Reader struct {
	R *bufio.Reader
	// Note: gc's dot *dotReader field is omitted; the dot-reader
	// protocol (DotReader) is not needed by net/http and is not ported.
}

// NewReader returns a new Reader reading from r.
func NewReader(r *bufio.Reader) *Reader {
	return &Reader{R: r}
}

// ReadLine reads a single line from r,
// eliding the final newline or carriage-return newline from the returned string.
func (r *Reader) ReadLine() (string, error) {
	line, err := r.readLineSlice(math.MaxInt64)
	return string(line), err
}

// ReadLineBytes is like ReadLine but returns the line as a []byte.
func (r *Reader) ReadLineBytes() ([]byte, error) {
	line, err := r.readLineSlice(math.MaxInt64)
	if line != nil {
		line = bytes.Clone(line)
	}
	return line, err
}

// readLineSlice reads a single line from the Reader,
// eliding the final newline or carriage-return newline.
func (r *Reader) readLineSlice(lim int64) ([]byte, error) {
	r.closeDot()
	var line []byte
	for {
		l, more, err := r.R.ReadLine()
		if err != nil {
			return nil, err
		}
		if lim >= 0 && int64(len(line))+int64(len(l)) > lim {
			return nil, errMessageTooLarge
		}
		// Avoid the copy if the first call produced a full line.
		if line == nil && !more {
			return l, nil
		}
		line = append(line, l...)
		if !more {
			break
		}
	}
	return line, nil
}

// ReadContinuedLine reads a possibly continued line from r,
// eliding the final trailing ASCII white space.
// Lines after the first are considered continuations if they
// begin with a space or tab character. In the returned data,
// continuation lines are separated from the previous line
// only by a single space: the newline and leading white space
// are removed.
func (r *Reader) ReadContinuedLine() (string, error) {
	line, err := r.readContinuedLineSlice(math.MaxInt64, noValidation)
	return string(line), err
}

func trim(s []byte) []byte {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	s = s[i:]
	n := len(s) - 1
	for n >= 0 && (s[n] == ' ' || s[n] == '\t') {
		n--
	}
	return s[:n+1]
}

// ReadContinuedLineBytes is like ReadContinuedLine but
// returns a []byte instead of a string.
func (r *Reader) ReadContinuedLineBytes() ([]byte, error) {
	line, err := r.readContinuedLineSlice(math.MaxInt64, noValidation)
	if line != nil {
		line = bytes.Clone(line)
	}
	return line, err
}

func (r *Reader) readContinuedLineSlice(lim int64, validateFirstLine func([]byte) error) ([]byte, error) {
	// Read the first line.
	line, err := r.readLineSlice(lim)
	if err != nil {
		return nil, err
	}
	if len(line) == 0 { // blank line - no continuation
		return line, err
	}
	if err := validateFirstLine(line); err != nil {
		return nil, err
	}

	// Optimistically assume that we have started to buffer the next line
	// and it starts with a letter (not a space), so we can avoid copying
	// that next line into the accidental buffer.
	if r.R.Buffered() > 1 {
		peek, _ := r.R.Peek(1)
		if len(peek) > 0 && !isASCIISpace(peek[0]) {
			return trim(line), nil
		}
	}

	// Read continuation lines.
	for r.skipSpace() > 0 {
		line = append(line, ' ')
		next, err := r.readLineSlice(lim)
		if err != nil {
			break
		}
		line = append(line, next...)
	}
	return trim(line), nil
}

// skipSpace skips spaces in the reader and returns the number of bytes skipped.
func (r *Reader) skipSpace() int {
	n := 0
	for {
		c, err := r.R.ReadByte()
		if err != nil {
			break
		}
		if c != ' ' && c != '\t' {
			r.R.UnreadByte()
			break
		}
		n++
	}
	return n
}

// ReadMIMEHeader reads a MIME-style header from r.
// The header is a sequence of possibly continued Key: Value lines
// ending in a blank line.
// The returned map m maps CanonicalMIMEHeaderKey(key) to a
// sequence of values in the order encountered.
func (r *Reader) ReadMIMEHeader() (MIMEHeader, error) {
	return readMIMEHeader(r, math.MaxInt64, math.MaxInt64)
}

// readMIMEHeader is a real-nethttp addition: net/http links to this
// unexported symbol via //go:linkname (net/http/request.go). It is the
// provider; net/http declares the bodyless consumer stub.
func readMIMEHeader(r *Reader, maxMemory, maxHeaders int64) (MIMEHeader, error) {
	// Avoid lots of small slice allocations later by allocating one
	// large one ahead of time which we'll cut up into smaller
	// slices. If this isn't big enough later, we allocate small ones.
	var strs []string
	hint := r.upcomingHeaderKeys()
	if hint > 0 {
		if hint > 1000 {
			hint = 1000 // set a cap to avoid overallocation
		}
		strs = make([]string, hint)
	}

	m := make(MIMEHeader, hint)

	// Account for 400 bytes of overhead for the MIMEHeader, plus 200 bytes per entry.
	maxMemory -= 400
	const mapEntryOverhead = 200

	// The first line cannot start with a leading space.
	if buf, err := r.R.Peek(1); err == nil && (buf[0] == ' ' || buf[0] == '\t') {
		const errorLimit = 80 // arbitrary limit on how much of the line we'll quote
		line, err := r.readLineSlice(errorLimit)
		if err != nil {
			return m, err
		}
		return m, ProtocolError(fmt.Sprintf("malformed MIME header initial line: %q", line))
	}

	for {
		kv, err := r.readContinuedLineSlice(maxMemory, mustHaveFieldNameColon)
		if len(kv) == 0 {
			return m, err
		}

		// Key ends at first colon.
		k, v, ok := bytes.Cut(kv, colon)
		if !ok {
			return m, ProtocolError(fmt.Sprintf("malformed MIME header line: %q", kv))
		}
		// real-nethttp adaptation: gc calls the unexported
		// canonicalMIMEHeaderKey; this synthetic package exposes the
		// same logic as __canonicalMIMEHeaderKey returning a pair.
		__cp := __canonicalMIMEHeaderKey(k)
		key, ok := __cp.s, __cp.ok
		if !ok {
			return m, ProtocolError(fmt.Sprintf("malformed MIME header line: %q", kv))
		}
		for _, c := range v {
			if !validHeaderValueByte(c) {
				return m, ProtocolError(fmt.Sprintf("malformed MIME header line: %q", kv))
			}
		}

		maxHeaders--
		if maxHeaders < 0 {
			return nil, errMessageTooLarge
		}

		// Skip initial spaces in value.
		value := string(bytes.TrimLeft(v, " \t"))

		vv := m[key]
		if vv == nil {
			maxMemory -= int64(len(key))
			maxMemory -= mapEntryOverhead
		}
		maxMemory -= int64(len(value))
		if maxMemory < 0 {
			return m, errMessageTooLarge
		}
		if vv == nil && len(strs) > 0 {
			// More than likely this will be a single-element key.
			// Most headers aren't multi-valued.
			// Set the capacity on strs[0] to 1, so any future append
			// won't extend the slice into the other strings.
			vv, strs = strs[:1:1], strs[1:]
			vv[0] = value
			m[key] = vv
		} else {
			m[key] = append(vv, value)
		}

		if err != nil {
			return m, err
		}
	}
}

var colon = []byte(":")

// noValidation is a no-op validation func for readContinuedLineSlice
// that permits any lines.
func noValidation(_ []byte) error { return nil }

// mustHaveFieldNameColon ensures that the line being validated is a
// header line ending with a colon.
func mustHaveFieldNameColon(line []byte) error {
	if bytes.IndexByte(line, ':') < 0 {
		return ProtocolError(fmt.Sprintf("malformed MIME header line: %q", string(line)))
	}
	return nil
}

var nl = []byte("\n")

// upcomingHeaderKeys returns a hint of the number of keys that will be in this header.
// It returns 0 if the hint cannot be determined.
func (r *Reader) upcomingHeaderKeys() int {
	// Try to determine the number of header keys that follow by
	// peeking at the buffered data. If we can't peek, return 0.
	buf, _ := r.R.Peek(r.R.Buffered())
	n := bytes.Count(buf, nl)
	if n == 0 {
		return 0
	}

	// Multiple lines may be continuations, so go through and count lines
	// that start with a non-space character.
	count := 0
	for len(buf) > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		// Don't count lines starting with a space as they are continuations.
		if i > 0 && buf[0] != ' ' && buf[0] != '\t' {
			count++
		}
		buf = buf[i+1:]
	}
	return count
}

// ProtocolError describes a protocol violation.
type ProtocolError string

func (pe ProtocolError) Error() string {
	return string(pe)
}

// isASCIILetter reports whether c is an ASCII letter.
func isASCIILetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// validHeaderValueByte reports whether c is a valid byte in a header
// field value. RFC 7230 says:
//
//	field-content  = field-vchar [ 1*( SP / HTAB ) field-vchar ]
//
//	field-vchar    = VCHAR / obs-text
//
//	obs-text       = %x80-FF
//
// RFC 5234 says:
//
//	HTAB           =  %x09
//
//	SP             =  %x20
//
//	VCHAR          =  %x21-7E
func validHeaderValueByte(c byte) bool {
	// mask is a 128-bit bitmap with 1s for allowed bytes,
	// so that the byte c can be tested with a shift and an and.
	// If c >= 128, then 1<<c and 1<<(c-64) will both be zero.
	// Since this is the obs-text range, we invert the mask to
	// create a bitmap with 1s for disallowed bytes.
	const mask = 0 |
		(1<<(0x7f-0x21)-1)<<0x21 | // VCHAR: %x21-7E
		1<<0x20 | // SP: %x20
		1<<0x09 // HTAB: %x09
	return ((uint64(1)<<c)&^(mask&(1<<64-1)) |
		(uint64(1)<<(c-64))&^(mask>>64)) == 0
}

// closeDot is a no-op placeholder; the dot-reader protocol is not
// needed by net/http (real-nethttp omits DotReader).
func (r *Reader) closeDot() {}
`
