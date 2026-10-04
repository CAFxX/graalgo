// O3 Go side: select fairness over 8 pre-filled channels (feederless,
// pure poll-order fairness), 20k selects.
package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
)

func main() {
	nCases := 8
	iters := 20000
	if len(os.Args) > 1 {
		iters, _ = strconv.Atoi(os.Args[1])
	}
	perCh := iters/nCases + 100
	chs := make([]chan int, nCases)
	for i := range chs {
		chs[i] = make(chan int, perCh)
		for k := 0; k < perCh; k++ {
			chs[i] <- i
		}
	}
	hits := make([]int64, nCases)
	for k := 0; k < iters; k++ {
		select {
		case v := <-chs[0]:
			hits[0]++
			_ = v
		case v := <-chs[1]:
			hits[1]++
			_ = v
		case v := <-chs[2]:
			hits[2]++
			_ = v
		case v := <-chs[3]:
			hits[3]++
			_ = v
		case v := <-chs[4]:
			hits[4]++
			_ = v
		case v := <-chs[5]:
			hits[5]++
			_ = v
		case v := <-chs[6]:
			hits[6]++
			_ = v
		case v := <-chs[7]:
			hits[7]++
			_ = v
		}
	}
	exp := float64(iters) / float64(nCases)
	maxZ := 0.0
	for _, h := range hits {
		z := math.Abs(float64(h)-exp) / math.Sqrt(exp*(1-1/float64(nCases)))
		if z > maxZ {
			maxZ = z
		}
	}
	fmt.Printf("cases=%d iters=%d expected=%.0f\n", nCases, iters, exp)
	fmt.Printf("hits=%v max_abs_z=%.2f\n", hits, maxZ)
}
