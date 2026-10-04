package harness

import (
	"strings"
	"testing"
)

// Red-green note: every normalization rule has a MUST-CHANGE case (asserting
// output != input, so the test fails if the rule is dropped) and a
// MUST-NOT-CHANGE case (asserting byte-identical output, so the test fails
// if the rule over-matches).

func TestNormalize_AddressesRedacted(t *testing.T) {
	in := "panic: runtime error: invalid memory address or nil pointer dereference\n\tmain.f(0xc000014260)"
	out := Normalize(in)
	if out == in {
		t.Fatal("address was not redacted")
	}
	if strings.Contains(out, "0xc000014260") {
		t.Fatalf("raw address survived: %q", out)
	}
	if !strings.Contains(out, "0xADDR") {
		t.Fatalf("expected 0xADDR marker, got %q", out)
	}
}

func TestNormalize_NoAddressUntouched(t *testing.T) {
	for _, in := range []string{
		"hello world 123",
		"exit status 3",
		"0x is not an address",
		"price: 0xZZ not hex, stays", // Z is not hex: no match at all
	} {
		if out := Normalize(in); out != in {
			t.Fatalf("over-match: %q -> %q", in, out)
		}
	}
}

func TestNormalize_GoroutineIDs(t *testing.T) {
	in := "goroutine 18 [running]:\ncreated by main.main in goroutine 7"
	out := Normalize(in)
	if out == in {
		t.Fatal("goroutine ids not normalized")
	}
	want := "goroutine # [running]:\ncreated by main.main in goroutine #"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestNormalize_GoLineNumbers(t *testing.T) {
	in := "\tmain.main()\n\t\t/tmp/x/main.go:42 +0x27"
	out := Normalize(in)
	if !strings.Contains(out, "main.go:LINE") {
		t.Fatalf("line not normalized: %q", out)
	}
	if strings.Contains(out, ":42") {
		t.Fatalf("raw line survived: %q", out)
	}
	// must not touch non-path numbers
	plain := "go version 1.27 on 2026-10-03"
	if out := Normalize(plain); out != plain {
		t.Fatalf("over-match: %q -> %q", plain, out)
	}
}

func TestNormalize_TracesEqualModuloAddrs(t *testing.T) {
	// The key property: two traces from different runs (different addresses,
	// goroutine ids, same lines modulo inlining) normalize to identical text.
	a := `panic: boom

goroutine 6 [running]:
main.level3()
	/tmp/c/main.go:4 +0x25
main.level2()
	/tmp/c/main.go:5 +0x14
created by main.main in goroutine 1
	/tmp/c/main.go:8 +0x29`
	b := `panic: boom

goroutine 19 [running]:
main.level3()
	/tmp/c/main.go:4 +0x3a
main.level2()
	/tmp/c/main.go:5 +0x1f
created by main.main in goroutine 1
	/tmp/c/main.go:8 +0x31`
	if a == b {
		t.Fatal("test bug: inputs must differ before normalization")
	}
	na, nb := Normalize(a), Normalize(b)
	if na != nb {
		t.Fatalf("normalized traces differ:\n%s\n---\n%s", na, nb)
	}
}

func TestNormalize_SignalInfo(t *testing.T) {
	in := "fatal error: concurrent map writes\n[signal SIGSEGV: segmentation violation code=0x1 addr=0x8 pc=0x4a3b2c]"
	out := Normalize(in)
	if strings.Contains(out, "0x8") || strings.Contains(out, "0x4a3b2c") {
		t.Fatalf("signal hex not redacted: %q", out)
	}
	if !strings.Contains(out, "SIGSEGV") {
		t.Fatalf("signal name must survive: %q", out)
	}
}
