package main

// P3-M9 (tier-4: net): synthetic `net/url` package.
//
// Real-Go source: adapted from gc go1.27.1 ($GOROOT/src/net/url/url.go).
// Imports (bytes, errors, fmt, net/netip, slices, strconv, strings) all
// resolve to synthetic packages.
//
// Adaptations for the v1 frontend (no multi-result functions, WS D):
// every multi-result function F reachable from the corpus surface is
// renamed to __F returning a pair/triple struct (field order = gc result
// order); all `return a, b[, c]` become `return pair{a, b[, c]}`;
// same-package call sites use the __-form explicitly. Public API keeps
// exact gc signatures with unreachable panic bodies — the Appendix-M
// desugar (urlMultiResultFuncs in mrdesugar_m5.go) redirects call sites
// to the __-helpers.
//   - strings.Cut is not in M7 scope: local __cut triple.
//   - internal/godebug: local shim pinning default values.
//   - path.Join: local __urlPathJoin (lexical clean, no FS).
//   - the badSetPath linkname is dropped (external back-compat hack).
//
// Cross-package multi-result calls (strconv.Atoi/ParseUint,
// netip.ParseAddr) desugar via the existing tables (loud on failure).
// Known D4-stub divergence: fmt.Errorf %w flattens (no wrap chain); the
// corpus pins error strings, not Is-chains, for the one %w site.
//
// Only frontend/v1-supported constructs (no generics, no reflect, no
// unsafe).
const syntheticNeturlSource = `// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate go run gen_encoding_table.go

// Package url parses URLs and implements query escaping.
//
// See RFC 3986. This package generally follows RFC 3986, except where
// it deviates for compatibility reasons.
// RFC 6874 followed for IPv6 zone literals.
package url

// When sending changes, first  search old issues for history on decisions.
// Unit tests should also contain references to issue numbers with details.

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)


// godebugSetting is a minimal stand-in for internal/godebug.Setting (no
// synthetic godebug package in M9): the url port pins the default (empty)
// values. Re-test: godebug intrinsic.
type godebugSetting struct{ name string }

func (s *godebugSetting) Value() string { return "" }
func (s *godebugSetting) IncNonDefault() {}

// cutTriple is the single-result form of strings.Cut (not in M7 scope).
type cutTriple struct {
	before, after string
	found          bool
}

// __cut is strings.Cut (gc's strings.Cut is a 3-result function outside
// M7 scope, so the url port carries a local single-result copy).
func __cut(s, sep string) cutTriple {
	if i := strings.Index(s, sep); i >= 0 {
		return cutTriple{s[:i], s[i+len(sep):], true}
	}
	return cutTriple{s, "", false}
}

// __urlPathJoin is path.Join (no synthetic path package in M9): joins
// with "/" and cleans. Used only by URL.joinPath/JoinPath.
func __urlPathJoin(elem ...string) string {
	return __urlPathClean(strings.Join(elem, "/"))
}

// __urlPathClean is path.Clean (see Go spec): lexical cleaning only,
// no filesystem access.
func __urlPathClean(p string) string {
	if p == "" {
		return "."
	}
	rooted := p[0] == '/'
	n := len(p)
	out := make([]byte, 0, n)
	r, w, dotdot := 0, 0, 0
	if rooted {
		out = append(out, '/')
		r, w, dotdot = 1, 1, 1
	}
	for r < n {
		switch {
		case p[r] == '/':
			r++
		case p[r] == '.' && (r+1 == n || p[r+1] == '/'):
			r++
		case p[r] == '.' && p[r+1] == '.' && (r+2 == n || p[r+2] == '/'):
			r += 2
			if w > dotdot {
				w--
				for w > dotdot && out[w] != '/' {
					w--
				}
			} else if !rooted {
				if w > 0 {
					out = append(out, '/')
					w++
				}
				out = append(out, '.', '.')
				w += 2
				dotdot = w
			}
		default:
			if (rooted && w != 1) || (!rooted && w != 0) {
				out = append(out, '/')
				w++
			}
			for ; r < n && p[r] != '/'; r++ {
				out = append(out, p[r])
				w++
			}
		}
	}
	if w == 0 {
		return "."
	}
	return string(out[:w])
}

// Pair/triple structs for the Appendix-M __-helpers (single-result forms
// of the multi-result API). Field order matches gc result order.
type stringErrPair struct {
	s   string
	err error
}

type getSchemeTriple struct {
	scheme, path string
	err          error
}

type parseAuthorityTriple struct {
	user *Userinfo
	host string
	err  error
}

type parseURLPair struct {
	u   *URL
	err error
}

type parseQueryPair struct {
	v   Values
	err error
}

type passwordPair struct {
	s  string
	ok bool
}

type splitHostPortPair struct {
	host, port string
}

type joinPathPair struct {
	url *URL
	err error
}

// Public multi-result API: exact gc signatures with unreachable panic
// bodies (the Appendix-M desugar redirects every call site to the
// __-helpers; a direct call means the desugar did not fire).
func Parse(rawURL string) (*URL, error) {
	panic("url.Parse: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func ParseRequestURI(rawURL string) (*URL, error) {
	panic("url.ParseRequestURI: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func ParseQuery(query string) (Values, error) {
	panic("url.ParseQuery: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func (u *URL) MarshalBinary() (text []byte, err error) {
	panic("url.URL.MarshalBinary: not in M9 scope (re-test when WS D lands)")
}

func (u *URL) AppendBinary(b []byte) ([]byte, error) {
	panic("url.URL.AppendBinary: not in M9 scope (re-test when WS D lands)")
}

// __queryUnescape / __pathUnescape are the desugar targets for the
// public QueryUnescape / PathUnescape.
func __queryUnescape(s string) stringErrPair {
	return __unescape(s, encodeQueryComponent)
}

func __pathUnescape(s string) stringErrPair {
	return __unescape(s, encodePathSegment)
}

// __parseURLPub is the desugar target for the public Parse (full logic,
// single-result form).
func __parseURLPub(rawURL string) parseURLPair {
	// Cut off #frag
	__tmpCut := __cut(rawURL, "#")
	u, frag, _ := __tmpCut.before, __tmpCut.after, __tmpCut.found
	__tmpP := __parse(u, false)
	url, err := __tmpP.u, __tmpP.err
	if err != nil {
		return parseURLPair{nil, &Error{"parse", u, err}}
	}
	if frag == "" {
		return parseURLPair{url, nil}
	}
	if err = url.setFragment(frag); err != nil {
		return parseURLPair{nil, &Error{"parse", rawURL, err}}
	}
	return parseURLPair{url, nil}
}

// __parseRequestURIPub is the desugar target for the public ParseRequestURI.
func __parseRequestURIPub(rawURL string) parseURLPair {
	__tmpP := __parse(rawURL, true)
	url, err := __tmpP.u, __tmpP.err
	if err != nil {
		return parseURLPair{nil, &Error{"parse", rawURL, err}}
	}
	return parseURLPair{url, nil}
}

// __userinfoPassword is the desugar target for (*Userinfo).Password.
func __userinfoPassword(u *Userinfo) passwordPair {
	if u == nil {
		return passwordPair{"", false}
	}
	return passwordPair{u.password, u.passwordSet}
}

// __urlParsePub is the desugar target for (*URL).Parse.
func __urlParsePub(u *URL, ref string) parseURLPair {
	__tmpP2 := __parseURLPub(ref)
	refURL, err := __tmpP2.u, __tmpP2.err
	if err != nil {
		return parseURLPair{nil, err}
	}
	return parseURLPair{u.ResolveReference(refURL), nil}
}

// __joinPathPub is the desugar target for the public JoinPath.
func __joinPathPub(base string, elem ...string) stringErrPair {
	__tmpP3 := __parseURLPub(base)
	url, err := __tmpP3.u, __tmpP3.err
	if err != nil {
		return stringErrPair{"", err}
	}
	__tmpJP := __joinPath(url, elem...)
	res, err := __tmpJP.url, __tmpJP.err
	if err != nil {
		return stringErrPair{"", err}
	}
	return stringErrPair{res.String(), nil}
}
var urlstrictcolons = &godebugSetting{name: "urlstrictcolons"}

// Error reports an error and the operation and URL that caused it.
type Error struct {
	Op  string
	URL string
	Err error
}

func (e *Error) Unwrap() error { return e.Err }
func (e *Error) Error() string { return fmt.Sprintf("%s %q: %s", e.Op, e.URL, e.Err) }

func (e *Error) Timeout() bool {
	t, ok := e.Err.(interface {
		Timeout() bool
	})
	return ok && t.Timeout()
}

func (e *Error) Temporary() bool {
	t, ok := e.Err.(interface {
		Temporary() bool
	})
	return ok && t.Temporary()
}

const upperhex = "0123456789ABCDEF"

// P3-M10: the escape table is gc 1.27.1's generated encoding_table.go
// verbatim (the M9 drop carried only the hexChar rows, so url.Parse
// rejected '.' and every other legal host character).
var table = [256]encoding{
	'!':  encodeFragment | encodeZone | encodeHost,
	'"':  encodeZone | encodeHost,
	'$':  encodeFragment | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'&':  encodeFragment | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'\'': encodeZone | encodeHost,
	'(':  encodeFragment | encodeZone | encodeHost,
	')':  encodeFragment | encodeZone | encodeHost,
	'*':  encodeFragment | encodeZone | encodeHost,
	'+':  encodeFragment | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	',':  encodeFragment | encodeUserPassword | encodeZone | encodeHost | encodePath,
	'-':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'.':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'/':  encodeFragment | encodePath,
	'0':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'1':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'2':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'3':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'4':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'5':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'6':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'7':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'8':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'9':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	':':  encodeFragment | encodeZone | encodeHost | encodePathSegment | encodePath,
	';':  encodeFragment | encodeUserPassword | encodeZone | encodeHost | encodePath,
	'<':  encodeZone | encodeHost,
	'=':  encodeFragment | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'>':  encodeZone | encodeHost,
	'?':  encodeFragment,
	'@':  encodeFragment | encodePathSegment | encodePath,
	'A':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'B':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'C':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'D':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'E':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'F':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'G':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'H':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'I':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'J':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'K':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'L':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'M':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'N':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'O':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'P':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'Q':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'R':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'S':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'T':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'U':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'V':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'W':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'X':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'Y':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'Z':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'[':  encodeZone | encodeHost,
	']':  encodeZone | encodeHost,
	'_':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'a':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'b':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'c':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'd':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'e':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'f':  hexChar | encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'g':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'h':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'i':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'j':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'k':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'l':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'm':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'n':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'o':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'p':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'q':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'r':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	's':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	't':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'u':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'v':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'w':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'x':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'y':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'z':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
	'~':  encodeFragment | encodeQueryComponent | encodeUserPassword | encodeZone | encodeHost | encodePathSegment | encodePath,
}

func ishex(c byte) bool {
	return table[c]&hexChar != 0
}

// Precondition: ishex(c) is true.
func unhex(c byte) byte {
	return 9*(c>>6) + (c & 15)
}

type EscapeError string

func (e EscapeError) Error() string {
	return "invalid URL escape " + strconv.Quote(string(e))
}

type InvalidHostError string

func (e InvalidHostError) Error() string {
	return "invalid character " + strconv.Quote(string(e)) + " in host name"
}

// See the reference implementation in gen_encoding_table.go.
type encoding uint8

const (
	encodePath encoding = 1 << iota
	encodePathSegment
	encodeHost
	encodeZone
	encodeUserPassword
	encodeQueryComponent
	encodeFragment

	// hexChar is actually NOT an encoding mode, but there are only seven
	// encoding modes. We might as well abuse the otherwise unused most
	// significant bit in uint8 to indicate whether a character is
	// hexadecimal.
	hexChar
)

func shouldEscape(c byte, mode encoding) bool {
	return table[c]&mode == 0
}

// QueryUnescape does the inverse transformation of [QueryEscape],
// converting each 3-byte encoded substring of the form "%AB" into the
// hex-decoded byte 0xAB.
// It returns an error if any % is not followed by two hexadecimal
// digits.
func QueryUnescape(s string) (string, error) {
	panic("url.QueryUnescape: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

// PathUnescape does the inverse transformation of [PathEscape],
// converting each 3-byte encoded substring of the form "%AB" into the
// hex-decoded byte 0xAB. It returns an error if any % is not followed
// by two hexadecimal digits.
//
// PathUnescape is identical to [QueryUnescape] except that it does not
// unescape '+' to ' ' (space).
func PathUnescape(s string) (string, error) {
	panic("url.PathUnescape: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

// unescape unescapes a string; the mode specifies
// which section of the URL string is being unescaped.
func __unescape(s string, mode encoding) stringErrPair {
	// Count %, check that they're well-formed.
	n := 0
	hasPlus := false
	for i := 0; i < len(s); {
		switch s[i] {
		case '%':
			n++
			if i+2 >= len(s) || !ishex(s[i+1]) || !ishex(s[i+2]) {
				s = s[i:]
				if len(s) > 3 {
					s = s[:3]
				}
				return stringErrPair{"", EscapeError(s)}
			}
			// Per https://tools.ietf.org/html/rfc3986#page-21
			// in the host component %-encoding can only be used
			// for non-ASCII bytes.
			// But https://tools.ietf.org/html/rfc6874#section-2
			// introduces %25 being allowed to escape a percent sign
			// in IPv6 scoped-address literals. Yay.
			if mode == encodeHost && unhex(s[i+1]) < 8 && s[i:i+3] != "%25" {
				return stringErrPair{"", EscapeError(s[i : i+3])}
			}
			if mode == encodeZone {
				// RFC 6874 says basically "anything goes" for zone identifiers
				// and that even non-ASCII can be redundantly escaped,
				// but it seems prudent to restrict %-escaped bytes here to those
				// that are valid host name bytes in their unescaped form.
				// That is, you can use escaping in the zone identifier but not
				// to introduce bytes you couldn't just write directly.
				// But Windows puts spaces here! Yay.
				v := unhex(s[i+1])<<4 | unhex(s[i+2])
				if s[i:i+3] != "%25" && v != ' ' && shouldEscape(v, encodeHost) {
					return stringErrPair{"", EscapeError(s[i : i+3])}
				}
			}
			i += 3
		case '+':
			hasPlus = mode == encodeQueryComponent
			i++
		default:
			if (mode == encodeHost || mode == encodeZone) && s[i] < 0x80 && shouldEscape(s[i], mode) {
				return stringErrPair{"", InvalidHostError(s[i : i+1])}
			}
			i++
		}
	}

	if n == 0 && !hasPlus {
		return stringErrPair{s, nil}
	}

	var unescapedPlusSign byte
	switch mode {
	case encodeQueryComponent:
		unescapedPlusSign = ' '
	default:
		unescapedPlusSign = '+'
	}
	var t strings.Builder
	t.Grow(len(s) - 2*n)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '%':
			// In the loop above, we established that unhex's precondition is
			// fulfilled for both s[i+1] and s[i+2].
			t.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
		case '+':
			t.WriteByte(unescapedPlusSign)
		default:
			t.WriteByte(s[i])
		}
	}
	return stringErrPair{t.String(), nil}
}

// QueryEscape escapes the string so it can be safely placed
// inside a [URL] query.
func QueryEscape(s string) string {
	return escape(s, encodeQueryComponent)
}

// PathEscape escapes the string so it can be safely placed inside a [URL] path segment,
// replacing special characters (including /) with %XX sequences as needed.
func PathEscape(s string) string {
	return escape(s, encodePathSegment)
}

func escape(s string, mode encoding) string {
	spaceCount, hexCount := 0, 0
	for _, c := range []byte(s) {
		if shouldEscape(c, mode) {
			if c == ' ' && mode == encodeQueryComponent {
				spaceCount++
			} else {
				hexCount++
			}
		}
	}

	if spaceCount == 0 && hexCount == 0 {
		return s
	}

	var buf [64]byte
	var t []byte

	required := len(s) + 2*hexCount
	if required <= len(buf) {
		t = buf[:required]
	} else {
		t = make([]byte, required)
	}

	if hexCount == 0 {
		copy(t, s)
		for i := 0; i < len(s); i++ {
			if s[i] == ' ' {
				t[i] = '+'
			}
		}
		return string(t)
	}

	j := 0
	for _, c := range []byte(s) {
		switch {
		case c == ' ' && mode == encodeQueryComponent:
			t[j] = '+'
			j++
		case shouldEscape(c, mode):
			t[j] = '%'
			t[j+1] = upperhex[c>>4]
			t[j+2] = upperhex[c&15]
			j += 3
		default:
			t[j] = c
			j++
		}
	}
	return string(t)
}

// A URL represents a parsed URL (technically, a URI reference).
//
// The general form represented is:
//
//	[scheme:][//[userinfo@]host][/]path[?query][#fragment]
//
// URLs that do not start with a slash after the scheme are interpreted as:
//
//	scheme:opaque[?query][#fragment]
//
// The Host field contains the host and port subcomponents of the URL.
// When the port is present, it is separated from the host with a colon.
// When the host is an IPv6 address, it must be enclosed in square brackets:
// "[fe80::1]:80". The [net.JoinHostPort] function combines a host and port
// into a string suitable for the Host field, adding square brackets to
// the host when necessary.
//
// Note that the Path field is stored in decoded form: /%47%6f%2f becomes /Go/.
// A consequence is that it is impossible to tell which slashes in the Path were
// slashes in the raw URL and which were %2f. This distinction is rarely important,
// but when it is, the code should use the [URL.EscapedPath] method, which preserves
// the original encoding of Path. The Fragment field is also stored in decoded form,
// use [URL.EscapedFragment] to retrieve the original encoding.
//
// The [URL.String] method uses the [URL.EscapedPath] method to obtain the path.
type URL struct {
	Scheme   string
	Opaque   string    // encoded opaque data
	User     *Userinfo // username and password information
	Host     string    // "host" or "host:port" (see Hostname and Port methods)
	Path     string    // path (relative paths may omit leading slash)
	Fragment string    // fragment for references (without '#')

	// RawQuery contains the encoded query values, without the initial '?'.
	// Use URL.Query to decode the query.
	RawQuery string

	// RawPath is an optional field containing an encoded path hint.
	// See the EscapedPath method for more details.
	//
	// In general, code should call EscapedPath instead of reading RawPath.
	RawPath string

	// RawFragment is an optional field containing an encoded fragment hint.
	// See the EscapedFragment method for more details.
	//
	// In general, code should call EscapedFragment instead of reading RawFragment.
	RawFragment string

	// ForceQuery indicates whether the original URL contained a query ('?') character.
	// When set, the String method will include a trailing '?', even when RawQuery is empty.
	ForceQuery bool

	// OmitHost indicates the URL has an empty host (authority).
	// When set, the String method will not include the host when it is empty.
	OmitHost bool
}

// User returns a [Userinfo] containing the provided username
// and no password set.
func User(username string) *Userinfo {
	return &Userinfo{username, "", false}
}

// UserPassword returns a [Userinfo] containing the provided username
// and password.
//
// This functionality should only be used with legacy web sites.
// RFC 2396 warns that interpreting Userinfo this way
// “is NOT RECOMMENDED, because the passing of authentication
// information in clear text (such as URI) has proven to be a
// security risk in almost every case where it has been used.”
func UserPassword(username, password string) *Userinfo {
	return &Userinfo{username, password, true}
}

// The Userinfo type is an immutable encapsulation of username and
// password details for a [URL]. An existing Userinfo value is guaranteed
// to have a username set (potentially empty, as allowed by RFC 2396),
// and optionally a password.
type Userinfo struct {
	username    string
	password    string
	passwordSet bool
}

// Username returns the username.
func (u *Userinfo) Username() string {
	if u == nil {
		return ""
	}
	return u.username
}

// Password returns the password in case it is set, and whether it is set.
func (u *Userinfo) Password() (string, bool) {
	panic("url.Userinfo.Password: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

// String returns the encoded userinfo information in the standard form
// of "username[:password]".
func (u *Userinfo) String() string {
	if u == nil {
		return ""
	}
	s := escape(u.username, encodeUserPassword)
	if u.passwordSet {
		s += ":" + escape(u.password, encodeUserPassword)
	}
	return s
}

// Maybe rawURL is of the form scheme:path.
// (Scheme must be [a-zA-Z][a-zA-Z0-9+.-]*)
// If so, return scheme, path; else return "", rawURL.
func __getScheme(rawURL string) getSchemeTriple {
	for i := 0; i < len(rawURL); i++ {
		c := rawURL[i]
		switch {
		case 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z':
		// do nothing
		case '0' <= c && c <= '9' || c == '+' || c == '-' || c == '.':
			if i == 0 {
				return getSchemeTriple{"", rawURL, nil}
			}
		case c == ':':
			if i == 0 {
				return getSchemeTriple{"", "", errors.New("missing protocol scheme")}
			}
			return getSchemeTriple{rawURL[:i], rawURL[i+1:], nil}
		default:
			// we have encountered an invalid character,
			// so there is no valid scheme
			return getSchemeTriple{"", rawURL, nil}
		}
	}
	return getSchemeTriple{"", rawURL, nil}
}

// Parse parses a raw url into a [URL] structure.
//
// The url may be relative (a path, without a host) or absolute
// (starting with a scheme). Trying to parse a hostname and path
// without a scheme is invalid but may not necessarily return an
// error, due to parsing ambiguities.


// ParseRequestURI parses a raw url into a [URL] structure. It assumes that
// url was received in an HTTP request, so the url is interpreted
// only as an absolute URI or an absolute path.
// The string url is assumed not to have a #fragment suffix.
// (Web browsers strip #fragment before sending the URL to a web server.)


// parse parses a URL from a string in one of two contexts. If
// viaRequest is true, the URL is assumed to have arrived via an HTTP request,
// in which case only absolute URLs or path-absolute relative URLs are allowed.
// If viaRequest is false, all forms of relative URLs are allowed.
func __parse(rawURL string, viaRequest bool) parseURLPair {
	var rest string
	var err error

	if stringContainsCTLByte(rawURL) {
		return parseURLPair{nil, errors.New("net/url: invalid control character in URL")}
	}

	if rawURL == "" && viaRequest {
		return parseURLPair{nil, errors.New("empty url")}
	}
	url := new(URL)

	if rawURL == "*" {
		url.Path = "*"
		return parseURLPair{url, nil}
	}

	// Split off possible leading "http:", "mailto:", etc.
	// Cannot contain escaped characters.
	__tmpGS := __getScheme(rawURL)
	url.Scheme, rest, err = __tmpGS.scheme, __tmpGS.path, __tmpGS.err
	if err != nil {
		return parseURLPair{nil, err}
	}
	url.Scheme = strings.ToLower(url.Scheme)

	if strings.HasSuffix(rest, "?") && strings.Count(rest, "?") == 1 {
		url.ForceQuery = true
		rest = rest[:len(rest)-1]
	} else {
		__tmpCut := __cut(rest, "?")
		rest, url.RawQuery, _ = __tmpCut.before, __tmpCut.after, __tmpCut.found
	}

	if !strings.HasPrefix(rest, "/") {
		if url.Scheme != "" {
			// We consider rootless paths per RFC 3986 as opaque.
			url.Opaque = rest
			return parseURLPair{url, nil}
		}
		if viaRequest {
			return parseURLPair{nil, errors.New("invalid URI for request")}
		}

		// Avoid confusion with malformed schemes, like cache_object:foo/bar.
		// See golang.org/issue/16822.
		//
		// RFC 3986, §3.3:
		// In addition, a URI reference (Section 4.1) may be a relative-path reference,
		// in which case the first path segment cannot contain a colon (":") character.
		__tmpCut2 := __cut(rest, "/")
		segment := __tmpCut2.before
		if strings.Contains(segment, ":") {
			// First path segment has colon. Not allowed in relative URL.
			return parseURLPair{nil, errors.New("first path segment in URL cannot contain colon")}
		}
	}

	if (url.Scheme != "" || !viaRequest && !strings.HasPrefix(rest, "///")) && strings.HasPrefix(rest, "//") {
		var authority string
		authority, rest = rest[2:], ""
		if i := strings.Index(authority, "/"); i >= 0 {
			authority, rest = authority[:i], authority[i:]
		}
		__tmpPA := __parseAuthority(url.Scheme, authority)
		url.User, url.Host, err = __tmpPA.user, __tmpPA.host, __tmpPA.err
		if err != nil {
			return parseURLPair{nil, err}
		}
	} else if url.Scheme != "" && strings.HasPrefix(rest, "/") {
		// OmitHost is set to true when rawURL has an empty host (authority).
		// See golang.org/issue/46059.
		url.OmitHost = true
	}

	// Set Path and, optionally, RawPath.
	// RawPath is a hint of the encoding of Path. We don't want to set it if
	// the default escaping of Path is equivalent, to help make sure that people
	// don't rely on it in general.
	if err := url.setPath(rest); err != nil {
		return parseURLPair{nil, err}
	}
	return parseURLPair{url, nil}
}

func __parseAuthority(scheme, authority string) parseAuthorityTriple {
	i := strings.LastIndex(authority, "@")
	var host string
	var err error
	var user *Userinfo
	var username, password string
	if i < 0 {
		__tmpPH := __parseHost(scheme, authority)
		host, err = __tmpPH.s, __tmpPH.err
	} else {
		__tmpPH := __parseHost(scheme, authority[i+1:])
		host, err = __tmpPH.s, __tmpPH.err
	}
	if err != nil {
		return parseAuthorityTriple{nil, "", err}
	}
	if i < 0 {
		return parseAuthorityTriple{nil, host, nil}
	}
	userinfo := authority[:i]
	if !validUserinfo(userinfo) {
		return parseAuthorityTriple{nil, "", errors.New("net/url: invalid userinfo")}
	}
	if !strings.Contains(userinfo, ":") {
		__tmpUE := __unescape(userinfo, encodeUserPassword)
		userinfo, err = __tmpUE.s, __tmpUE.err
		if err != nil {
			return parseAuthorityTriple{nil, "", err}
		}
		user = User(userinfo)
	} else {
		__tmpCut3 := __cut(userinfo, ":")
		username, password, _ = __tmpCut3.before, __tmpCut3.after, __tmpCut3.found
		__tmpUE := __unescape(username, encodeUserPassword)
		username, err = __tmpUE.s, __tmpUE.err
		if err != nil {
			return parseAuthorityTriple{nil, "", err}
		}
		__tmpUE2 := __unescape(password, encodeUserPassword)
		password, err = __tmpUE2.s, __tmpUE2.err
		if err != nil {
			return parseAuthorityTriple{nil, "", err}
		}
		user = UserPassword(username, password)
	}
	return parseAuthorityTriple{user, host, nil}
}

// parseHost parses host as an authority without user
// information. That is, as host[:port].
func __parseHost(scheme, host string) stringErrPair {
	if openBracketIdx := strings.LastIndex(host, "["); openBracketIdx > 0 {
		return stringErrPair{"", errors.New("invalid IP-literal")}
	} else if openBracketIdx == 0 {
		// Parse an IP-Literal in RFC 3986 and RFC 6874.
		// E.g., "[fe80::1]", "[fe80::1%25en0]", "[fe80::1]:80".
		closeBracketIdx := strings.LastIndex(host, "]")
		if closeBracketIdx < 0 {
			return stringErrPair{"", errors.New("missing ']' in host")}
		}

		colonPort := host[closeBracketIdx+1:]
		if !validOptionalPort(colonPort) {
			return stringErrPair{"", fmt.Errorf("invalid port %q after host", colonPort)}
		}
		__tmpUE2 := __unescape(colonPort, encodeHost)
		unescapedColonPort, err := __tmpUE2.s, __tmpUE2.err
		if err != nil {
			return stringErrPair{"", err}
		}

		hostname := host[openBracketIdx+1 : closeBracketIdx]
		var unescapedHostname string
		// RFC 6874 defines that %25 (%-encoded percent) introduces
		// the zone identifier, and the zone identifier can use basically
		// any %-encoding it likes. That's different from the host, which
		// can only %-encode non-ASCII bytes.
		// We do impose some restrictions on the zone, to avoid stupidity
		// like newlines.
		zoneIdx := strings.Index(hostname, "%25")
		if zoneIdx >= 0 {
			__tmpUE3 := __unescape(hostname[:zoneIdx], encodeHost)
			hostPart, err := __tmpUE3.s, __tmpUE3.err
			if err != nil {
				return stringErrPair{"", err}
			}
			__tmpUE4 := __unescape(hostname[zoneIdx:], encodeZone)
			zonePart, err := __tmpUE4.s, __tmpUE4.err
			if err != nil {
				return stringErrPair{"", err}
			}
			unescapedHostname = hostPart + zonePart
		} else {
			var err error
			__tmpUE5 := __unescape(hostname, encodeHost)
			unescapedHostname, err = __tmpUE5.s, __tmpUE5.err
			if err != nil {
				return stringErrPair{"", err}
			}
		}

		// Per RFC 3986, only a host identified by a valid
		// IPv6 address can be enclosed by square brackets.
		// This excludes any IPv4, but notably not IPv4-mapped addresses.
		addr, err := netip.ParseAddr(unescapedHostname)
		if err != nil {
			return stringErrPair{"", fmt.Errorf("invalid host: %w", err)}
		}
		if addr.Is4() {
			return stringErrPair{"", errors.New("invalid IP-literal")}
		}
		return stringErrPair{"[" + unescapedHostname + "]" + unescapedColonPort, nil}
	} else if i := strings.Index(host, ":"); i != -1 {
		lastColon := strings.LastIndex(host, ":")
		if lastColon != i {
			// RFC 3986 does not allow colons to appear in the host subcomponent.
			//
			// However, a number of databases including PostgreSQL and MongoDB
			// permit a comma-separated list of hosts (with optional ports) in the
			// host subcomponent.
			//
			// Since we historically permitted colons to appear in the host,
			// enforce strict colons only for http and https URLs.
			//
			// See https://go.dev/issue/75223 and https://go.dev/issue/78077.
			if scheme == "http" || scheme == "https" {
				if urlstrictcolons.Value() == "0" {
					urlstrictcolons.IncNonDefault()
					i = lastColon
				}
			} else {
				i = lastColon
			}
		}
		colonPort := host[i:]
		if !validOptionalPort(colonPort) {
			return stringErrPair{"", fmt.Errorf("invalid port %q after host", colonPort)}
		}
	}

	var err error
	__tmpUE6 := __unescape(host, encodeHost)
	host, err = __tmpUE6.s, __tmpUE6.err
	if err != nil {
		return stringErrPair{"", err}
	}
	return stringErrPair{host, nil}
}

// setPath sets the Path and RawPath fields of the URL based on the provided
// escaped path p. It maintains the invariant that RawPath is only specified
// when it differs from the default encoding of the path.
// For example:
// - setPath("/foo/bar")   will set Path="/foo/bar" and RawPath=""
// - setPath("/foo%2fbar") will set Path="/foo/bar" and RawPath="/foo%2fbar"
// setPath will return an error only if the provided path contains an invalid
// escaping.
//
// setPath should be an internal detail,
// but widely used packages access it using linkname.
// Notable members of the hall of shame include:
//   - github.com/sagernet/sing
//
// Do not remove or change the type signature.
// See go.dev/issue/67401.
//
func (u *URL) setPath(p string) error {
	__tmpUE7 := __unescape(p, encodePath)
	path, err := __tmpUE7.s, __tmpUE7.err
	if err != nil {
		return err
	}
	u.Path = path
	if escp := escape(path, encodePath); p == escp {
		// Default encoding is fine.
		u.RawPath = ""
	} else {
		u.RawPath = p
	}
	return nil
}


// EscapedPath returns the escaped form of u.Path.
// In general there are multiple possible escaped forms of any path.
// EscapedPath returns u.RawPath when it is a valid escaping of u.Path.
// Otherwise EscapedPath ignores u.RawPath and computes an escaped
// form on its own.
// The [URL.String] and [URL.RequestURI] methods use EscapedPath to construct
// their results.
// In general, code should call EscapedPath instead of
// reading u.RawPath directly.
func (u *URL) EscapedPath() string {
	if u.RawPath != "" && validEncoded(u.RawPath, encodePath) {
		__tmpUE8 := __unescape(u.RawPath, encodePath)
	p, err := __tmpUE8.s, __tmpUE8.err
		if err == nil && p == u.Path {
			return u.RawPath
		}
	}
	if u.Path == "*" {
		return "*" // don't escape (Issue 11202)
	}
	return escape(u.Path, encodePath)
}

// validEncoded reports whether s is a valid encoded path or fragment,
// according to mode.
// It must not contain any bytes that require escaping during encoding.
func validEncoded(s string, mode encoding) bool {
	for i := 0; i < len(s); i++ {
		// RFC 3986, Appendix A.
		// pchar = unreserved / pct-encoded / sub-delims / ":" / "@".
		// shouldEscape is not quite compliant with the RFC,
		// so we check the sub-delims ourselves and let
		// shouldEscape handle the others.
		switch s[i] {
		case '!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=', ':', '@':
			// ok
		case '[', ']':
			// ok - not specified in RFC 3986 but left alone by modern browsers
		case '%':
			// ok - percent encoded, will decode
		default:
			if shouldEscape(s[i], mode) {
				return false
			}
		}
	}
	return true
}

// setFragment is like setPath but for Fragment/RawFragment.
func (u *URL) setFragment(f string) error {
	__tmpUE9 := __unescape(f, encodeFragment)
	frag, err := __tmpUE9.s, __tmpUE9.err
	if err != nil {
		return err
	}
	u.Fragment = frag
	if escf := escape(frag, encodeFragment); f == escf {
		// Default encoding is fine.
		u.RawFragment = ""
	} else {
		u.RawFragment = f
	}
	return nil
}

// EscapedFragment returns the escaped form of u.Fragment.
// In general there are multiple possible escaped forms of any fragment.
// EscapedFragment returns u.RawFragment when it is a valid escaping of u.Fragment.
// Otherwise EscapedFragment ignores u.RawFragment and computes an escaped
// form on its own.
// The [URL.String] method uses EscapedFragment to construct its result.
// In general, code should call EscapedFragment instead of
// reading u.RawFragment directly.
func (u *URL) EscapedFragment() string {
	if u.RawFragment != "" && validEncoded(u.RawFragment, encodeFragment) {
		__tmpUE10 := __unescape(u.RawFragment, encodeFragment)
	f, err := __tmpUE10.s, __tmpUE10.err
		if err == nil && f == u.Fragment {
			return u.RawFragment
		}
	}
	return escape(u.Fragment, encodeFragment)
}

// validOptionalPort reports whether port is either an empty string
// or matches /^:\d*$/
func validOptionalPort(port string) bool {
	if port == "" {
		return true
	}
	if port[0] != ':' {
		return false
	}
	for _, b := range port[1:] {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}

// String reassembles the [URL] into a valid URL string.
// The general form of the result is one of:
//
//	scheme:opaque?query#fragment
//	scheme://userinfo@host/path?query#fragment
//
// If u.Opaque is non-empty, String uses the first form;
// otherwise it uses the second form.
// Any non-ASCII characters in host are escaped.
// To obtain the path, String uses u.EscapedPath().
//
// In the second form, the following rules apply:
//   - if u.Scheme is empty, scheme: is omitted.
//   - if u.User is nil, userinfo@ is omitted.
//   - if u.Host is empty, host/ is omitted.
//   - if u.Scheme and u.Host are empty and u.User is nil,
//     the entire scheme://userinfo@host/ is omitted.
//   - if u.Host is non-empty and u.Path begins with a /,
//     the form host/path does not add its own /.
//   - if u.RawQuery is empty, ?query is omitted.
//   - if u.Fragment is empty, #fragment is omitted.
func (u *URL) String() string {
	var buf strings.Builder

	n := len(u.Scheme)
	if u.Opaque != "" {
		n += len(u.Opaque)
	} else {
		if !u.OmitHost && (u.Scheme != "" || u.Host != "" || u.User != nil) {
			username := u.User.Username()
			password, _ := u.User.Password()
			n += len(username) + len(password) + len(u.Host)
		}
		n += len(u.Path)
	}
	n += len(u.RawQuery) + len(u.RawFragment)
	n += len(":" + "//" + "//" + ":" + "@" + "/" + "./" + "?" + "#")
	buf.Grow(n)

	if u.Scheme != "" {
		buf.WriteString(u.Scheme)
		buf.WriteByte(':')
	}
	if u.Opaque != "" {
		buf.WriteString(u.Opaque)
	} else {
		if u.Scheme != "" || u.Host != "" || u.User != nil {
			if u.OmitHost && u.Host == "" && u.User == nil {
				// omit empty host
			} else {
				if u.Host != "" || u.Path != "" || u.User != nil {
					buf.WriteString("//")
				}
				if ui := u.User; ui != nil {
					buf.WriteString(ui.String())
					buf.WriteByte('@')
				}
				if h := u.Host; h != "" {
					buf.WriteString(escape(h, encodeHost))
				}
			}
		}
		path := u.EscapedPath()
		if u.OmitHost && u.Host == "" && u.User == nil && strings.HasPrefix(path, "//") {
			// Escape the first / in a path starting with "//" and no authority
			// so that re-parsing the URL doesn't turn the path into an authority
			// (e.g., Path="//host/p" producing "http://host/p").
			buf.WriteString("%2F")
			path = path[1:]
		}
		if path != "" && path[0] != '/' && u.Host != "" {
			buf.WriteByte('/')
		}
		if buf.Len() == 0 {
			// RFC 3986 §4.2
			// A path segment that contains a colon character (e.g., "this:that")
			// cannot be used as the first segment of a relative-path reference, as
			// it would be mistaken for a scheme name. Such a segment must be
			// preceded by a dot-segment (e.g., "./this:that") to make a relative-
			// path reference.
			__tmpCut6 := __cut(path, "/")
			segment := __tmpCut6.before
			if strings.Contains(segment, ":") {
				buf.WriteString("./")
			}
		}
		buf.WriteString(path)
	}
	if u.ForceQuery || u.RawQuery != "" {
		buf.WriteByte('?')
		buf.WriteString(u.RawQuery)
	}
	if u.Fragment != "" {
		buf.WriteByte('#')
		buf.WriteString(u.EscapedFragment())
	}
	return buf.String()
}

// Redacted is like [URL.String] but replaces any password with "xxxxx".
// Only the password in u.User is redacted.
func (u *URL) Redacted() string {
	if u == nil {
		return ""
	}

	ru := *u
	if _, has := ru.User.Password(); has {
		ru.User = UserPassword(ru.User.Username(), "xxxxx")
	}
	return ru.String()
}

// Values maps a string key to a list of values.
// It is typically used for query parameters and form values.
// Unlike in the http.Header map, the keys in a Values map
// are case-sensitive.
type Values map[string][]string

// Get gets the first value associated with the given key.
// If there are no values associated with the key, Get returns
// the empty string. To access multiple values, use the map
// directly.
func (v Values) Get(key string) string {
	vs := v[key]
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

// Set sets the key to value. It replaces any existing
// values.
func (v Values) Set(key, value string) {
	v[key] = []string{value}
}

// Add adds the value to key. It appends to any existing
// values associated with key.
func (v Values) Add(key, value string) {
	v[key] = append(v[key], value)
}

// Del deletes the values associated with key.
func (v Values) Del(key string) {
	delete(v, key)
}

// Has checks whether a given key is set.
func (v Values) Has(key string) bool {
	_, ok := v[key]
	return ok
}

// Clone creates a deep copy of the subject [Values].
func (vs Values) Clone() Values {
	if vs == nil {
		return nil
	}

	newVals := make(Values, len(vs))
	for k, v := range vs {
		newVals[k] = slices.Clone(v)
	}
	return newVals
}

// ParseQuery parses the URL-encoded query string and returns
// a map listing the values specified for each key.
// ParseQuery always returns a non-nil map containing all the
// valid query parameters found; err describes the first decoding error
// encountered, if any.
//
// Query is expected to be a list of key=value settings separated by ampersands.
// A setting without an equals sign is interpreted as a key set to an empty
// value.
// Settings containing a non-URL-encoded semicolon are considered invalid.
func __parseQueryPub(query string) parseQueryPair {
	m := make(Values)
	err := parseQuery(m, query)
	return parseQueryPair{m, err}
}

var urlmaxqueryparams = &godebugSetting{name: "urlmaxqueryparams"}

// Keep this in sync with net/http/httputil.
const defaultMaxParams = 10000

func urlParamsWithinMax(params int) bool {
	withinDefaultMax := params <= defaultMaxParams
	if urlmaxqueryparams.Value() == "" {
		return withinDefaultMax
	}
	customMax, err := strconv.Atoi(urlmaxqueryparams.Value())
	if err != nil {
		return withinDefaultMax
	}
	withinCustomMax := customMax == 0 || params < customMax
	if withinDefaultMax != withinCustomMax {
		urlmaxqueryparams.IncNonDefault()
	}
	return withinCustomMax
}

func parseQuery(m Values, query string) (err error) {
	if !urlParamsWithinMax(strings.Count(query, "&") + 1) {
		return errors.New("number of URL query parameters exceeded limit")
	}
	for query != "" {
		var key string
		__tmpCut7 := __cut(query, "&")
		key, query, _ = __tmpCut7.before, __tmpCut7.after, __tmpCut7.found
		if strings.Contains(key, ";") {
			err = fmt.Errorf("invalid semicolon separator in query")
			continue
		}
		if key == "" {
			continue
		}
		__tmpCut8 := __cut(key, "=")
		key, value, _ := __tmpCut8.before, __tmpCut8.after, __tmpCut8.found
		__tmpQ := __queryUnescape(key)
		key, err1 := __tmpQ.s, __tmpQ.err
		if err1 != nil {
			if err == nil {
				err = err1
			}
			continue
		}
		__tmpQ2 := __queryUnescape(value)
		value, err1 = __tmpQ2.s, __tmpQ2.err
		if err1 != nil {
			if err == nil {
				err = err1
			}
			continue
		}
		m[key] = append(m[key], value)
	}
	return err
}

// Encode encodes the values into “URL encoded” form
// ("bar=baz&foo=quux") sorted by key.
func (v Values) Encode() string {
	if len(v) == 0 {
		return ""
	}
	var buf strings.Builder
	// To minimize allocations, we eschew iterators and pre-size the slice in
	// which we collect v's keys.
	keys := make([]string, len(v))
	var i int
	for k := range v {
		keys[i] = k
		i++
	}
	slices.Sort(keys)
	for _, k := range keys {
		vs := v[k]
		keyEscaped := QueryEscape(k)
		for _, v := range vs {
			if buf.Len() > 0 {
				buf.WriteByte('&')
			}
			buf.WriteString(keyEscaped)
			buf.WriteByte('=')
			buf.WriteString(QueryEscape(v))
		}
	}
	return buf.String()
}

// resolvePath applies special path segments from refs and applies
// them to base, per RFC 3986.
func resolvePath(base, ref string) string {
	var full string
	if ref == "" {
		full = base
	} else if ref[0] != '/' {
		i := strings.LastIndex(base, "/")
		full = base[:i+1] + ref
	} else {
		full = ref
	}
	if full == "" {
		return ""
	}

	dst := make([]byte, 0, len(full)+1)
	dst = append(dst, '/')
	elem := ""
	remaining := full
	found := true
	first := true
	for found {
		__tmpCut9 := __cut(remaining, "/")
		elem, remaining, found = __tmpCut9.before, __tmpCut9.after, __tmpCut9.found
		switch elem {
		case ".":
			first = false
			continue
		case "..":
			if i := bytes.LastIndexByte(dst[1:], '/'); i >= 0 {
				dst = dst[:i+1]
			} else {
				dst = dst[:1]
			}
			first = len(dst) == 1
		default:
			if !first {
				dst = append(dst, '/')
			}
			dst = append(dst, elem...)
			first = false
		}
	}

	if elem == "." || elem == ".." {
		dst = append(dst, '/')
	}

	// We wrote an initial '/', but we don't want two.
	if len(dst) > 1 && dst[1] == '/' {
		return string(dst[1:])
	}
	return string(dst)
}

// IsAbs reports whether the [URL] is absolute.
// Absolute means that it has a non-empty scheme.
func (u *URL) IsAbs() bool {
	return u.Scheme != ""
}

// Parse parses a [URL] in the context of the receiver. The provided URL
// may be relative or absolute. Parse returns nil, err on parse
// failure, otherwise its return value is the same as [URL.ResolveReference].
func (u *URL) Parse(ref string) (*URL, error) {
	panic("url.URL.Parse: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

// ResolveReference resolves a URI reference to an absolute URI from
// an absolute base URI u, per RFC 3986 Section 5.2. The URI reference
// may be relative or absolute. ResolveReference always returns a new
// [URL] instance, even if the returned URL is identical to either the
// base or reference. If ref is an absolute URL, then ResolveReference
// ignores base and returns a copy of ref.
func (u *URL) ResolveReference(ref *URL) *URL {
	url := *ref
	if ref.Scheme == "" {
		url.Scheme = u.Scheme
	}
	if ref.Scheme != "" || ref.Host != "" || ref.User != nil {
		// The "absoluteURI" or "net_path" cases.
		// We can ignore the error from setPath since we know we provided a
		// validly-escaped path.
		url.setPath(resolvePath(ref.EscapedPath(), ""))
		return &url
	}
	if ref.Opaque != "" {
		url.User = nil
		url.Host = ""
		url.Path = ""
		return &url
	}
	if ref.Path == "" && !ref.ForceQuery && ref.RawQuery == "" {
		url.RawQuery = u.RawQuery
		if ref.Fragment == "" {
			url.Fragment = u.Fragment
			url.RawFragment = u.RawFragment
		}
	}
	if ref.Path == "" && u.Opaque != "" {
		url.Opaque = u.Opaque
		url.User = nil
		url.Host = ""
		url.Path = ""
		return &url
	}
	// The "abs_path" or "rel_path" cases.
	url.Host = u.Host
	url.User = u.User
	url.setPath(resolvePath(u.EscapedPath(), ref.EscapedPath()))
	return &url
}

// Query parses RawQuery and returns the corresponding values.
// It silently discards malformed value pairs.
// To check errors use [ParseQuery].
func (u *URL) Query() Values {
	__tmpQ3 := __parseQueryPub(u.RawQuery)
	v := __tmpQ3.v
	return v
}

// RequestURI returns the encoded path?query or opaque?query
// string that would be used in an HTTP request for u.
func (u *URL) RequestURI() string {
	result := u.Opaque
	if result == "" {
		result = u.EscapedPath()
		if result == "" {
			result = "/"
		}
	} else {
		if strings.HasPrefix(result, "//") {
			result = u.Scheme + ":" + result
		}
	}
	if u.ForceQuery || u.RawQuery != "" {
		result += "?" + u.RawQuery
	}
	return result
}

// Hostname returns u.Host, stripping any valid port number if present.
//
// If the result is enclosed in square brackets, as literal IPv6 addresses are,
// the square brackets are removed from the result.
func (u *URL) Hostname() string {
	__tmpSHP := __splitHostPort(u.Host)
	host := __tmpSHP.host
	return host
}

// Port returns the port part of u.Host, without the leading colon.
//
// If u.Host doesn't contain a valid numeric port, Port returns an empty string.
func (u *URL) Port() string {
	__tmpSHP2 := __splitHostPort(u.Host)
	port := __tmpSHP2.port
	return port
}

// splitHostPort separates host and port. If the port is not valid, it returns
// the entire input as host, and it doesn't check the validity of the host.
// Unlike net.SplitHostPort, but per RFC 3986, it requires ports to be numeric.
func __splitHostPort(hostPort string) splitHostPortPair {
	host := hostPort
	var port string

	colon := strings.LastIndexByte(host, ':')
	if colon != -1 && validOptionalPort(host[colon:]) {
		host, port = host[:colon], host[colon+1:]
	}

	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}

	return splitHostPortPair{host, port}
}

// Marshaling interface implementations.
// Would like to implement MarshalText/UnmarshalText but that will change the JSON representation of URLs.





func (u *URL) UnmarshalBinary(text []byte) error {
	u1, err := Parse(string(text))
	if err != nil {
		return err
	}
	*u = *u1
	return nil
}

// JoinPath returns a new [URL] with the provided path elements joined to
// any existing path and the resulting path cleaned of any ./ or ../ elements.
// Any sequences of multiple / characters will be reduced to a single /.
// Path elements must already be in escaped form, as produced by [PathEscape].
func (u *URL) JoinPath(elem ...string) *URL {
	__tmpJP2 := __joinPath(u, elem...)
	return __tmpJP2.url
}

func __joinPath(u *URL, elem ...string) joinPathPair {
	elem = append([]string{u.EscapedPath()}, elem...)
	var p string
	if !strings.HasPrefix(elem[0], "/") {
		// Return a relative path if u is relative,
		// but ensure that it contains no ../ elements.
		elem[0] = "/" + elem[0]
		p = __urlPathJoin(elem...)[1:]
	} else {
		p = __urlPathJoin(elem...)
	}
	// path.Join will remove any trailing slashes.
	// Preserve at least one.
	if strings.HasSuffix(elem[len(elem)-1], "/") && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	url := *u
	err := url.setPath(p)
	return joinPathPair{&url, err}
}

// validUserinfo reports whether s is a valid userinfo string per RFC 3986
// Section 3.2.1:
//
//	userinfo    = *( unreserved / pct-encoded / sub-delims / ":" )
//	unreserved  = ALPHA / DIGIT / "-" / "." / "_" / "~"
//	sub-delims  = "!" / "$" / "&" / "'" / "(" / ")"
//	              / "*" / "+" / "," / ";" / "="
//
// It doesn't validate pct-encoded. The caller does that via func unescape.
func validUserinfo(s string) bool {
	for _, r := range s {
		if 'A' <= r && r <= 'Z' {
			continue
		}
		if 'a' <= r && r <= 'z' {
			continue
		}
		if '0' <= r && r <= '9' {
			continue
		}
		switch r {
		case '-', '.', '_', ':', '~', '!', '$', '&', '\'',
			'(', ')', '*', '+', ',', ';', '=', '%':
			continue
		case '@':
			// 'RFC 3986 section 3.2.1' does not allow '@' in userinfo.
			// It is a delimiter between userinfo and host.
			// However, URLs are diverse, and in some cases,
			// the userinfo may contain an '@' character,
			// for example, in "http://username:p@ssword@google.com",
			// the string "username:p@ssword" should be treated as valid userinfo.
			// Ref:
			//   https://go.dev/issue/3439
			//   https://go.dev/issue/22655
			continue
		default:
			return false
		}
	}
	return true
}

// stringContainsCTLByte reports whether s contains any ASCII control character.
func stringContainsCTLByte(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < ' ' || b == 0x7f {
			return true
		}
	}
	return false
}

// JoinPath returns a [URL] string with the provided path elements joined to
// the existing path of base and the resulting path cleaned of any ./ or ../ elements.
// Path elements must already be in escaped form, as produced by [PathEscape].
func JoinPath(base string, elem ...string) (result string, err error) {
	panic("url.JoinPath: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

// Clone creates a deep copy of the fields of the subject [URL].
func (u *URL) Clone() *URL {
	if u == nil {
		return nil
	}

	uc := new(*u)
	if u.User != nil {
		uc.User = new(*u.User)
	}
	return uc
}
`
