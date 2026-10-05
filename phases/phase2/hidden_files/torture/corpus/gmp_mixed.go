package main

// GOMAXPROCS matrix: mixed-intensity workload (Go-level).
//
// NOTE: true I/O-slot behavior (ioEnter/ioExit while blocked) cannot be
// expressed in Go source yet — no timer/import support (WS C timers exist
// in Java; Go `import "time"` is WS G). This program varies compute
// intensity across workers (bursty vs steady) to exercise the drainer's
// admission fairness under heterogeneous load. True mixed I/O+compute
// with slot release is covered Java-level (TortureBattery F5b port +
// IoSlotsTest).
import "fmt"

var done [4]bool
var iters [4]int64

func main() {
	for i := 0; i < 4; i++ {
		id := i
		bursty := id < 2
		go func() {
			var c int64
			if bursty {
				for round := 0; round < 50; round++ {
					for j := 0; j < 100000; j++ {
						c += int64(j)
					}
					for j := 0; j < 50000; j++ {
					}
				}
			} else {
				for j := 0; j < 5000000; j++ {
					c += int64(j)
				}
			}
			iters[id] = c
			done[id] = true
		}()
	}
	for !(done[0] && done[1] && done[2] && done[3]) {
	}
	var total int64
	for i := 0; i < 4; i++ {
		total += iters[i]
	}
	fmt.Println(total)
}
