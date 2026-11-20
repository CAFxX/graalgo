package main

// L11 (REVIEW-2026-11-18-05): testing/synctest — zero corpus imports
// before this program. Running code in a synctest bubble with a fake
// clock: a goroutine sleeps, the test waits for it, and the order of
// events is pinned. Sleeps consume no real time (fake clock), but
// the program is still marked .timing per the corpus convention.
// The *testing.T is a zero value: synctest.Test only needs it for
// FailNow on failure, which never happens here.

import (
	"fmt"
	"testing"
	"testing/synctest"
	"time"
)

func main() {
	t := &testing.T{}
	synctest.Test(t, func(t *testing.T) {
		done := make(chan string, 2)
		go func() {
			time.Sleep(2 * time.Second)
			done <- "worker-after-2s"
		}()
		go func() {
			time.Sleep(1 * time.Second)
			done <- "worker-after-1s"
		}()
		// synctest.Sleep advances the fake clock and waits for the
		// bubble to settle.
		synctest.Sleep(3 * time.Second)
		first := <-done
		second := <-done
		fmt.Printf("order %s then %s\n", first, second)
		// Wait blocks until every other goroutine in the bubble is
		// durably blocked (both workers have finished by now).
		synctest.Wait()
		fmt.Printf("wait-settled=true\n")
	})
	fmt.Printf("bubble-done=true\n")
}
