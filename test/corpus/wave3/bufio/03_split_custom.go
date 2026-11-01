package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
)

// Scanner with custom split functions (P1-1, REVIEW-2026-11-01-21):
// a named split, a func-lit split, a split returning an error, and a
// split returning ErrFinalToken. Before the fix, every Split call
// panicked at guest runtime on first Scan; custom splits were the
// stated WS D rationale, but the general multi-result func-field path
// now serves them too — so they must work, not fail at build time.
func commaSplit(data []byte, atEOF bool) (advance int, token []byte, err error) {
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

var errBoom = errors.New("boom")

func main() {
	// Named custom split.
	s := bufio.NewScanner(bytes.NewReader([]byte("a,b,,c")))
	s.Split(commaSplit)
	for s.Scan() {
		fmt.Printf("comma %q\n", s.Text())
	}
	fmt.Println("comma err:", s.Err())

	// Func-lit custom split (semicolons).
	s2 := bufio.NewScanner(bytes.NewReader([]byte("x;y;z")))
	s2.Split(func(data []byte, atEOF bool) (int, []byte, error) {
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
	for s2.Scan() {
		fmt.Printf("semi %q\n", s2.Text())
	}
	fmt.Println("semi err:", s2.Err())

	// Custom split returning an error: Scan stops, Err reports it.
	s3 := bufio.NewScanner(bytes.NewReader([]byte("ok bad")))
	s3.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if len(data) >= 2 && string(data[:2]) == "ok" {
			return 2, data[:2], nil
		}
		if len(data) > 0 {
			return 0, nil, errBoom
		}
		return 0, nil, nil
	})
	for s3.Scan() {
		fmt.Printf("errtok %q\n", s3.Text())
	}
	fmt.Println("errsplit err:", s3.Err())

	// Custom split returning ErrFinalToken with a final token.
	s4 := bufio.NewScanner(bytes.NewReader([]byte("last")))
	s4.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if len(data) > 0 {
			return len(data), data, bufio.ErrFinalToken
		}
		return 0, nil, nil
	})
	for s4.Scan() {
		fmt.Printf("final %q\n", s4.Text())
	}
	fmt.Println("final err:", s4.Err())
	fmt.Println("split custom ok")
}
