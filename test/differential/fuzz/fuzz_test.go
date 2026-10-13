// Native differential fuzz targets (GG-0037(i)): testing.F fuzzing of
// pure stdlib functions with identical corpora under gc and graalgo.
//
// Each FuzzXxx target grows a corpus in testdata/fuzz/FuzzXxx/ via
// `go test -fuzz`. The replay driver (replay.py) materializes every corpus
// entry as a single-file program and runs it differential gc vs graalgo
// through the shared diffharness oracle. A divergence is a graalgo bug
// (the functions are pure; inputs are fixed seeds + fuzzer mutations).
//
// The fuzzer's mutation engine supplies input diversity; the differential
// replay is the oracle. New targets: add a FuzzXxx here + a replay template
// in replay.py's TEMPLATES table.
package fuzz

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func FuzzStringsToUpper(f *testing.F) {
	for _, s := range []string{"", "hello", "HELLO", "ß", "ﬁ", "\xff\xfe", "aBc123"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _ = strings.ToUpper(s) })
}

func FuzzStringsToLower(f *testing.F) {
	for _, s := range []string{"", "hello", "HELLO", "İ", "Σ", "\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _ = strings.ToLower(s) })
}

func FuzzStringsTrimSpace(f *testing.F) {
	for _, s := range []string{"", "  x  ", "\t\nx\r\n", " ", "x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _ = strings.TrimSpace(s) })
}

func FuzzStringsCount(f *testing.F) {
	for _, p := range [][2]string{{"", ""}, {"hello", "l"}, {"aaa", "aa"}, {"x", ""}} {
		f.Add(p[0], p[1])
	}
	f.Fuzz(func(t *testing.T, s, sub string) { _ = strings.Count(s, sub) })
}

func FuzzStringsReplaceAll(f *testing.F) {
	for _, p := range [][3]string{{"", "", ""}, {"hello", "l", "L"}, {"aaa", "aa", "b"}} {
		f.Add(p[0], p[1], p[2])
	}
	f.Fuzz(func(t *testing.T, s, old, new string) { _ = strings.ReplaceAll(s, old, new) })
}

func FuzzStrconvAtoi(f *testing.F) {
	for _, s := range []string{"0", "42", "-42", "  42", "0x2a", "42x", "", "9999999999999999999999"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _, _ = strconv.Atoi(s) })
}

func FuzzStrconvItoa(f *testing.F) {
	for _, n := range []int{0, 1, -1, 42, 1 << 62, -1 << 62} {
		f.Add(n)
	}
	f.Fuzz(func(t *testing.T, n int) { _ = strconv.Itoa(n) })
}

func FuzzBytesToUpper(f *testing.F) {
	for _, b := range [][]byte{{}, []byte("hello"), []byte("HELLO"), {0xff, 0xfe}} {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) { _ = bytes.ToUpper(b) })
}
