package main

// GOMAXPROCS matrix: compute-bound workload.
// N spinners, each doing M iterations of integer arithmetic; main waits
// via package-level flags, then prints a checksum. The checksum is
// deterministic (order-independent sum), so output is directly
// comparable across --gmp values and against gc.
import "fmt"

var done [8]bool
var sums [8]int64

func main() {
	n := 4
	for i := 0; i < n; i++ {
		id := i // per-iteration variable: each closure captures its own
		go func() {
			var s int64
			for j := 0; j < 2000000; j++ {
				s += int64(j) * 0x9E3779B9
				s ^= s >> 29
			}
			sums[id] = s
			done[id] = true
		}()
	}
	for !(done[0] && done[1] && done[2] && done[3]) {
	}
	var total int64
	for i := 0; i < n; i++ {
		total += sums[i]
	}
	fmt.Println(total)
}
