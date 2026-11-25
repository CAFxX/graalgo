// O2 Go side: channel fairness. 8 senders x 20k on one shared channel.
package main

import (
	"fmt"
	"os"
	"strconv"
	"sync"
)

func main() {
	capv, _ := strconv.Atoi(os.Args[1])
	const nSenders = 8
	const perSender = 20000
	ch := make(chan int, capv)
	var wg sync.WaitGroup
	got := make([]int64, nSenders)
	for i := 0; i < nSenders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for m := 0; m < perSender; m++ {
				ch <- id
			}
		}(i)
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	for id := range ch {
		got[id]++
	}
	var mn, mx int64 = 1 << 62, 0
	for _, v := range got {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	fmt.Printf("cap=%d senders=%d perSender=%d\n", capv, nSenders, perSender)
	fmt.Printf("per_sender=%v max_min_ratio=%.4f\n", got, float64(mx)/float64(mn))
}
