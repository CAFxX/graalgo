package main

// Map key edge cases found by test/fuzz: assigning to an existing entry
// stores the new key (+0 then -0 leaves -0, gc's NeedKeyUpdate; BUG-0041);
// a lookup or delete on a nil or empty map checks an interface key and an
// unhashable one panics with maps.unhashableTypeError ("hash of unhashable
// type: T", no "runtime error: " prefix), while a non-empty map hashes and
// panics with the runtime's errorString (BUG-0040).

func negZero() float64 {
	z := 0.0
	return -z
}

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(name, "panic:", r.(interface{ Error() string }).Error())
		}
	}()
	f()
	println(name, "no panic")
}

type K struct {
	n int
	v any
}

var sink int

func main() {
	z := map[float64]string{}
	z[0] = "pos"
	z[negZero()] = "neg"
	for k, v := range z {
		println("key", k, 1/k < 0, v)
	}
	zi := map[any]int{}
	zi[0.0] = 1
	zi[negZero()] = 2
	for k, v := range zi {
		println("ikey", 1/k.(float64) < 0, v)
	}

	var nilm map[any]int
	empty := map[any]int{}
	full := map[any]int{1: 1}
	try("nil get", func() { sink = nilm[[]int{1}] })
	try("nil get2", func() { _, ok := nilm[map[int]int{}]; println(ok) })
	try("nil delete", func() { delete(nilm, []int{}) })
	try("empty get", func() { sink = empty[func() {}] })
	try("empty delete", func() { delete(empty, []byte{}) })
	try("full get", func() { sink = full[[]int{1}] })
	try("full delete", func() { delete(full, []int{1}) })
	try("empty put", func() { empty[[]int{}] = 1 })
	var sk map[K]int
	try("struct nil get", func() { sink = sk[K{1, []int{}}] })
	try("struct ok", func() { sink = sk[K{1, 2}] })
	println(len(empty), len(full))
}
