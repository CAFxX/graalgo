package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSyntheticBufioUtf8 is the REVIEW-2026-11-03-16 S9-extension
// regression test: bufio's utf8 helpers were inlined as __bufio*
// pending unicode/utf8 landing, and Reader.ReadRune was deleted
// outright (3-result; "uses 2-result utf8 helper"). unicode/utf8 has
// landed in syntheticSources, so bufio now uses it directly and
// ReadRune is restored via the Appendix-M pair helper. This test pins
// export of a program exercising ScanWords over non-ASCII input,
// ReadRune, and WriteRune; execution-level assertions live in the
// lane's sync-run verification.
func TestSyntheticBufioUtf8(t *testing.T) {
	src := `package main

import (
	"bufio"
	"strings"
)

func main() {
	sc := bufio.NewScanner(strings.NewReader("café au lait"))
	sc.Split(bufio.ScanWords)
	for sc.Scan() {
		println(sc.Text())
	}
	r := bufio.NewReader(strings.NewReader("café"))
	ch, size, err := r.ReadRune()
	if err != nil {
		println("readrune err")
	}
	println(string(ch), size)
	var sb strings.Builder
	w := bufio.NewWriter(&sb)
	if _, err := w.WriteRune('é'); err != nil {
		println("writerune err")
	}
	if err := w.Flush(); err != nil {
		println("flush err")
	}
	println(sb.String())
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synbufio.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFile(p); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
}
