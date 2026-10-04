package main

import (
	"fmt"
	"strconv"
)

func main() {
	fmt.Println(string(rune(0x1F600)))
	fmt.Println([]byte("abc"))
	fmt.Println(string([]byte{104, 105}))
	fmt.Println(strconv.Itoa(-42))
	n, err := strconv.Atoi("1234")
	fmt.Println(n, err)
	f := 3.99
	fmt.Println(int(f), float64(7)/2)
	var i int = 65
	fmt.Println(string(rune(i))) // "A" — explicit rune conversion
	b := []rune("héllo")
	fmt.Println(len(b), string(b))
}
