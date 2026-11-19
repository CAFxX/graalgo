package main

// A conversion T(nil) is a typed nil, not the predeclared nil: converted to
// an interface it is a non-nil interface value holding a nil pointer (map,
// slice, func, channel), and comparing an interface with it compares
// dynamic types.

type E interface{ Error() string }

type P struct{ x int }

func (p *P) Error() string { return "P" }

func isnil(a any) bool { return a == nil }

func kind(a any) string {
	switch a.(type) {
	case nil:
		return "nil"
	case *int:
		return "*int"
	case *P:
		return "*P"
	case []int:
		return "[]int"
	case map[string]int:
		return "map"
	case func():
		return "func"
	case chan int:
		return "chan"
	case *E:
		return "*E"
	}
	return "other"
}

func main() {
	var p *int
	println(isnil(p), isnil((*int)(nil)), isnil((*E)(nil)), isnil((*error)(nil)), isnil(nil))
	println(isnil([]int(nil)), isnil(map[string]int(nil)), isnil((func())(nil)), isnil((chan int)(nil)))
	println(isnil(any(nil)), isnil(E(nil)), isnil(error(nil)))
	println(kind((*int)(nil)), kind((*P)(nil)), kind([]int(nil)), kind(map[string]int(nil)),
		kind((func())(nil)), kind((chan int)(nil)), kind((*E)(nil)), kind(nil), kind(E(nil)))

	var e E = (*P)(nil)
	println(e == nil, e == (*P)(nil), e != (*P)(nil), (*P)(nil) == e)
	var a any = (*int)(nil)
	println(a == nil, a == (*int)(nil), a == (*P)(nil), a == any((*int)(nil)))
	a = nil
	println(a == nil, a == (*int)(nil))

	// A typed nil where no interface is involved stays a plain nil.
	q := (*P)(nil)
	println(q == nil, (*P)(nil) == nil, q == (*P)(nil), []int(nil) == nil, len([]int(nil)))
	var arr = [2]any{(*int)(nil), nil}
	println(arr[0] == nil, arr[1] == nil)
	m := map[string]any{"k": (*P)(nil)}
	println(m["k"] == nil, m["x"] == nil)
	f := func() E { return (*P)(nil) }
	println(f() == nil, f().Error())
}
