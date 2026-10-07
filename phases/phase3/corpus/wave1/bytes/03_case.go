package main

import (
	"bytes"
	"fmt"
)

func main() {
	// Case mapping, ASCII fast path (non-ASCII falls back to Map with a
	// func value: runtime crash, see README; Map/ToTitle/Title likewise)
	fmt.Println(string(bytes.ToUpper([]byte("hello"))))
	fmt.Println(string(bytes.ToUpper([]byte("HELLO"))))
	fmt.Println(string(bytes.ToUpper([]byte(""))))
	fmt.Println(string(bytes.ToLower([]byte("HELLO"))))
	fmt.Println(string(bytes.ToLower([]byte("hello"))))
	fmt.Println(string(bytes.ToLower([]byte("HeLLo WoRLd"))))

	// ToValidUTF8 (valid input only; invalid input hits a runtime crash,
	// see README)
	fmt.Println(string(bytes.ToValidUTF8([]byte("hello"), []byte("?"))))
	fmt.Println(string(bytes.ToValidUTF8([]byte(""), []byte("?"))))

	// (Runes crashes at runtime even on ASCII input: see README.)
}
