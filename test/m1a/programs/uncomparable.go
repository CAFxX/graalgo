package main

// Comparing interface values whose dynamic type is not comparable, and using
// one as a map key, panic naming the dynamic type (gc's efaceeq / typehash),
// not the field type that makes it uncomparable.

type F struct{ f func() }

type G struct {
	n int
	s []int
}

type Outer struct {
	g G
}

func try(name string, f func()) {
	defer func() { println(name+":", recover().(error).Error()) }()
	f()
}

func main() {
	try("struct with func", func() {
		var a, b any = F{}, F{}
		_ = a == b
	})
	try("nested struct with slice", func() {
		var a, b any = Outer{}, Outer{}
		_ = a == b
	})
	try("array of struct", func() {
		var a, b any = [1]G{}, [1]G{}
		_ = a == b
	})
	try("map key", func() {
		m := map[any]int{}
		m[Outer{}] = 1
	})
	try("map key slice", func() {
		m := map[any]int{}
		m[[]int{1}] = 1
	})
	try("struct field iface", func() {
		type H struct{ v any }
		a, b := H{[]int{}}, H{[]int{}}
		_ = a == b
	})
}
