// Targets an unrecovered nil pointer dereference: gc prints
// "panic: runtime error: invalid memory address or nil pointer dereference"
// plus the "[signal SIGSEGV: segmentation violation code=... addr=0x0 pc=...]"
// line, after earlier recovered nil derefs through methods and fields; exit 2.
package main

type node struct {
	val  int
	next *node
}

func (n *node) sum() int {
	if n == nil {
		return 0
	}
	return n.val + n.next.sum()
}

func (n *node) mustVal() int { return n.val }

func safe(f func() int) (v int, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			println("recovered:", r.(interface{ Error() string }).Error())
		}
	}()
	return f(), true
}

func main() {
	list := &node{1, &node{2, &node{3, nil}}}
	println("sum", list.sum())
	var empty *node
	println("nil sum", empty.sum())
	v, ok := safe(empty.mustVal)
	println("safe", v, ok)
	v, ok = safe(func() int { return list.next.next.next.val })
	println("safe2", v, ok)
	println("about to crash")
	println(list.next.next.next.next.val)
}
