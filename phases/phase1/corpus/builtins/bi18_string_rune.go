package main

import "fmt"

// string([]rune) with invalid code points (R1 P1-3): gc substitutes U+FFFD;
// the old code threw IllegalArgumentException. Also []byte round-trips.
func main() {
	fmt.Printf("%q\n", string([]rune{0xD800, 0x110000, -1, 65}))
	fmt.Printf("%q\n", string([]rune{'h', 'i', 0x1F600}))
	fmt.Printf("%q\n", string([]byte{104, 105}))
	fmt.Printf("%q\n", string([]rune{}))
	fmt.Println([]rune("hi"))
	fmt.Println(string(0x110000))
	fmt.Println(string(-1))
}
