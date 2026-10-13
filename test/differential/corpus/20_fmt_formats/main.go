package main

import "fmt"

type Point struct{ X, Y int }

func main() {
	p := Point{3, 4}
	fmt.Printf("%v %+v %#v %T\n", p, p, p, p)
	fmt.Printf("%d %x %o %b\n", 255, 255, 255, 255)
	fmt.Printf("%f %.2f %e\n", 3.14159, 3.14159, 3.14159)
	fmt.Printf("%q %s\n", "héllo", "world")
	fmt.Printf("%p\n", &p) // address: normalized by the harness
	b := []byte{0xde, 0xad, 0xbe, 0xef}
	fmt.Printf("%x % X\n", b, b)
}
