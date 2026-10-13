package main

import (
	"fmt"
	"unicode"
)

func main() {
	// unicode.IsLetter, IsDigit, IsSpace
	fmt.Println(unicode.IsLetter('é')) // true
	fmt.Println(unicode.IsLetter('1')) // false
	fmt.Println(unicode.IsDigit('5'))  // true
	fmt.Println(unicode.IsDigit('a'))  // false
	fmt.Println(unicode.IsSpace(' '))  // true
	fmt.Println(unicode.IsSpace('a'))  // false
}
