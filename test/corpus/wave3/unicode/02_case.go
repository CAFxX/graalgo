package main

import (
	"fmt"
	"unicode"
)

func main() {
	// unicode.ToUpper, ToLower, ToTitle
	fmt.Println(string(unicode.ToUpper('é'))) // É
	fmt.Println(string(unicode.ToLower('É'))) // é
	fmt.Println(string(unicode.ToTitle('a'))) // A
	fmt.Println(string(unicode.ToUpper('a'))) // A
}
