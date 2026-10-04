package main

import "fmt"

const (
	_ = 1 << iota
	Read
	Write
	Exec
)

type Level int

const (
	Debug Level = iota
	Info
	Warning
	Error
)

const (
	KB = 1 << (10 * iota)
	MB
	GB
)

func main() {
	fmt.Println(Read, Write, Exec)
	fmt.Println(Debug, Info, Warning, Error)
	fmt.Println(KB, MB, GB)
}
