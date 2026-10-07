package main

import (
	"bytes"
	"fmt"
)

func main() {
	// Split family (empty separator hits a runtime crash, see README;
	// FieldsFunc needs a func value: runtime crash)
	fmt.Println(bytes.Split([]byte("a,b,c"), []byte(",")))
	fmt.Println(bytes.Split([]byte("a,b,c"), []byte(":")))
	fmt.Println(bytes.SplitN([]byte("a,b,c"), []byte(","), 2))
	fmt.Println(bytes.SplitN([]byte("a,b,c"), []byte(","), 0))
	fmt.Println(bytes.SplitN([]byte("a,b,c"), []byte(","), -1))
	fmt.Println(bytes.SplitAfter([]byte("a,b,c"), []byte(",")))
	fmt.Println(bytes.SplitAfterN([]byte("a,b,c"), []byte(","), 2))
	fmt.Println(bytes.Split([]byte(""), []byte(",")))
	fmt.Println(bytes.Split([]byte("a,,b"), []byte(",")))

	// Fields (inlined asciiSpace fast path; no func value crosses the boundary)
	fmt.Println(bytes.Fields([]byte("  foo bar  baz   ")))
	fmt.Println(bytes.Fields([]byte("")))
	fmt.Println(bytes.Fields([]byte("a\tb\nc\rd")))

	// Join
	fmt.Println(string(bytes.Join([][]byte{[]byte("a"), []byte("b"), []byte("c")}, []byte(","))))
	fmt.Println(string(bytes.Join(nil, []byte(","))))
	fmt.Println(string(bytes.Join([][]byte{[]byte("x")}, []byte(","))))

	// Replace family
	fmt.Println(string(bytes.Replace([]byte("hello"), []byte("l"), []byte("L"), 1)))
	fmt.Println(string(bytes.Replace([]byte("hello"), []byte("l"), []byte("L"), -1)))
	fmt.Println(string(bytes.Replace([]byte("hello"), []byte("l"), []byte("L"), 0)))
	fmt.Println(string(bytes.ReplaceAll([]byte("hello"), []byte("l"), []byte("L"))))
	// (Replace with empty old hits a runtime crash, see README)
	fmt.Println(string(bytes.Replace([]byte("oink"), []byte("o"), []byte("00"), -1)))

	// Repeat
	fmt.Println(string(bytes.Repeat([]byte("ab"), 3)))
	fmt.Println(string(bytes.Repeat([]byte(""), 5)))
	fmt.Println(string(bytes.Repeat([]byte("ab"), 0)))
	fmt.Println(string(bytes.Repeat([]byte("é"), 2)))
}
