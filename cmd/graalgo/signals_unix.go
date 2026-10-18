//go:build unix

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// catchDriverSignals mirrors `go run` (base.StartSigHandlers): the driver
// catches interrupt and quit and discards them, so those signals still
// reach the backend with default disposition while the driver survives to
// report the backend's signal death as "signal: <name>" with exit 1.
//
// It must be catch-and-discard (signal.Notify), NOT signal.Ignore: ignored
// dispositions survive execve, which would make the backend unkillable by
// Ctrl-C too. Caught dispositions reset to SIG_DFL in the exec'd child —
// that reset is what lets Ctrl-C kill the backend while the driver lives.
func catchDriverSignals() {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, os.Interrupt, syscall.SIGQUIT)
	go func() {
		for range ch {
		}
	}()
}
