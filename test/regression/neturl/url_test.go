package neturl

// Regression tests for the P1-net remediation (2026-10-18):
// URL.MarshalBinary / URL.AppendBinary in the synthetic net/url package
// (previously fail-fast panics).
//
// These run through `graalgo test` (test/regression), exercising the
// Appendix-M desugar (URL.MarshalBinary -> __urlMarshalBinary) end to
// end. Direct calls only: interface dispatch of multi-result methods
// does not desugar (known M9/WS-D limitation, same as every other
// multi-result method in the synthetic packages).

import (
	"net/url"
	"testing"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): unexpected err: %v", raw, err)
	}
	return u
}

// TestURLMarshalBinary pins gc semantics: MarshalBinary returns
// u.AppendBinary(nil), i.e. the String() form as bytes.
func TestURLMarshalBinary(t *testing.T) {
	u := mustParse(t, "https://user:pass@example.com:8080/p?q=1#frag")
	b, err := u.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: unexpected err: %v", err)
	}
	if string(b) != u.String() {
		t.Fatalf("MarshalBinary = %q, want %q", b, u.String())
	}
}

// TestURLAppendBinary pins gc semantics: AppendBinary appends the
// String() form to b and never fails.
func TestURLAppendBinary(t *testing.T) {
	u := mustParse(t, "https://example.com/path")
	prefix := []byte("PREFIX:")
	b, err := u.AppendBinary(prefix)
	if err != nil {
		t.Fatalf("AppendBinary: unexpected err: %v", err)
	}
	if string(b) != "PREFIX:"+u.String() {
		t.Fatalf("AppendBinary = %q, want %q", b, "PREFIX:"+u.String())
	}
	// Appending to nil matches MarshalBinary.
	b2, err := u.AppendBinary(nil)
	if err != nil {
		t.Fatalf("AppendBinary(nil): unexpected err: %v", err)
	}
	m, err := u.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: unexpected err: %v", err)
	}
	if string(b2) != string(m) {
		t.Fatalf("AppendBinary(nil) = %q, MarshalBinary = %q, want equal", b2, m)
	}
}

// TestURLBinaryRoundTrip pins the marshal/parse round trip: the binary
// form re-parses to an equal URL.
func TestURLBinaryRoundTrip(t *testing.T) {
	u := mustParse(t, "https://example.com:8443/a/b?x=1&y=2#z")
	b, err := u.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: unexpected err: %v", err)
	}
	v, err := url.Parse(string(b))
	if err != nil {
		t.Fatalf("Parse(MarshalBinary): unexpected err: %v", err)
	}
	if v.String() != u.String() {
		t.Fatalf("round trip = %q, want %q", v.String(), u.String())
	}
}
