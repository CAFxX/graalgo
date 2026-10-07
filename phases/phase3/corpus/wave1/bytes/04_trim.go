package main

import (
	"bytes"
	"fmt"
)

func main() {
	// Trim with cutset (TrimFunc/TrimLeftFunc/TrimRightFunc need func
	// values: runtime crash, see README; Title uses Map: runtime crash)
	fmt.Println(string(bytes.Trim([]byte("  hello  "), " ")))
	fmt.Println(string(bytes.Trim([]byte("!!hello!!"), "!")))
	fmt.Println(string(bytes.TrimLeft([]byte("  hello  "), " ")))
	fmt.Println(string(bytes.TrimRight([]byte("  hello  "), " ")))
	fmt.Println(string(bytes.Trim([]byte(""), " ")))
	fmt.Println(string(bytes.Trim([]byte("hello"), "")))
	fmt.Println(string(bytes.Trim([]byte("cutset"), "cst")))
	fmt.Println(string(bytes.TrimLeft([]byte("aabhello"), "ab")))
	fmt.Println(string(bytes.TrimRight([]byte("helloaab"), "ab")))

	// TrimPrefix/TrimSuffix
	fmt.Println(string(bytes.TrimPrefix([]byte("--hello"), []byte("--"))))
	fmt.Println(string(bytes.TrimPrefix([]byte("hello"), []byte("--"))))
	fmt.Println(string(bytes.TrimSuffix([]byte("hello--"), []byte("--"))))
	fmt.Println(string(bytes.TrimSuffix([]byte("hello"), []byte("--"))))
	fmt.Println(string(bytes.TrimPrefix([]byte(""), []byte("--"))))

	// TrimSpace (ASCII whitespace only; unicode whitespace needs the
	// space table: runtime crash, see README)
	fmt.Println(string(bytes.TrimSpace([]byte(" \t\n hello \r\n"))))
	fmt.Println(string(bytes.TrimSpace([]byte(""))))
}
