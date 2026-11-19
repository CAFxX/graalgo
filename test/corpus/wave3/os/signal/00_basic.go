package main

// L11 (REVIEW-2026-11-18-05): os/signal — zero corpus imports before this
// program. Notify for SIGUSR1, kill self, receive from the channel.
// Deterministic: the only output is fixed strings; the receive blocks
// until the self-sent signal arrives (no timeout, no wall clock).

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	c := make(chan os.Signal, 2)
	signal.Notify(c, syscall.SIGUSR1)
	defer signal.Stop(c)

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		fmt.Printf("kill-err=%v\n", err)
		return
	}
	sig := <-c
	fmt.Printf("got=%v\n", sig)
	fmt.Printf("is-usr1=%v\n", sig == syscall.SIGUSR1)

	// After Stop, no further delivery is registered. NOTE: do NOT send
	// another SIGUSR1 here -- once uncaught its default action terminates
	// the process. Just confirm nothing is pending on the channel.
	signal.Stop(c)
	select {
	case s := <-c:
		fmt.Printf("after-stop-got=%v\n", s)
	default:
		fmt.Printf("after-stop-got=<none>\n")
	}

	// Ignoring a signal: no delivery, no crash.
	signal.Ignore(syscall.SIGUSR2)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR2); err != nil {
		fmt.Printf("kill3-err=%v\n", err)
		return
	}
	fmt.Printf("ignored-ok=true\n")
}
