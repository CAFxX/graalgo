// goroutines: goroutine scalability. GOR_N (default 100000) goroutines are
// spawned and park on a channel. Before the timed rounds, the idle cost:
//
//	STAT goroutines spawn_ms             spawning GOR_N goroutines until all run
//	STAT goroutines rss_before_mb        VmRSS before
//	STAT goroutines idle_rss_mb          VmRSS with GOR_N parked (after a GC)
//	STAT goroutines idle_bytes_per_g     (idle - before) / GOR_N
//
// A round is one lifecycle of GOR_N goroutines: spawn, park, a broadcast wake
// (close), exit, joined by a WaitGroup.
package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"graalgo/bench/h"
)

var n = 100000

func vmRSSKB() int {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return -1
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "VmRSS:") {
			f := strings.Fields(ln)
			v, _ := strconv.Atoi(f[1])
			return v
		}
	}
	return -1
}

func lifecycle(release chan struct{}, started, done *sync.WaitGroup) {
	started.Add(n)
	done.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			started.Done()
			<-release
			done.Done()
		}()
	}
	started.Wait()
}

func work() int {
	release := make(chan struct{})
	var started, done sync.WaitGroup
	lifecycle(release, &started, &done)
	close(release)
	done.Wait()
	return n
}

func main() {
	if s := os.Getenv("GOR_N"); s != "" {
		n, _ = strconv.Atoi(s)
	}
	runtime.GC()
	before := vmRSSKB()
	release := make(chan struct{})
	var started, done sync.WaitGroup
	t0 := h.Now()
	lifecycle(release, &started, &done)
	spawn := h.Now() - t0
	runtime.GC()
	time.Sleep(500 * time.Millisecond)
	idle := vmRSSKB()
	close(release)
	done.Wait()
	println("STAT goroutines spawn_ms", int(spawn/1000000))
	println("STAT goroutines rss_before_mb", before/1024)
	println("STAT goroutines idle_rss_mb", idle/1024)
	println("STAT goroutines idle_bytes_per_g", (idle-before)*1024/n)
	h.Run("goroutines", 3, work)
}
