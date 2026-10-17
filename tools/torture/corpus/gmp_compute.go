package main

import "fmt"

// gmp_compute: deterministic parallel compute workload.
//
// Eight workers each sum the squares of their strided slice of 1..N; the
// partial sums are added in a fixed order, so the printed total is
// identical under any goroutine schedule and any GOMAXPROCS. The
// gmp-matrix and stress-differential scripts compare graalgo's stdout
// byte-for-byte against gc's.
func main() {
	const N = 200000
	const workers = 8
	ch := make(chan int64, workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			var sum int64
			for i := w + 1; i <= N; i += workers {
				sum += int64(i) * int64(i)
			}
			ch <- sum
		}(w)
	}
	var total int64
	for w := 0; w < workers; w++ {
		total += <-ch
	}
	fmt.Println(total)
}
