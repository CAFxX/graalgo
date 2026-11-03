package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSyntheticTextScannerUnicode is the REVIEW-2026-11-03-16 S9
// regression test: text/scanner's IsLetter/IsDigit were ASCII-only and
// its utf8 handling was inlined __ts* helpers, pending the full unicode
// tables landing in the overlay. The tables landed without anyone
// re-porting the package, so non-ASCII identifiers scanned wrong. The
// package is now gc-verbatim, importing the synthetic unicode and
// unicode/utf8 packages directly. This test pins export of a program
// that scans a non-ASCII identifier (café) and a non-ASCII digit
// context through the scanner; execution-level token assertions live
// in the lane's sync-run verification (scanner must return one Ident
// token for café, not caf + an error token).
func TestSyntheticTextScannerUnicode(t *testing.T) {
	src := `package main

import (
	"strings"
	"text/scanner"
)

func main() {
	var s scanner.Scanner
	s.Init(strings.NewReader("café x2"))
	s.Mode = scanner.ScanIdents
	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		println(s.TokenText())
	}
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synscanner.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFile(p); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
}
