package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyntheticMathBigScan (R08-S-04) verifies the synthetic math/big
// Scan methods are real implementations, not panic-first stubs:
//
//   - Float.Scan / Int.Scan / Rat.Scan execute the upstream bodies
//     (via the desugared __mrFloat_scan / __mrInt_scan / __mrRat_SetString
//     helpers and the byteReader ReadByte helper).
//   - The synthetic source contains no "Scan not supported" panic.
//   - A program that fmt.Sscanf's into all three big types exports
//     cleanly and its wire document contains no trace of the stub
//     panic (the Scan funcdecl bodies are emitted, so a panic-first
//     stub would leak its message string into the export).
//
// The behavioural (runtime) check lives in the e2e Sscanf round-trip:
// the same program run under graalgo must print the same values as gc.
// This test pins the frontend/export half: the Scan paths classify and
// export, and no stub panic survives into the emitted program.
func TestSyntheticMathBigScan(t *testing.T) {
	// (1) Source-level guard: the stub panic must be gone, and the live
	// bodies must be the upstream ones.
	if strings.Contains(syntheticMathBigSource, "Scan not supported") {
		t.Fatalf("syntheticMathBigSource still contains a 'Scan not supported' panic stub")
	}
	for _, want := range []string{
		"func (z *Float) Scan(s fmt.ScanState, ch rune) error {",
		"s.SkipSpace()",
		"z.scan(byteReader{s}, 0)",
		"func (z *Int) Scan(s fmt.ScanState, ch rune) error {",
		"z.scan(byteReader{s}, base)",
		"func (z *Rat) Scan(s fmt.ScanState, ch rune) error {",
		"tok, err := s.Token(true, ratTok)",
		"ch, size, err := r.ScanState.ReadRune()",
		"return __mrbyteReader_ReadBytePair{byte(ch), err}",
	} {
		if !strings.Contains(syntheticMathBigSource, want) {
			t.Errorf("syntheticMathBigSource missing live Scan body line: %q", want)
		}
	}
	for _, gone := range []string{
		"func (r byteReader) ReadRune() (rune, int, error) {",
		"func (r byteReader) Token(skipSpace bool, f func(rune) bool) ([]byte, error) {",
	} {
		if strings.Contains(syntheticMathBigSource, gone) {
			t.Errorf("syntheticMathBigSource still contains stubbed byteReader method: %q", gone)
		}
	}

	// (2) Export-level: Sscanf into all three big types.
	src := `package main

import (
	"fmt"
	"math/big"
)

func main() {
	var i big.Int
	n, err := fmt.Sscanf("123456789012345678901234567890", "%d", &i)
	fmt.Println(n, err, i.String())
	var f big.Float
	n, err = fmt.Sscanf("3.141592653589793238462643383279", "%g", &f)
	fmt.Println(n, err, f.Text('g', 30))
	var r big.Rat
	n, err = fmt.Sscanf("22/7", "%v", &r)
	fmt.Println(n, err, r.RatString())
	var h big.Int
	n, err = fmt.Sscanf("0xdeadbeef", "%x", &h)
	fmt.Println(n, err, h.String())
	var neg big.Int
	n, err = fmt.Sscanf("-42", "%d", &neg)
	fmt.Println(n, err, neg.String())
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synbigscan.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	// The Scan funcdecl bodies are part of the export; a panic-first
	// stub leaks its message string into the wire document.
	if strings.Contains(out, "Scan not supported") {
		t.Errorf("exported program still contains the 'Scan not supported' stub panic")
	}
	// The desugared scan helpers must be reachable from the export:
	// their presence proves the Scan paths (z.scan, byteReader.ReadByte,
	// Rat s.Token) classified instead of failing fast.
	for _, helper := range []string{"__mrFloat_scan", "__mrInt_scan", "__mrbyteReader_ReadByte"} {
		if !strings.Contains(out, helper) {
			t.Errorf("exported program missing desugared scan helper %q", helper)
		}
	}
}
