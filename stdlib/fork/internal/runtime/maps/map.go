// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package maps is graalgo's facade of upstream's internal/runtime/maps
// (docs/arch/toolexec-design.md §5.1, §5.6). Upstream's package is the Swiss
// table behind gc's built-in map; graalgo's maps are objects of the backend
// (SPEC §3.4), so the table code has nothing to run on. The facade declares
// what Go source outside the runtime names — reflect's map iterator and
// hash/maphash's Use64BitHash — with upstream's names and signatures.
package maps

import (
	"internal/abi"
	"internal/goarch"
	"unsafe"
)

// Use64BitHash is true if the hash function produces 64-bit hashes.
const Use64BitHash = goarch.PtrSize == 8 && goarch.IsWasm == 0

// Map is a Go map as *Map names it: the backend's map object. It has no
// fields a Go program may touch.
type Map struct {
	_ [0]func() // not comparable, never constructed in Go
}

// Iter is a map iterator (upstream's Iter, reduced to its contract): after
// Init, each Next positions it on an entry not yet produced, or on none
// (Key() == nil) when the iteration is over.
type Iter struct {
	key  unsafe.Pointer // a copy of the current key, nil at the end
	elem unsafe.Pointer // a copy of the current element
	typ  *abi.MapType
	m    *Map
	// state is the backend's iterator over m (its position survives growth
	// and deletion as a range statement's does).
	state unsafe.Pointer
}

// Init initializes Iter for iteration.
func (it *Iter) Init(t *abi.MapType, m *Map) {
	it.typ = t
	it.m = m
	it.key = nil
	it.elem = nil
	it.state = nil
}

// Initialized reports whether Init was called.
func (it *Iter) Initialized() bool {
	return it.typ != nil
}

// Map returns the map this iterator is iterating over.
func (it *Iter) Map() *Map {
	return it.m
}

// Key returns a pointer to the current key. nil indicates end of iteration.
//
// Must not be called prior to Next.
func (it *Iter) Key() unsafe.Pointer {
	return it.key
}

// Elem returns a pointer to the current element. nil indicates end of
// iteration.
//
// Must not be called prior to Next.
func (it *Iter) Elem() unsafe.Pointer {
	return it.elem
}

// Next proceeds to the next element in iteration, which can be accessed via
// the Key and Elem methods.
//
// The iteration order is random, as a range statement's.
func (it *Iter) Next() {
	it.state, it.key, it.elem = iterNext(it.typ, it.m, it.state)
}

// iterNext advances the backend iterator state over m (nil: start one) and
// returns it with pointers to fresh copies of the entry's key and element,
// or nil pointers at the end.
func iterNext(t *abi.MapType, m *Map, state unsafe.Pointer) (next, key, elem unsafe.Pointer)
