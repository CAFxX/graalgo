package main

// gmp_compute: deterministic parallel compute workload.
//
// Eight workers each sum the squares of their strided slice of 1..N; the
// partial sums are added in a fixed order, so the printed total is
// identical under any goroutine schedule and any GOMAXPROCS. The
// gmp-matrix and stress-differential scripts compare graalgo's output
// byte-for-byte against gc's.
//
// NOTE: output goes through the println builtin (stderr on both gc and
// graalgo) because the fmt/os packages are not yet executable through the
// product launcher on the current base (pre-existing toolchain gaps, not
// script issues); see tools/torture/README.md.
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
	println(total)
}
