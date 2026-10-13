package main

import "fmt"

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
	fmt.Println(Debug, Info, Warning, Error)
	fmt.Println(KB, MB, GB)
	fmt.Println(Error > Info)
}
