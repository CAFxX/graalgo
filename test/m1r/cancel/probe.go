// probe: the program graalgo.launcher.CancelProbe cancels (test/m1r/cancel.sh).
// It starts goroutines parked on a channel, parked on timers and spinning (and
// so preempted at every quantum), creates the file named by its argument once
// they all started, and waits forever; only a cancellation ends it. Not here:
// a goroutine blocked in a system call (a read nobody answers). Its carrier
// waits for the system-call thread outside Truffle's safepoint protocol, so
// Context.close(true) waits for it forever; making that wait interruptible
// needs the native call's buffers kept alive past the cancel (work package
// WP-TR-AUDIT-3, docs/arch/truffle-audit.md).
package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

var sink int

func spin(n int) {
	s := 0
	for i := 0; ; i++ {
		s += i * n
		if s == -1 {
			break
		}
	}
	sink = s
}

func main() {
	var started sync.WaitGroup
	block := make(chan int)
	for i := 0; i < 1000; i++ {
		started.Add(1)
		go func() {
			started.Done()
			<-block
		}()
	}
	for i := 0; i < 100; i++ {
		started.Add(1)
		go func() {
			started.Done()
			time.Sleep(time.Hour)
		}()
	}
	for i := 0; i < runtime.GOMAXPROCS(0)+2; i++ {
		started.Add(1)
		go func(i int) {
			started.Done()
			spin(i + 1)
		}(i)
	}
	started.Wait()
	if err := os.WriteFile(os.Args[1], []byte("ready\n"), 0o644); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	select {}
}
