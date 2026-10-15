package pkg031

import (
	"io"
	"strings"
	"testing"
	"time"
)

// runeWriter mirrors the shape of strings.Builder.WriteRune behind an
// interface. (The standard library has no io.RuneWriter.)
type runeWriter interface {
	WriteRune(r rune) (int, error)
}

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
// The test drives all three methods through interface dispatch in a
// goroutine guarded by a timeout: pre-fix it panics or times out (red);
// post-fix it completes and the accumulated content is verified (green).
func TestBuilderWriteViaInterface(t *testing.T) {
	type result struct {
		s   string
		err string
	}
	done := make(chan result, 1)
	go func() {
		var b strings.Builder

		var w io.Writer = &b
		for i := 0; i < 8; i++ {
			if n, err := w.Write([]byte("0123456789")); err != nil || n != 10 {
				done <- result{err: "io.Writer.Write: n/err mismatch"}
				return
			}
		}

		var sw io.StringWriter = &b
		if n, err := sw.WriteString("abcdefgh"); err != nil || n != 8 {
			done <- result{err: "io.StringWriter.WriteString: n/err mismatch"}
			return
		}

		var rw runeWriter = &b
		if n, err := rw.WriteRune('é'); err != nil || n != 2 {
			done <- result{err: "runeWriter.WriteRune: n/err mismatch"}
			return
		}

		done <- result{s: b.String()}
	}()

	select {
	case r := <-done:
		if r.err != "" {
			t.Fatal(r.err)
		}
		want := "0123456789012345678901234567890123456789012345678901234567890123456789" +
			"0123456789" + "abcdefgh" + "é"
		if r.s != want {
			t.Fatalf("Builder via interface = %q, want %q", r.s, want)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("TestBuilderWriteViaInterface timed out after 30s (PKG-031: Builder.Write* via interface hangs)")
	}
}
