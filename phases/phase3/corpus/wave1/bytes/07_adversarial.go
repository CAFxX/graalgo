package main

import (
	"bytes"
	"fmt"
)

func main() {
	// Empty inputs
	fmt.Println(bytes.Contains([]byte(""), []byte("")))
	fmt.Println(bytes.HasPrefix([]byte(""), []byte("")))
	fmt.Println(bytes.Index([]byte(""), []byte("")))
	fmt.Println(bytes.Split([]byte(""), []byte(",")))
	fmt.Println(bytes.Fields([]byte("")))
	fmt.Println(string(bytes.TrimSpace([]byte(""))))
	fmt.Println(string(bytes.ToUpper([]byte(""))))
	fmt.Println(string(bytes.Repeat([]byte(""), 5)))
	fmt.Println(bytes.Count([]byte(""), []byte("")))
	fmt.Println(string(bytes.ToLower([]byte(""))))

	// Nil inputs
	fmt.Println(bytes.Equal(nil, nil))
	fmt.Println(string(bytes.ToLower(nil)))
	fmt.Println(bytes.Split(nil, []byte(",")))
	fmt.Println(string(bytes.Trim(nil, " ")))
	fmt.Println(bytes.IndexByte(nil, 'a'))

	// Unicode edge cases (Index on multibyte works; Count with empty sep
	// on multibyte hits a runtime crash, see README)
	fmt.Println(bytes.Index([]byte("héllo"), []byte("é")))
	fmt.Println(bytes.Contains([]byte("héllo"), []byte("é")))

	// Malformed UTF-8: truncated, surrogates, overlong.
	// (ToValidUTF8 and Runes crash on invalid input: runtime bug, see README.
	// IndexRune/ContainsRune/Count/Index handle it fine.)
	bad := []byte{0xff, 0xfe, 0xc0, 0xaf, 0xed, 0xa0, 0x80, 'a', 0xe2, 0x82}
	fmt.Println(bytes.IndexRune(bad, 'a'))
	fmt.Println(bytes.ContainsRune(bad, 'a'))
	fmt.Println(bytes.ContainsRune(bad, 'z'))
	fmt.Println(bytes.Count(bad, []byte{0xff}))
	fmt.Println(bytes.Index(bad, []byte{0xff}))
	fmt.Println(bytes.IndexByte(bad, 'a'))

	// Huge input (moderate size for test speed)
	big := bytes.Repeat([]byte("a"), 100000)
	fmt.Println(len(big))
	fmt.Println(bytes.Count(big, []byte("a")))
	fmt.Println(bytes.Index(big, []byte("a")))
	fmt.Println(bytes.LastIndex(big, []byte("a")))
	fmt.Println(string(bytes.ToUpper(bytes.Repeat([]byte("a"), 3))))
	f := bytes.Fields(bytes.Repeat([]byte("a b "), 1000))
	fmt.Println(len(f))
	fmt.Println(string(bytes.Join(f[:3], []byte("-"))))
}
