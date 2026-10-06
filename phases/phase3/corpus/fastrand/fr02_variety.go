package main

import (
	"fmt"
	_ "unsafe"
)

//go:linkname fastrand runtime.fastrand
func fastrand() uint32

func main() {
	// 1000 consecutive fastrand values are not all equal (a stuck RNG
	// fails; a working one passes with overwhelming probability).
	first := fastrand()
	allSame := true
	for i := 0; i < 1000; i++ {
		if fastrand() != first {
			allSame = false
			break
		}
	}
	fmt.Println("varied:", !allSame)

	// Low bits vary too (not just high bits).
	seen := map[uint32]bool{}
	for i := 0; i < 100; i++ {
		seen[fastrand()%16] = true
	}
	fmt.Println("low bits vary:", len(seen) > 8)
}
