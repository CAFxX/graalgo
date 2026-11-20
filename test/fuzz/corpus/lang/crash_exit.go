// Targets the unrecovered-panic path: deferred calls run (and print) during
// panicking, a recovered-and-repanicked custom error value, and the exact
// "panic: ... [recovered]" crash text plus exit status 2 for an error value.
package main

import "strconv"

type codeErr struct {
	code int
	msg  string
}

func (e *codeErr) Error() string { return "code " + strconv.Itoa(e.code) + ": " + e.msg }

type node struct {
	name string
	next *node
}

func depth(n *node) int {
	if n == nil {
		return 0
	}
	return 1 + depth(n.next)
}

func work(n *node, limit int) (count int) {
	defer func() {
		println("work defer, count =", count)
	}()
	for p := n; p != nil; p = p.next {
		count++
		if count > limit {
			panic(&codeErr{count, "limit exceeded at " + p.name})
		}
	}
	return count
}

func guard(n *node, limit int) int {
	defer func() {
		if r := recover(); r != nil {
			e := r.(*codeErr)
			println("guard saw:", e.Error())
			e.code *= 10
			panic(e)
		}
	}()
	return work(n, limit)
}

func main() {
	var head *node
	for i := range 6 {
		head = &node{name: "n" + strconv.Itoa(i), next: head}
	}
	println("depth", depth(head))
	println("ok", guard(head, 10))
	defer println("main defer 1")
	defer func() {
		println("main defer 2")
	}()
	println("about to crash")
	guard(head, 3)
	println("unreachable")
}
