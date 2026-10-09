package main

import (
	"fmt"
	"synthbisect"
)

// SCOPE-LIMITED + NOGC: external programs cannot import internal/* (go
// tool restriction). No synthetic adapter exists: the matcher's core
// (stack-pattern matching) needs runtime.Callers/CallersFrames, and there
// is no in-tree consumer. API-surface pin over the pure functions,
// verified by building against the real package in the tier1d harness.
func main() {
	m, err := bisect.New("")
	fmt.Println(err == nil)
	fmt.Println(m == nil) // empty pattern disables: nil matcher, nil error

	m2, err := bisect.New("y")
	fmt.Println(err == nil)
	fmt.Println(m2 != nil)

	// Marker round-trip: Marker -> CutMarker recovers id and short text
	// (one adjacent space is removed: "prefix [m] suffix" -> "prefix suffix").
	mk := bisect.Marker(999)
	short, id, ok := bisect.CutMarker("prefix " + mk + " suffix")
	fmt.Println(ok)
	fmt.Println(id == 999)
	fmt.Println(short == "prefix suffix")

	// CutMarker on a marker-free line.
	s2, id2, ok2 := bisect.CutMarker("no marker here")
	fmt.Println(!ok2)
	fmt.Println(id2 == 0)
	fmt.Println(s2 == "no marker here")

	// Hash is deterministic.
	fmt.Println(bisect.Hash("hello") == bisect.Hash("hello"))
	fmt.Println(bisect.Hash("hello") == bisect.Hash("world"))

	// AppendMarker produces a non-empty encoding.
	var buf []byte
	buf = bisect.AppendMarker(buf, 42)
	fmt.Println(len(buf) > 0)

	fmt.Println("ok")
}
