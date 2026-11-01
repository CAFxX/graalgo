package pkg031

import (
	"io"
	"strings"
	"testing"
)

// TestBuilderWriteViaInterface is a regression test for PKG-031
// (graalgo test encoding/pem hangs — the second hang).
//
// encoding/pem's TestLineBreaker assigns *strings.Builder to an io.Writer
// and writes through the interface. The synthetic Builder.Write/WriteRune/
// WriteString were fail-fast panics ("not in M7 scope"); dispatching to
// them through an interface hung the test binary instead of failing
// cleanly, so `graalgo test encoding/pem` never completed.
//
// The fix implements the three methods for real (the m7 desugar still
// rewrites direct calls to the __builderWrite* helpers; interface dispatch
// needs the real methods).
//
// The test drives Write and WriteString through interface dispatch and
// verifies the accumulated content. It guards the dispatch path that
// encoding/pem's TestLineBreaker relies on: interface dispatch of
// (*Builder).Write must reach a working implementation (via the
// __builderWrite* helpers that the method table maps to), not a fail-fast
// panic or a hang. Note: it passes both before and after the part-2
// commit that gave the methods real bodies, because interface dispatch
// routes to the helpers either way — the part-2 bodies are currently
// unreachable through supported call paths (direct calls desugar, and
// method values bind the helpers too, per P1-9), so no red/green test
// can distinguish it; it is kept as a robustness improvement.
//
// NOTE: this test deliberately avoids one pattern that is broken under
// graalgo for an unrelated, pre-existing reason (see the PKG-031 lane
// report): the goroutine+time.After timeout guard — timer channels do
// not fire yet, so a timeout-guarded test hangs instead of failing.
// Correctness is asserted synchronously instead. The second pattern the
// original note avoided — a custom interface with a multi-result method
// (e.g. for WriteRune) taken as a method value — is covered by
// TestBuilderMethodValues below since P1-9 (review-2026-10-31-22);
// it used to fail at export time.
func TestBuilderWriteViaInterface(t *testing.T) {
	var b strings.Builder

	var w io.Writer = &b
	for i := 0; i < 8; i++ {
		if _, err := w.Write([]byte("0123456789")); err != nil {
			t.Fatalf("io.Writer.Write: unexpected err: %v", err)
		}
	}

	var sw io.StringWriter = &b
	if _, err := sw.WriteString("abcdefgh"); err != nil {
		t.Fatalf("io.StringWriter.WriteString: unexpected err: %v", err)
	}

	want := "0123456789012345678901234567890123456789012345678901234567890123456789" +
		"0123456789" + "abcdefgh"
	if got := b.String(); got != want {
		t.Fatalf("Builder via interface = %q, want %q", got, want)
	}
}

// mvRuneWriter is a custom interface with a multi-result method, the
// shape the original PKG-031 note avoided: taking such a method as a
// value used to fail at export time (P1-9, review-2026-10-31-22).
type mvRuneWriter interface {
	WriteRune(r rune) (int, error)
}

// mvCounter is a small value type for the receiver-snapshot check:
// a method value binds the receiver evaluated at formation time.
type mvCounter struct{ x int }

func (c mvCounter) Add(y int) int { return c.x + y }

// TestBuilderMethodValues covers method values on strings.Builder:
// through io.Writer / io.StringWriter (single-result interface method
// values), through a custom multi-result interface (WriteRune), and
// directly on the concrete Builder (multi-result method values). It
// also checks that the bound receiver is the one evaluated at
// formation: reassigning the counter variable afterwards does not
// change what the value returns.
func TestBuilderMethodValues(t *testing.T) {
	var b strings.Builder

	// Interface method value, single result, via io.Writer.
	var w io.Writer = &b
	f := w.Write
	if n, err := f([]byte("ab")); n != 2 || err != nil {
		t.Fatalf("io.Writer method value: n=%d err=%v, want 2 nil", n, err)
	}

	// Interface method value, single result, via io.StringWriter.
	var sw io.StringWriter = &b
	g := sw.WriteString
	if n, err := g("cd"); n != 2 || err != nil {
		t.Fatalf("io.StringWriter method value: n=%d err=%v, want 2 nil", n, err)
	}

	// Interface method value, multi-result, via a custom interface.
	var rw mvRuneWriter = &b
	h := rw.WriteRune
	if n, err := h('e'); n != 1 || err != nil {
		t.Fatalf("mvRuneWriter method value: n=%d err=%v, want 1 nil", n, err)
	}

	// Concrete multi-result method values on the Builder itself.
	ws := b.WriteString
	if n, err := ws("fg"); n != 2 || err != nil {
		t.Fatalf("Builder.WriteString method value: n=%d err=%v, want 2 nil", n, err)
	}
	wr := b.WriteRune
	if n, err := wr('h'); n != 1 || err != nil {
		t.Fatalf("Builder.WriteRune method value: n=%d err=%v, want 1 nil", n, err)
	}

	if got := b.String(); got != "abcdefgh" {
		t.Fatalf("Builder via method values = %q, want %q", got, "abcdefgh")
	}

	// Forming a method value on a nil interface must not panic; only
	// calling it would. (No call here, by design.)
	var nilW io.Writer
	nilF := nilW.Write
	_ = nilF

	// Receiver snapshot: the value keeps the receiver from formation.
	c := mvCounter{x: 3}
	add := c.Add
	c = mvCounter{x: 100}
	if got := add(4); got != 7 {
		t.Fatalf("method value after receiver reassignment = %d, want 7", got)
	}
}
