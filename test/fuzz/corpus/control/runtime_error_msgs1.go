// Targets exact runtime.Error message text for bounds and index failures
// recovered at runtime: index out of range, slice bounds of every shape
// (2- and 3-index, strings, arrays via pointer), and the runtime.Error
// interface being satisfied.
package main

import "runtime"

func try(name string, f func()) {
	defer func() {
		r := recover()
		if r == nil {
			println(name, "no panic")
			return
		}
		_, isRT := r.(runtime.Error)
		e, ok := r.(interface{ Error() string })
		if !ok {
			println(name, "non-error panic")
			return
		}
		println(name, isRT, e.Error())
	}()
	f()
}

var sink int

func main() {
	s := []int{1, 2, 3}
	c5 := make([]int, 2, 5)
	str := "hello"
	arr := [4]int{}
	pa := &arr
	i, j, k := 5, 7, 2
	neg := -1
	try("idx", func() { sink = s[i] })
	try("idxneg", func() { sink = s[neg] })
	try("idxstr", func() { sink = int(str[i+1]) })
	try("idxarr", func() { sink = pa[i] })
	try("store", func() { s[len(s)] = 1 })
	try("hi>cap", func() { _ = c5[:j] })
	try("lo>hi", func() { _ = s[3:k] })
	try("lo>len", func() { _ = s[i:] })
	try("str hi", func() { _ = str[:j] })
	try("str lo>hi", func() { _ = str[4:k] })
	try("3idx max", func() { _ = c5[0:1:j] })
	try("3idx hi>max", func() { _ = c5[0 : 4 : k+1] })
	try("3idx lo>hi", func() { _ = c5[k+1 : k : 4] })
	try("arrptr", func() { _ = pa[1:j] })
	try("neglo", func() { _ = s[neg:] })
	var u8 uint8 = 200
	try("u8", func() { sink = s[u8] })
	var big uint64 = 1 << 40
	try("u64", func() { sink = s[big] })
	try("conv", func() { _ = [4]int(s) })
	try("ok", func() { _ = s[1:2:3] })
	var ns []int
	try("nil idx", func() { sink = ns[0] })
	try("nil slice ok", func() { _ = ns[0:0] })
}
