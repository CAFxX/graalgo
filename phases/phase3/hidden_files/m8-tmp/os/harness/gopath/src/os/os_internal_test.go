// In-package tests for the synthetic os's pure helpers (no intrinsics).
package os

import "testing"

func TestPrefixAndSuffix(t *testing.T) {
	ps := __prefixAndSuffix("foo*bar")
	if ps.prefix != "foo" || ps.suffix != "bar" || ps.err != nil {
		t.Fatalf("got %+v", ps)
	}
	ps = __prefixAndSuffix("foo")
	if ps.prefix != "foo" || ps.suffix != "" {
		t.Fatalf("got %+v", ps)
	}
	ps = __prefixAndSuffix("a*b*c")
	if ps.prefix != "a*b" || ps.suffix != "c" {
		t.Fatalf("got %+v", ps)
	}
	if ps := __prefixAndSuffix("a/b"); ps.err == nil {
		t.Fatal("separator not rejected")
	}
}

func TestJoinPath(t *testing.T) {
	if got := joinPath("/a/b", "c"); got != "/a/b/c" {
		t.Fatal(got)
	}
	if got := joinPath("/a/b/", "c"); got != "/a/b/c" {
		t.Fatal(got)
	}
	if got := joinPath("", "c"); got != "c" {
		t.Fatal(got)
	}
}

func TestSplitPath(t *testing.T) {
	d, f := splitPath("/a/b/c")
	if d != "/a/b" || f != "c" {
		t.Fatal(d, f)
	}
	d, f = splitPath("c")
	if d != "" || f != "c" {
		t.Fatal(d, f)
	}
	d, f = splitPath("/")
	if d != "/" || f != "" {
		t.Fatal(d, f)
	}
}

func TestValidEnvKey(t *testing.T) {
	if !__validEnvKey("FOO_BAR123") {
		t.Fatal("valid key rejected")
	}
	for _, k := range []string{"", "A=B", "A\x00B"} {
		if __validEnvKey(k) {
			t.Fatal("invalid key accepted:", k)
		}
	}
}

func TestIsPathSeparator(t *testing.T) {
	if !IsPathSeparator('/') || IsPathSeparator('\\') {
		t.Fatal("separator")
	}
}

func TestFormatUint(t *testing.T) {
	if __formatUint(0) != "0" || __formatUint(123456789) != "123456789" {
		t.Fatal("formatUint")
	}
}

func TestTimeFromUnixNano(t *testing.T) {
	// 1700000000 -> 2023-11-14; spot-check the civil conversion.
	tm := __timeFromUnixNano(1700000000 * 1e9)
	if tm.UnixNano()/1e9 != 1700000000 {
		t.Fatal("roundtrip")
	}
}
