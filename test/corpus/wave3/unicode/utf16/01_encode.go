package main

import (
	"fmt"
	"unicode/utf16"
)

func main() {
	// utf16.RuneLen, Encode, Decode
	fmt.Println(utf16.RuneLen('é')) // 1 (BMP)
	fmt.Println(utf16.RuneLen('𝄞')) // 2 (surrogate pair)
	s := utf16.Encode([]rune("héllo"))
	fmt.Println(len(s)) // 5
	d := utf16.Decode(s)
	fmt.Println(string(d)) // héllo
}
