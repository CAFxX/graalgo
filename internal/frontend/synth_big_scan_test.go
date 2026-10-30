package main

// Review 2026-10-30 P1-13: math/big Scan methods were runtime panic
// traps ("Scan not supported in frontend/v1 scope") with no
// compile-time gate, because Scan is single-result and the
// multi-result fail-fast only covers multi-result functions — so user
// code calling fmt.Fscan into a big.Int type-checked cleanly and then
// panicked at runtime where gc succeeds. The fix restores the upstream
// Scan bodies (Float/Int delegate to the pair-transformed z.scan via
// byteReader; Rat.Scan calls s.Token; byteReader.ReadByte calls
// ScanState.ReadRune). These tests pin that:
//
//   - the four restored bodies contain their upstream markers and no
//     "Scan not supported" panic (fails on the pre-fix source);
//   - a program calling all three Scan methods, directly and through
//     the fmt.Scanner interface, exports without error — proving the
//     new bodies (notably ScanState's multi-result ReadRune/Token
//     interface calls and the z.scan desugar) are emittable.
//
// The two remaining "Scan not supported" strings in the synthetic
// source are the byteReader.ReadRune/Token stubs, which nothing calls
// (ReadByte goes through ScanState.ReadRune directly, Rat.Scan through
// ScanState.Token directly); the count check pins that no other Scan
// trap remains.

import (
	"strings"
	"testing"
)

func bigScanBody(t *testing.T, sig string) string {
	t.Helper()
	i := strings.Index(syntheticMathBigSource, sig)
	if i < 0 {
		t.Fatalf("synthetic math/big source has no %s", sig)
	}
	rest := syntheticMathBigSource[i:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("cannot find end of %s", sig)
	}
	return rest[:end]
}

func TestBigScanBodiesRestored(t *testing.T) {
	cases := []struct{ sig, marker string }{
		{"func (z *Float) Scan(s fmt.ScanState, ch rune) error {", "z.scan(byteReader{s}, 0)"},
		{"func (z *Int) Scan(s fmt.ScanState, ch rune) error {", "z.scan(byteReader{s}, base)"},
		{"func (z *Rat) Scan(s fmt.ScanState, ch rune) error {", "s.Token(true, ratTok)"},
		{"func __mrbyteReader_ReadByte(r byteReader, ) __mrbyteReader_ReadBytePair {", "r.ScanState.ReadRune()"},
	}
	for _, c := range cases {
		body := bigScanBody(t, c.sig)
		if strings.Contains(body, "Scan not supported") {
			t.Errorf("%s still contains the Scan panic trap", c.sig)
		}
		if !strings.Contains(body, c.marker) {
			t.Errorf("%s missing upstream marker %q", c.sig, c.marker)
		}
	}
	// Only the two unreachable byteReader stubs may still mention it.
	if n := strings.Count(syntheticMathBigSource, "Scan not supported"); n != 2 {
		t.Errorf("synthetic math/big source has %d 'Scan not supported' strings, want 2 (byteReader.ReadRune/Token stubs only)", n)
	}
}

func TestBigScanExports(t *testing.T) {
	src := `package main

import (
	"fmt"
	"math/big"
)

func main() {
	var s fmt.ScanState
	var i big.Int
	var f big.Float
	var r big.Rat
	_ = i.Scan(s, 'd')
	_ = f.Scan(s, 'g')
	_ = r.Scan(s, 'g')
	var sc fmt.Scanner = &i
	_ = sc.Scan(s, 'd')
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("program calling big Scan methods should export, got: %v", err)
	}
}
