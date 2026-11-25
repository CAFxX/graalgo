// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Coroutines: what upstream's coro.go provides to package iter
// (//go:linknamestd newcoro, coroswitch), on graalgo's goroutines.

package runtime

// A coro represents extra concurrency without extra parallelism,
// as would be needed for a coroutine.
//
// A coro is a goroutine blocked waiting to run f, and a hand-off point:
// coroswitch(c) wakes the goroutine blocked on c and blocks the caller on c,
// so exactly one of the two sides runs at a time. Upstream switches the M
// directly to the other goroutine (coroswitch_m); here the hand-off is an
// unbuffered channel, whose send pairs with the other side's receive: the
// side that sends is the one that then waits (receives), so the two
// operations of a switch can only pair with the other side's. Panics and
// runtime.Goexit inside f are iter.Pull's business (its deferred function
// records them and the other side re-raises them); f's goroutine ends with
// coroexit, which hands control back for the last time.
type coro struct {
	f    func(*coro)
	wake chan struct{}
}

// newcoro creates a new coro containing a
// goroutine blocked waiting to run f
// and returns that coro.
func newcoro(f func(*coro)) *coro {
	c := &coro{f: f, wake: make(chan struct{})}
	go corostart(c)
	return c
}

// corostart is the entry func for a new coroutine.
// It runs the coroutine user function f passed to corostart
// and then calls coroexit to remove the extra concurrency.
func corostart(c *coro) {
	<-c.wake
	defer coroexit(c)
	c.f(c)
}

// coroexit is like coroswitch but closes the coro
// and exits the current goroutine
func coroexit(c *coro) {
	c.wake <- struct{}{}
}

// coroswitch switches to the goroutine blocked on c
// and then blocks the current goroutine on c.
func coroswitch(c *coro) {
	c.wake <- struct{}{}
	<-c.wake
}
