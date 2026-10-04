package main

import "fmt"

var zi int
var zs string
var zb bool
var zf float64
var zsl []int
var zm map[string]int

func main() {
	fmt.Println(zi, zs, zb, zf)
	fmt.Println(zsl == nil, zm == nil, len(zsl), len(zm))
}
