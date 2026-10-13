package main

import (
	"bytes"
	"fmt"
)

func main() {
	// Equal
	fmt.Println(bytes.Equal([]byte("hello"), []byte("hello")))
	fmt.Println(bytes.Equal([]byte("hello"), []byte("world")))
	fmt.Println(bytes.Equal(nil, []byte{}))
	fmt.Println(bytes.Equal(nil, nil))

	// Compare
	fmt.Println(bytes.Compare([]byte("a"), []byte("b")))
	fmt.Println(bytes.Compare([]byte("b"), []byte("a")))
	fmt.Println(bytes.Compare([]byte("a"), []byte("a")))
	fmt.Println(bytes.Compare([]byte("abc"), []byte("abd")))

	// Count
	fmt.Println(bytes.Count([]byte("mississippi"), []byte("ss")))
	fmt.Println(bytes.Count([]byte("hello"), []byte("")))
	fmt.Println(bytes.Count([]byte("aaaa"), []byte("aa")))

	// Contains family (ContainsFunc needs a func value: runtime crash, see README)
	fmt.Println(bytes.Contains([]byte("hello"), []byte("ll")))
	fmt.Println(bytes.Contains([]byte("hello"), []byte("xx")))
	fmt.Println(bytes.ContainsAny([]byte("hello"), "aeiou"))
	fmt.Println(bytes.ContainsAny([]byte("xyz"), "aeiou"))
	fmt.Println(bytes.ContainsRune([]byte("hello"), 'l'))
	fmt.Println(bytes.ContainsRune([]byte("hello"), 'x'))
	fmt.Println(bytes.ContainsRune([]byte("héllo"), 'é'))

	// HasPrefix/HasSuffix
	fmt.Println(bytes.HasPrefix([]byte("hello"), []byte("he")))
	fmt.Println(bytes.HasPrefix([]byte("hello"), []byte("xx")))
	fmt.Println(bytes.HasSuffix([]byte("hello"), []byte("lo")))
	fmt.Println(bytes.HasSuffix([]byte("hello"), []byte("xx")))

	// EqualFold (ASCII fast path; the unicode path uses the fold table:
	// runtime crash, see README)
	fmt.Println(bytes.EqualFold([]byte("hello"), []byte("HELLO")))
	fmt.Println(bytes.EqualFold([]byte("abc"), []byte("abd")))

	// Clone
	c := bytes.Clone([]byte("hello"))
	fmt.Println(string(c))
	fmt.Println(bytes.Clone(nil) == nil)

	// MinRead const / ErrTooLarge var
	fmt.Println(bytes.MinRead)
	fmt.Println(bytes.ErrTooLarge.Error())
}
