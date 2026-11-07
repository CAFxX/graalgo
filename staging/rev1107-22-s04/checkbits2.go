package main

import (
	"fmt"
	"math"
)

func main() {
	for _, b := range []uint64{
		0x3F947AE147AE147B, 0x3F947AE147AE147C, 0x3FA999999999999A, 0x3F9999999999999A,
		0x3FB999999999999A, 0x3F847AE147AE147B, 0x3F83E42568A1F5C3, 0x3F86F9DB22D0E560,
		0x3F5A7EF9DB22D0E5, 0x3F4C823C79A6D5E5, 0x3F70000000000000,
	} {
		fmt.Printf("0x%016X -> %v\n", b, math.Float64frombits(b))
	}
	for _, v := range []float64{0.005, 0.004, 0.006, 0.0006, 0.00006, 0.00390625, 0.05, 0.025} {
		fmt.Printf("%v -> 0x%016X\n", v, math.Float64bits(v))
	}
}
