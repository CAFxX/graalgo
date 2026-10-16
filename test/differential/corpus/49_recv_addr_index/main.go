package main

// Regression: implicit method-receiver & on index expressions.
//
// x.M() with a pointer receiver (*T) on an addressable receiver x is
// (&x).M() per the Go spec. When x is an index expression (arr[i], s[i]),
// the frontend must emit the lvalue {"array","index"} addrof schema so
// the pointer addresses the element slot. Emitting the operand-addressing
// {"x"} schema makes the backend snapshot the element value, silently
// losing receiver mutations for value-copied element types (scalars).
//
// Uses println (not fmt): the fmt import is broken on master for an
// unrelated pre-existing reason (linkname multi-result scope error).

type I int

func (i *I) Inc() { *i++ }

type Counter struct{ n int }

func (c *Counter) Inc() { c.n++ }

func main() {
	// Array element, scalar type: mutation must persist.
	arr := [3]I{10, 20, 30}
	arr[1].Inc()
	println("array:", int(arr[1]))

	// Slice element, scalar type: mutation must persist.
	s := []I{10, 20, 30}
	s[2].Inc()
	println("slice:", int(s[2]))

	// Method value on an index expression binds the slot, not a copy:
	// gc evaluates arr[1] = 99 before f() runs, so f() sees 99 -> 100.
	mv := [3]I{10, 20, 30}
	f := mv[1].Inc
	mv[1] = 99
	f()
	println("methodvalue:", int(mv[1]))

	// Struct element (reference-aliased): must keep working.
	carr := [3]Counter{}
	carr[0].Inc()
	println("struct-elem:", carr[0].n)

	// Field via index: must keep working.
	type Pair struct{ c Counter }
	ps := []Pair{{}, {}}
	ps[1].c.Inc()
	println("field-via-index:", ps[1].c.n)
}
