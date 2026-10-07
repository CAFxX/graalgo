package main

import (
	"bytes"
	"fmt"
)

func main() {
	// Index family (IndexFunc/LastIndexFunc need func values: runtime crash, see README)
	fmt.Println(bytes.Index([]byte("hello"), []byte("ll")))
	fmt.Println(bytes.Index([]byte("hello"), []byte("xx")))
	fmt.Println(bytes.IndexByte([]byte("hello"), 'l'))
	fmt.Println(bytes.IndexByte([]byte("hello"), 'x'))
	fmt.Println(bytes.IndexRune([]byte("héllo"), 'é'))
	fmt.Println(bytes.IndexRune([]byte("hello"), 'é'))
	fmt.Println(bytes.IndexAny([]byte("hello"), "aeiou"))
	fmt.Println(bytes.IndexAny([]byte("xyz"), "aeiou"))
	fmt.Println(bytes.LastIndex([]byte("ababa"), []byte("aba")))
	fmt.Println(bytes.LastIndex([]byte("hello"), []byte("xx")))
	fmt.Println(bytes.LastIndexByte([]byte("hello"), 'l'))
	fmt.Println(bytes.LastIndexByte([]byte("hello"), 'x'))
	fmt.Println(bytes.LastIndexAny([]byte("hello"), "aeiou"))
	fmt.Println(bytes.LastIndexAny([]byte("xyz"), "aeiou"))

	// Edge cases: empty needle/haystack
	fmt.Println(bytes.Index([]byte(""), []byte("")))
	fmt.Println(bytes.Index([]byte("hello"), []byte("")))
	fmt.Println(bytes.Index([]byte(""), []byte("x")))
	fmt.Println(bytes.LastIndex([]byte(""), []byte("")))
	fmt.Println(bytes.IndexByte(nil, 'a'))
}
