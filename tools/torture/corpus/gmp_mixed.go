package main

// gmp_mixed: deterministic mixed compute+communication workload.
//
// A producer feeds 1..N into a channel; four worker goroutines square each
// value and forward it; the main goroutine sums the squares. Addition is
// order-independent, so the printed total is identical under any schedule
// and any GOMAXPROCS. The gmp-matrix and stress-differential scripts compare
// graalgo's output byte-for-byte against gc's.
//
// NOTE: output goes through the println builtin (stderr on both gc and
// graalgo); see gmp_compute.go for why fmt is not used.
func main() {
	const N = 5000
	const workers = 4
	in := make(chan int)
	out := make(chan int)
	for w := 0; w < workers; w++ {
		go func() {
			for {
				v, ok := <-in
				if !ok {
					return
				}
				out <- v * v
			}
		}()
	}
	go func() {
		for i := 1; i <= N; i++ {
			in <- i
		}
		close(in)
	}()
	var total int64
	for i := 0; i < N; i++ {
		total += int64(<-out)
	}
	println(total)
}
