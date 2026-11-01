// Regression tests for P1-1 (REVIEW-2026-11-01-21): bufio.Scanner.Split
// panicked at guest runtime for every split function. The synthetic
// Scan() body panicked whenever s.split != nil, so Split(ScanWords) —
// and even Split(ScanLines), the documented default — compiled cleanly
// and died on first Scan. The fix restores gc-verbatim Scan()/NewScanner
// and serves the split through the general multi-result func-field
// desugar (named func types unwrapped; bare-ident table-driven values
// lowered to their __-helpers).
//
// Pre-fix, every test below that calls Split fails with the WS-D panic
// "bufio.Scanner: custom SplitFunc is not supported in frontend/v1
// scope (WS D)" on first Scan; the default test passed pre-fix (nil
// split took the __bufioScanLines path) and pins no regression there.
package bufiosplit

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func collect(t *testing.T, s *bufio.Scanner) []string {
	t.Helper()
	var got []string
	for s.Scan() {
		got = append(got, s.Text())
	}
	if err := s.Err(); err != nil {
		t.Fatalf("scanner Err() = %v, want nil", err)
	}
	return got
}

func equal(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tokens = %q, want %q", got, want)
		}
	}
}

func TestSplitDefault(t *testing.T) {
	s := bufio.NewScanner(strings.NewReader("x\ny"))
	equal(t, collect(t, s), []string{"x", "y"})
}

func TestSplitScanLines(t *testing.T) {
	s := bufio.NewScanner(strings.NewReader("one\r\n\r\nthree"))
	s.Split(bufio.ScanLines)
	equal(t, collect(t, s), []string{"one", "", "three"})
}

func TestSplitScanWords(t *testing.T) {
	s := bufio.NewScanner(strings.NewReader("  a\tb\nc  d "))
	s.Split(bufio.ScanWords)
	equal(t, collect(t, s), []string{"a", "b", "c", "d"})
}

func TestSplitScanRunes(t *testing.T) {
	s := bufio.NewScanner(strings.NewReader("a\xc3\xa9\xffz"))
	s.Split(bufio.ScanRunes)
	equal(t, collect(t, s), []string{"a", "\u00e9", "\ufffd", "z"})
}

func TestSplitScanBytes(t *testing.T) {
	s := bufio.NewScanner(strings.NewReader("ab"))
	s.Split(bufio.ScanBytes)
	equal(t, collect(t, s), []string{"a", "b"})
}

func commaSplit(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, b := range data {
		if b == ',' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func TestSplitNamedCustom(t *testing.T) {
	s := bufio.NewScanner(strings.NewReader("a,b,,c"))
	s.Split(commaSplit)
	equal(t, collect(t, s), []string{"a", "b", "", "c"})
}

func TestSplitLitCustom(t *testing.T) {
	s := bufio.NewScanner(strings.NewReader("x;y;z"))
	s.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		for i, b := range data {
			if b == ';' {
				return i + 1, data[:i], nil
			}
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	equal(t, collect(t, s), []string{"x", "y", "z"})
}

func TestSplitCustomError(t *testing.T) {
	errBoom := errors.New("boom")
	s := bufio.NewScanner(strings.NewReader("ok bad"))
	s.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if len(data) >= 2 && string(data[:2]) == "ok" {
			return 2, data[:2], nil
		}
		if len(data) > 0 {
			return 0, nil, errBoom
		}
		return 0, nil, nil
	})
	var got []string
	for s.Scan() {
		got = append(got, s.Text())
	}
	equal(t, got, []string{"ok"})
	if err := s.Err(); err != errBoom {
		t.Fatalf("scanner Err() = %v, want boom", err)
	}
}

func TestSplitCustomFinalToken(t *testing.T) {
	s := bufio.NewScanner(strings.NewReader("last"))
	s.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if len(data) > 0 {
			return len(data), data, bufio.ErrFinalToken
		}
		return 0, nil, nil
	})
	equal(t, collect(t, s), []string{"last"})
}
