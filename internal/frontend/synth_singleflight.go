package main

// P3-M9 (tier-4: net): synthetic `internal/singleflight` package.
//
// Real-Go source: adapted from gc go1.27.1
// ($GOROOT/src/internal/singleflight/singleflight.go).
//
// v1 limitation (documented, not hidden): the frontend cannot invoke a
// multi-result func VALUE (WS D — the Appendix-M desugar only covers
// named functions in its tables). singleflight's core operation is
// invoking the caller's func() (any, error). That invocation is therefore
// not expressible in v1. The package keeps gc's exact API:
//   - types call, Group, Result: ported verbatim;
//   - ForgetUnshared: ported verbatim (no fn invocation — fully working);
//   - Do, DoChan, doCall: exact gc signatures with fail-fast bodies
//     naming the retest trigger (WS D: multi-result func-value
//     invocation). They are never emitted (multi-result decls are
//     skipped); call sites fail fast with the named M9 scope error.
// No M9 corpus program pins singleflight (it is net/http infrastructure);
// the fail-fast is loud, never silent.
const syntheticSingleflightSource = `package singleflight

import "sync"

// call is an in-flight or completed singleflight.Do call
type call struct {
	wg sync.WaitGroup

	// These fields are written once before the WaitGroup is done
	// and are only read after the WaitGroup is done.
	val any
	err error

	// These fields are read and written with the singleflight
	// mutex held before the WaitGroup is done, and are read but
	// not written after the WaitGroup is done.
	dups  int
	chans []chan<- Result
}

// Group represents a class of work and forms a namespace in
// which units of work can be executed with duplicate suppression.
type Group struct {
	mu sync.Mutex       // protects m
	m  map[string]*call // lazily initialized
}

// Result holds the results of Do, so they can be passed
// on a channel.
type Result struct {
	Val    any
	Err    error
	Shared bool
}

// Do executes and returns the results of the given function, making
// sure that only one execution is in-flight for a given key at a
// time. If a duplicate comes in, the duplicate caller waits for the
// original to complete and receives the same results.
// The return value shared indicates whether v was given to multiple callers.
func (g *Group) Do(key string, fn func() (any, error)) (v any, err error, shared bool) {
	panic("singleflight.Group.Do: not in M9 scope (needs multi-result func-value invocation support)")
}

// DoChan is like Do but returns a channel that will receive the
// results when they are ready.
func (g *Group) DoChan(key string, fn func() (any, error)) <-chan Result {
	panic("singleflight.Group.DoChan: not in M9 scope (needs multi-result func-value invocation support)")
}

// doCall handles the single call for a key.
func (g *Group) doCall(c *call, key string, fn func() (any, error)) {
	panic("singleflight.Group.doCall: not in M9 scope (needs multi-result func-value invocation support)")
}

// ForgetUnshared tells the singleflight to forget about a key if it is not
// shared with any other goroutines. Future calls to Do for a forgotten key
// will call the function rather than waiting for an earlier call to complete.
// Returns whether the key was forgotten or unknown--that is, whether no
// other goroutines are waiting for the result.
func (g *Group) ForgetUnshared(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	c, ok := g.m[key]
	if !ok {
		return true
	}
	if c.dups == 0 {
		delete(g.m, key)
		return true
	}
	return false
}
`
