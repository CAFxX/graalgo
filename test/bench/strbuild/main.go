// Benchmark: strings.Builder appends and string indexing.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func work(n int) int {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("ab")
		b.WriteByte(byte('0' + i%10))
	}
	s := b.String()
	t := 0
	for i := 0; i < len(s); i++ {
		t += int(s[i])
	}
	return t + len(s)
}

func main() {
	var r int
	best := time.Duration(1 << 62)
	for i := 0; i < 15; i++ {
		t0 := time.Now()
		r = work(100000)
		if d := time.Since(t0); d < best {
			best = d
		}
	}
	fmt.Println(r)
	fmt.Fprintf(os.Stderr, "BENCH strbuild %d\n", best.Nanoseconds())
}
