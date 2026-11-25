package main

import (
	"runtime"
	"time"
)

// Finalizers and cleanups (M3 gap G3): they run after runtime.GC finds
// their object unreachable, one at a time on a runtime goroutine; a
// finalizer gets its object back (contents intact, resurrectable, never
// finalized twice) and keeps what it references for a later cycle;
// SetFinalizer(x, nil), Cleanup.Stop and a live object
// (KeepAlive) keep them from running. Objects carry a pointer field so that
// gc never tiny-allocates them, and are made in non-inlined helpers so that
// no frame of main keeps them.
//
// A "sentinel" proves a negative: finalizers (and, separately, cleanups) run
// in queue order on one goroutine, so once a sentinel queued by a later
// collection has run, anything queued before it has too.

type obj struct {
	name string
	next *obj
	pad  [4]int64
}

func (o *obj) Name() string { return o.name }

type namer interface{ Name() string }

var events = make(chan string, 64)

func wait(what string) {
	select {
	case e := <-events:
		println(what, "->", e)
	case <-time.After(20 * time.Second):
		println(what, "-> timeout")
	}
}

//go:noinline
func finalized(name string) {
	o := &obj{name: name}
	runtime.SetFinalizer(o, func(p *obj) { events <- "finalized " + p.name })
}

//go:noinline
func removed(name string) {
	o := &obj{name: name}
	runtime.SetFinalizer(o, func(p *obj) { events <- "finalized " + p.name })
	runtime.SetFinalizer(o, nil)
}

// chained: head points at tail, both with finalizers. gc runs only head's
// in the collection that finds them; tail is reachable from head's special
// until head's finalizer has run, so tail's runs after the next collection.
//
//go:noinline
func chained() {
	tail := &obj{name: "tail"}
	head := &obj{name: "head", next: tail}
	runtime.SetFinalizer(tail, func(p *obj) { events <- "finalized " + p.name })
	runtime.SetFinalizer(head, func(p *obj) { events <- "finalized " + p.name + " -> " + p.next.name })
}

var saved *obj

//go:noinline
func resurrected(name string) {
	o := &obj{name: name, pad: [4]int64{1, 2, 3, 4}}
	runtime.SetFinalizer(o, func(p *obj) {
		saved = p
		events <- "resurrected " + p.name
	})
}

//go:noinline
func viaInterfaces() {
	a := &obj{name: "any"}
	runtime.SetFinalizer(a, func(x any) { events <- "any " + x.(*obj).name })
	n := &obj{name: "namer"}
	runtime.SetFinalizer(n, func(x namer) { events <- "namer " + x.Name() })
}

//go:noinline
func cleanup(name string, stop bool) {
	o := &obj{name: "x"}
	c := runtime.AddCleanup(o, func(s string) { events <- "cleanup " + s }, name)
	if stop {
		c.Stop()
	}
}

//go:noinline
func interiorCleanup(name string) {
	o := &obj{name: "x"}
	runtime.AddCleanup(&o.pad[2], func(s string) { events <- "cleanup " + s }, name)
}

var global obj

func main() {
	finalized("a")
	runtime.GC()
	wait("basic")

	removed("removed")
	finalized("sentinel1")
	runtime.GC()
	wait("after remove")
	finalized("sentinel2")
	runtime.GC()
	wait("after remove")
	println("pending", len(events))

	resurrected("lazarus")
	runtime.GC()
	wait("resurrection")
	println("saved", saved.name, saved.pad[0]+saved.pad[3])
	saved = nil
	runtime.GC()
	finalized("sentinel3")
	runtime.GC()
	wait("one-shot")
	println("pending", len(events))

	chained()
	runtime.GC()
	wait("chain")
	println("pending", len(events))
	runtime.GC()
	wait("chain")

	viaInterfaces()
	runtime.GC()
	got := map[string]bool{}
	for range 2 {
		select {
		case e := <-events:
			got[e] = true
		case <-time.After(20 * time.Second):
		}
	}
	println("interfaces", got["any any"], got["namer namer"])

	kept := &obj{name: "kept"}
	runtime.SetFinalizer(kept, func(p *obj) { events <- "finalized " + p.name })
	finalized("sentinel4")
	runtime.GC()
	wait("keepalive")
	println("pending", len(events))
	runtime.KeepAlive(kept)
	runtime.SetFinalizer(kept, nil) // (it is unreachable from here on)

	runtime.SetFinalizer(&global, func(p *obj) { events <- "global" })
	finalized("sentinel5")
	runtime.GC()
	wait("global")

	cleanup("c1", false)
	runtime.GC()
	wait("cleanup")
	cleanup("stopped", true)
	cleanup("c2", false)
	runtime.GC()
	wait("cleanup stop")
	cleanup("c3", false)
	runtime.GC()
	wait("cleanup stop")
	println("pending", len(events))
	interiorCleanup("interior")
	runtime.GC()
	wait("interior")

	func() {
		defer func() { println("recovered:", recover().(string)) }()
		o := &obj{name: "self"}
		runtime.AddCleanup(o, func(*obj) {}, o)
	}()
}
