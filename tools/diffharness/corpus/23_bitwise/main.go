package main

import "fmt"

const (
	_ = 1 << iota
	Read
	Write
	Exec
)

func main() {
	var flags int = Read | Write
	fmt.Println(flags, flags&Exec == 0)
	fmt.Printf("%08b\n", flags<<2)
	fmt.Println(^uint8(0), ^uint8(0)>>4)
	x := 0xDEADBEEF
	fmt.Println(x>>16, x&0xFFFF)
	fmt.Println(Read, Write, Exec)
}
