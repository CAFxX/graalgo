// O1 Go side: preemption torture. nSpin CPU-bound goroutines + one
// latency ping-pong pair. Run with GOMAXPROCS=2.
package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"
)

var stop = false

func main() {
	nSpin, _ := strconv.Atoi(os.Args[1])
	secs, _ := strconv.Atoi(os.Args[2])

	counts := make([]int64, nSpin)
	for i := 0; i < nSpin; i++ {
		go func(idx int) {
			var c int64
			var sink uint64
			for !stop {
				sink += uint64(c) * 0x9E3779B97F4A7C15
				sink ^= sink >> 29
				c++
			}
			counts[idx] = c
			_ = sink
		}(i)
	}

	ping := make(chan int64)
	pong := make(chan int64)
	const maxSamples = 200000
	rtts := make([]int64, maxSamples)
	n := 0
	done := make(chan struct{})
	go func() {
		for !stop && n < maxSamples {
			t0 := time.Now().UnixNano()
			ping <- t0
			<-pong
			rtts[n] = time.Now().UnixNano() - t0
			n++
		}
		close(done)
	}()
	go func() {
		for {
			select {
			case t := <-ping:
				pong <- t
			case <-done:
				return
			}
		}
	}()

	time.Sleep(time.Duration(secs) * time.Second)
	stop = true
	<-done
	time.Sleep(200 * time.Millisecond)

	s := rtts[:n]
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	var sum int64
	for _, v := range s {
		sum += v
	}
	pct := func(p float64) int64 { return s[int(p/100*float64(n))] }
	var mean float64
	for _, v := range counts {
		mean += float64(v)
	}
	mean /= float64(nSpin)
	var sd float64
	for _, v := range counts {
		d := float64(v) - mean
		sd += d * d
	}
	sd = sqrt(sd / float64(nSpin))
	fmt.Printf("nSpin=%d secs=%d samples=%d\n", nSpin, secs, n)
	fmt.Printf("spinner_counts=%v\n", counts)
	fmt.Printf("spinner_cv=%.4f\n", sd/mean)
	fmt.Printf("rtt_ns mean=%.0f p50=%d p99=%d max=%d\n",
		float64(sum)/float64(n), pct(50), pct(99), s[n-1])
}

func sqrt(x float64) float64 {
	z := x
	for i := 0; i < 50; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}
