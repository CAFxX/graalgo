// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package atomic provides low-level atomic memory primitives
// useful for implementing synchronization algorithms.
//
// These functions require great care to be used correctly.
// Except for special, low-level applications, synchronization is better
// done with channels or the facilities of the [sync] package.
// Share memory by communicating;
// don't communicate by sharing memory.
//
// The swap operation, implemented by the SwapT functions, is the atomic
// equivalent of:
//
//	old = *addr
//	*addr = new
//	return old
//
// The compare-and-swap operation, implemented by the CompareAndSwapT
// functions, is the atomic equivalent of:
//
//	if *addr == old {
//		*addr = new
//		return true
//	}
//	return false
//
// The add operation, implemented by the AddT functions, is the atomic
// equivalent of:
//
//	*addr += delta
//	return *addr
//
// The load and store operations, implemented by the LoadT and StoreT
// functions, are the atomic equivalents of "return *addr" and
// "*addr = val".
//
// In the terminology of [the Go memory model], if the effect of
// an atomic operation A is observed by atomic operation B,
// then A “synchronizes before” B.
// Additionally, all the atomic operations executed in a program
// behave as though executed in some sequentially consistent order.
// This definition provides the same semantics as
// C++'s sequentially consistent atomics and Java's volatile variables.
//
// Only a few integer sizes are supported: on many architectures,
// atomic operations on non-word-sized integers are inefficient or
// infeasible. For example, a [Bool] may be larger than a built-in bool.
//
// [the Go memory model]: https://go.dev/ref/mem
package atomic

