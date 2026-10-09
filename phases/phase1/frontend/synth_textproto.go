package main

// P3-M9 (tier-4: net): synthetic `net/textproto` package (pure parts).
//
// Real-Go source: adapted from gc go1.27.1 ($GOROOT/src/net/textproto/header.go,
// reader.go, textproto.go). Ports MIMEHeader, CanonicalMIMEHeaderKey,
// TrimString and their unexported helpers. Reader/Writer/Conn/Pipeline are
// NOT ported (need bufio + multi-result I/O; re-test when synthetic bufio
// lands and WS D multi-result dispatch is available).
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
`
