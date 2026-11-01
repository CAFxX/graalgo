package main

import (
	"bufio"
	"bytes"
	"fmt"
)

// Scanner with every built-in split function (P1-1,
// REVIEW-2026-11-01-21): ScanWords, ScanLines, ScanRunes, ScanBytes,
// plus the default (no Split call, i.e. ScanLines). Before the fix,
// any Split call panicked at guest runtime on first Scan — even
// Split(ScanLines), the documented default.
func main() {
	// ScanWords: runs of non-space, incl. tabs/newlines and edges.
	s := bufio.NewScanner(bytes.NewReader([]byte("  a\tb\nc  d ")))
	s.Split(bufio.ScanWords)
	for s.Scan() {
		fmt.Printf("words %q\n", s.Text())
	}
	fmt.Println("words err:", s.Err())

	// ScanLines, explicit: CRLF, empty lines, final line w/o newline.
	s2 := bufio.NewScanner(bytes.NewReader([]byte("one\r\n\r\nthree")))
	s2.Split(bufio.ScanLines)
	for s2.Scan() {
		fmt.Printf("lines %q\n", s2.Text())
	}
	fmt.Println("lines err:", s2.Err())

	// ScanRunes: ASCII, multi-byte, and an invalid byte (U+FFFD).
	s3 := bufio.NewScanner(bytes.NewReader([]byte("a\xc3\xa9\xffz")))
	s3.Split(bufio.ScanRunes)
	for s3.Scan() {
		fmt.Printf("runes %q\n", s3.Text())
	}
	fmt.Println("runes err:", s3.Err())

	// ScanBytes: every byte as a token, incl. empty input.
	s4 := bufio.NewScanner(bytes.NewReader([]byte("ab")))
	s4.Split(bufio.ScanBytes)
	for s4.Scan() {
		fmt.Printf("bytes %q\n", s4.Bytes())
	}
	fmt.Println("bytes err:", s4.Err())
	s5 := bufio.NewScanner(bytes.NewReader([]byte("")))
	s5.Split(bufio.ScanBytes)
	fmt.Println("bytes-empty scan:", s5.Scan(), "err:", s5.Err())

	// Default: no Split call at all.
	s6 := bufio.NewScanner(bytes.NewReader([]byte("x\ny")))
	for s6.Scan() {
		fmt.Printf("default %q\n", s6.Text())
	}
	fmt.Println("default err:", s6.Err())
	fmt.Println("split builtins ok")
}
