package main

import (
	"errors"
	"sort"
)

// so09: 3-feature combination — custom error type carrying a priority,
// sorted by priority via a custom Interface, then Joined in priority
// order; Search over the sorted priorities.

// prioErr is an error with a numeric priority (lower = more severe).
type prioErr struct {
	prio int
	msg  string
}

func (e *prioErr) Error() string { return e.msg }

type byPrio []*prioErr

func (p byPrio) Len() int           { return len(p) }
func (p byPrio) Less(i, j int) bool { return p[i].prio < p[j].prio }
func (p byPrio) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func main() {
	errs := []*prioErr{
		{3, "disk slow"},
		{1, "disk dead"},
		{2, "disk warm"},
		{1, "net dead"},
		{2, "net warm"},
	}
	sort.Stable(byPrio(errs)) // stable: same-prio keeps insertion order
	for _, e := range errs {
		print(e.prio, ":", e.msg, " ")
	}
	println()
	// Join in priority order (spread: v1 has no variadic packing).
	flat := make([]error, len(errs))
	prios := make([]int, len(errs))
	for i, e := range errs {
		flat[i] = e
		prios[i] = e.prio
	}
	joined := errors.Join(flat...)
	println("[" + joined.Error() + "]")
	// Binary search the sorted priorities.
	println(sort.SearchInts(prios, 2)) // 2 (first prio-2)
	println(sort.SearchInts(prios, 1)) // 0
	// Is finds a member through the join.
	println(errors.Is(joined, errs[1])) // true
	println(errors.Is(joined, &prioErr{prio: 9, msg: "nope"})) // false
}
