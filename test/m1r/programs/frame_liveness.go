package main

import (
	"fmt"
	"runtime"
	"weak"
)

// A dead local does not keep its object alive (truffle-audit F-8.2). gc's
// stack maps let a pointer go once its variable is dead; graalgo clears a
// local's frame slot after the variable's last use, or when the header or
// block that declares it ends. Each case leaves an object reachable only
// through a dead local of a frame that is still running, collects, and
// checks a weak pointer to it.

type blob struct {
	data [1 << 16]byte
	id   int
}

var sink int

func use(b *blob) int { return b.id + int(b.data[0]) }

func collected(w weak.Pointer[blob]) bool {
	for range 5 {
		runtime.GC()
		if w.Value() == nil {
			return true
		}
	}
	return false
}

// After its last use, in the list that declares it.
func lastUse() bool {
	b := &blob{id: 1}
	w := weak.Make(b)
	sink += use(b)
	return collected(w)
}

// Declared in a nested block that has ended.
func nestedBlock() bool {
	var w weak.Pointer[blob]
	if sink >= 0 {
		b := &blob{id: 2}
		w = weak.Make(b)
		sink += use(b)
	}
	return collected(w)
}

// A range variable, once the loop is over.
func rangeVar() bool {
	ch := make(chan *blob, 1)
	ch <- &blob{id: 3}
	close(ch)
	var w weak.Pointer[blob]
	for b := range ch {
		w = weak.Make(b)
		sink += use(b)
	}
	return collected(w)
}

// Captured by a closure that is itself dead: the slot held the box.
func captured() bool {
	b := &blob{id: 4}
	w := weak.Make(b)
	f := func() int { return use(b) }
	sink += f()
	return collected(w)
}

// Still live: used after the collection, so it must survive it.
func live() bool {
	b := &blob{id: 5}
	w := weak.Make(b)
	ok := !collected(w)
	sink += use(b)
	return ok
}

func main() {
	fmt.Println("last use:", lastUse())
	fmt.Println("nested block:", nestedBlock())
	fmt.Println("range variable:", rangeVar())
	fmt.Println("captured:", captured())
	fmt.Println("live:", live())
}
