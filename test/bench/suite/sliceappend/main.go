// Slice growth and copying: append from nil, copy, reslicing.
package main

import "graalgo/bench/h"

func work() int {
	var s []int
	for i := 0; i < 500_000; i++ {
		s = append(s, i)
	}
	d := make([]int, len(s))
	copy(d, s)
	var b []byte
	for i := 0; i < 200_000; i++ {
		b = append(b, byte(i))
	}
	t := 0
	for i := 0; i+8 <= len(d); i += 8 {
		w := d[i : i+8]
		t += w[0] + w[7]
	}
	return t + len(b) + int(b[len(b)-1])
}

func main() { h.Run("sliceappend", 30, work) }
