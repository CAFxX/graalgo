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
// unreachable through supported call paths (direct calls desugar;
// method values of multi-result methods don't compile), so no red/green
// test can distinguish it; it is kept as a robustness improvement.
//
// NOTE: this test deliberately avoids two patterns that are broken under
// graalgo for unrelated, pre-existing reasons (see the PKG-031 lane
// report): (1) the goroutine+time.After timeout guard — timer channels do
// not fire yet, so a timeout-guarded test hangs instead of failing;
// (2) a custom interface with a multi-result method (e.g. for WriteRune)
// — the backend rejects it with "illegal conversion: STRUCT -> INT64".
// Correctness is asserted synchronously instead.
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
