package main

// P1-1 (REVIEW-2026-11-01-21): bufio.Scanner.Split panicked at guest
// runtime for every split function. The synthetic Scan() body used to
// panic whenever s.split != nil (WS D: v1 could not call through a
// 3-result function value), so Split(ScanWords) — and even
// Split(ScanLines), the documented default — compiled cleanly and died
// on first Scan.
//
// The fix is the general multi-result func-field path, not a bufio
// special case:
//   - Scan() is gc-verbatim again: advance, token, err :=
//     s.split(...), desugared by classifyGeneralFuncField (which now
//     also unwraps named func types such as bufio.SplitFunc).
//   - NewScanner is gc-verbatim again (split: ScanLines); the bare
//     ident lowers to the __bufioScanLines helper via the same
//     table-driven value lowering the package-qualified path already
//     performed (exportFuncValue fallback).
//
// These tests pin the export shape, not just export success (export
// success alone is not discriminating: the pre-fix tree also exported
// Split calls fine and panicked only at runtime):
//   1. TestBufioSplitScanDesugarsFuncField fails on the pre-fix tree:
//      Scan's body then contained the WS-D panic string and no
//      selector-fun call (it called __bufioScanLines directly).
//   2. TestBufioSplitNewScannerDefault fails on the pre-fix tree:
//      NewScanner then stored no split at all (nil = default).

import (
	"strings"
	"testing"
)

const bufioSplitScanSrc = `package main

import (
	"bufio"
	"strings"
)

func main() {
	s := bufio.NewScanner(strings.NewReader("a b c"))
	s.Split(bufio.ScanWords)
	for s.Scan() {
		println(s.Text())
	}
}
`

// bufioFuncDecl returns the wire funcdecl named name in package pkgpath,
// or fails the test.
func bufioFuncDecl(t *testing.T, doc map[string]any, pkgpath, name string) map[string]any {
	t.Helper()
	for _, f := range findAllKind(doc, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		if fl["name"] == name && fl["pkgpath"] == pkgpath {
			return fl
		}
	}
	t.Fatalf("no funcdecl %s in package %s", name, pkgpath)
	return nil
}

// wireHasFuncFieldDesugar reports whether the funcdecl body contains a
// pair temp (__p5mr_N) initialized by a call whose fun is a field-access
// selector — the desugarFuncFieldCall shape.
func wireHasFuncFieldDesugar(t *testing.T, body any) bool {
	t.Helper()
	foundPair, foundSelCall := false, false
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if v["kind"] == "vardecl" {
				if names, ok := v["fields"].(map[string]any)["names"].([]any); ok {
					for _, nm := range names {
						if ns, ok := nm.(string); ok && strings.HasPrefix(ns, "__p5mr_") {
							foundPair = true
						}
					}
				}
			}
			if v["kind"] == "call" {
				if fun, ok := v["fields"].(map[string]any)["fun"].(map[string]any); ok && fun["kind"] == "selector" {
					foundSelCall = true
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(body)
	return foundPair && foundSelCall
}

func TestBufioSplitScanDesugarsFuncField(t *testing.T) {
	out, err := ExportFile(writeIfaceProg(t, bufioSplitScanSrc))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if strings.Contains(out, "custom SplitFunc is not supported") {
		t.Fatal("wire still contains the WS-D split panic")
	}
	doc := decodeExport(t, out)
	scan := bufioFuncDecl(t, doc, "bufio", "Scan")
	if !wireHasFuncFieldDesugar(t, scan["body"]) {
		t.Fatal("bufio Scan body has no func-field desugar (no __p5mr_ pair temp + selector-fun call for s.split)")
	}
}

func TestBufioSplitNewScannerDefault(t *testing.T) {
	out, err := ExportFile(writeIfaceProg(t, bufioSplitScanSrc))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	ns := bufioFuncDecl(t, doc, "bufio", "NewScanner")
	// NewScanner's composite literal must store the default split as
	// the pair-returning helper (bare-ident table lowering); the
	// pre-fix tree stored nothing here.
	body, _ := ns["body"].(map[string]any)
	found := false
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if v["kind"] == "ident" {
				if name, _ := v["fields"].(map[string]any)["name"].(string); name == "__bufioScanLines" {
					found = true
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(body)
	if !found {
		t.Fatal("bufio NewScanner body has no __bufioScanLines ident (default split not stored)")
	}
}

// TestBufioSplitVariantsExport pins that every split form the lane
// claims to support exports without error: each built-in, a named
// custom split, a func-lit custom split, and the default (no Split).
// Export success is necessary but not sufficient — the desugar shape
// is pinned by TestBufioSplitScanDesugarsFuncField above; these cases
// pin that no variant regresses to an export error.
func TestBufioSplitVariantsExport(t *testing.T) {
	variants := map[string]string{
		"ScanLines":   `s.Split(bufio.ScanLines)`,
		"ScanWords":   `s.Split(bufio.ScanWords)`,
		"ScanRunes":   `s.Split(bufio.ScanRunes)`,
		"ScanBytes":   `s.Split(bufio.ScanBytes)`,
		"namedCustom": `s.Split(commaSplit)`,
		"litCustom":   `s.Split(func(data []byte, atEOF bool) (int, []byte, error) { return len(data), data, nil })`,
		"default":     ``,
	}
	for name, split := range variants {
		src := "package main\n\nimport (\n\"bufio\"\n\"strings\"\n)\n\nfunc commaSplit(data []byte, atEOF bool) (int, []byte, error) {\n\tif atEOF && len(data) == 0 {\n\t\treturn 0, nil, nil\n\t}\n\tfor i, b := range data {\n\t\tif b == ',' {\n\t\t\treturn i + 1, data[:i], nil\n\t\t}\n\t}\n\tif atEOF {\n\t\treturn len(data), data, nil\n\t}\n\treturn 0, nil, nil\n}\n\nfunc main() {\n\ts := bufio.NewScanner(strings.NewReader(\"a,b;c\"))\n\t" + split + "\n\tfor s.Scan() {\n\t\tprintln(s.Text())\n\t}\n}\n"
		if _, err := ExportFile(writeIfaceProg(t, src)); err != nil {
			t.Errorf("variant %s: export failed: %v", name, err)
		}
	}
}
