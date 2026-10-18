//go:build !unix

package main

// catchDriverSignals is a no-op off unix (no SIGQUIT there); the `run`
// path still reports a signal-killed backend via decideRunExit.
func catchDriverSignals() {}
