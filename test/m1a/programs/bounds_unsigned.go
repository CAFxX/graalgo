package main

// Bounds panics print an index of an unsigned type unsigned, also at or above
// 2^63 (gc's boundsError.signed comes from the index type), for indexing and
// every slice-expression check; signed indexes keep the negative forms.

func try(name string, f func()) {
	defer func() { println(name+":", recover().(error).Error()) }()
	f()
}

func main() {
	x := []int{1, 2, 3}
	a := [3]int{}
	s := "abc"
	h := uint64(1 << 63)
	u := ^uint64(0)
	n := -1
	try("slice index", func() { _ = x[h] })
	try("array index", func() { _ = a[u] })
	try("string index", func() { _ = s[h] })
	try("signed index", func() { _ = x[n] })
	try("slice hi", func() { _ = x[:h] })
	try("slice lo", func() { _ = x[h:] })
	try("slice3 max", func() { _ = x[0:1:h] })
	try("slice3 hi", func() { _ = x[0:h:2] })
	try("slice3 lo", func() { _ = x[h:1:2] })
	try("array hi", func() { _ = a[:u] })
	try("string hi", func() { _ = s[:h] })
	try("string lo", func() { _ = s[h:] })
	try("uint32 index", func() { _ = x[uint32(1<<31)] })
	try("signed lo", func() { _ = x[n:] })
}
