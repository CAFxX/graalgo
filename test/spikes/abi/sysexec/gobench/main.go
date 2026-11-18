// Command gobench prints gc's cost of the three calls abispike.SysMain
// bench times (best of 5 rounds), for the comparison in abi-spike.md.
package main

import (
	"fmt"
	"syscall"
	"time"
)

func main() {
	fd, err := syscall.Open("/dev/null", syscall.O_WRONLY, 0)
	if err != nil {
		panic(err)
	}
	buf := make([]byte, 64)
	var st syscall.Stat_t
	const n = 1_000_000
	best := [3]time.Duration{1 << 62, 1 << 62, 1 << 62}
	for round := 0; round < 5; round++ {
		t0 := time.Now()
		for i := 0; i < n; i++ {
			syscall.Write(fd, buf)
		}
		t1 := time.Now()
		for i := 0; i < n; i++ {
			syscall.Fstat(fd, &st)
		}
		t2 := time.Now()
		for i := 0; i < n; i++ {
			syscall.Getpid()
		}
		t3 := time.Now()
		best[0], best[1], best[2] = min(best[0], t1.Sub(t0)), min(best[1], t2.Sub(t1)), min(best[2], t3.Sub(t2))
	}
	fmt.Printf("bench gc   write(/dev/null, 64B) %.0f ns/op\n", float64(best[0])/n)
	fmt.Printf("bench gc   fstat                 %.0f ns/op\n", float64(best[1])/n)
	fmt.Printf("bench gc   getpid                %.0f ns/op\n", float64(best[2])/n)
}
