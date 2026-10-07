package main

import (
	"fmt"
	"io"
	"strings"
)

// io16: WriteString through interfaces: the StringWriter fast path
// (io.Discard implements StringWriter). D1 (P3-M8 WS D).
//
// NOTE (reviewer-2 P2-1): the "fallback" this program claims below does
// NOT exercise __writeString's fallback branch (synth_io.go: the
// `else w.Write([]byte(s))` arm for a Writer that is not a
// StringWriter). Calling w2.Write directly is a plain interface Write
// dispatch, not the WriteString fallback path. Every WriteString call
// in the corpus uses io.Discard (a StringWriter), so the fallback
// branch has ZERO differential coverage. Re-test trigger: user-type
// method tables (a user Writer that is not a StringWriter — currently
// fails with "no method table installed", pre-existing gap) or a
// synthetic non-StringWriter Writer; then add a corpus program driving
// io.WriteString through it differentially.
func main() {
	// Fast path: io.Discard implements io.StringWriter.
	var w1 io.Writer = io.Discard
	n1, err1 := io.WriteString(w1, "hello")
	fmt.Println(n1, err1 == nil)

	// NOT the WriteString fallback: a direct interface Write dispatch
	// (see NOTE above). Kept as a plain Write sanity check.
	var w2 io.Writer = io.Discard
	n2, err2 := w2.Write([]byte("world"))
	fmt.Println(n2, err2 == nil)

	// WriteString with empty string.
	n3, err3 := io.WriteString(io.Discard, "")
	fmt.Println(n3, err3 == nil)

	// The StringWriter interface assertion itself.
	sw := io.Discard.(io.StringWriter)
	n4, err4 := sw.WriteString("asserted")
	fmt.Println(n4, err4 == nil)

	// Ensure strings package still works alongside.
	var r io.Reader = strings.NewReader("x")
	buf := make([]byte, 4)
	n5, err5 := r.Read(buf)
	fmt.Println(n5, err5 == nil)
}
