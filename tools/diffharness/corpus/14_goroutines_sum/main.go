package main

import (
	"fmt"
	"sync"
)

func main() {
	nums := []int{1, 2, 3, 4, 5, 6, 7, 8}
	ch := make(chan int, 4)
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(part []int) {
			defer wg.Done()
			sum := 0
			for _, v := range part {
				sum += v
			}
			ch <- sum
		}(nums[w*2 : w*2+2])
	}
	go func() { wg.Wait(); close(ch) }()
	total := 0
	for s := range ch {
		total += s
	}
	fmt.Println(total) // 36, order-independent
}
