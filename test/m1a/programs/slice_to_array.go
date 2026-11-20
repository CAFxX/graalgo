package main

// Slice-to-array and slice-to-array-pointer conversions (Go 1.17/1.20):
// the array is a copy, the pointer aliases the slice's backing array, a
// short slice panics with gc's boundsConvert text, a nil slice converts to
// a nil *[0]T and to a zero [0]T. Found by test/fuzz (BUG-0038).

type pair struct {
	a int
	s string
}

func arr(s []int) [4]int       { return [4]int(s) }
func ptr(s []int) *[4]int      { return (*[4]int)(s) }
func arr0(s []int) [0]int      { return [0]int(s) }
func ptr0(s []int) *[0]int     { return (*[0]int)(s) }
func pairs(s []pair) [2]pair   { return [2]pair(s) }
func bytes3(s []byte) *[3]byte { return (*[3]byte)(s) }
func named(s []int) Quad       { return Quad(s) }

type Quad [4]int

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(name, "panic:", r.(interface{ Error() string }).Error())
		}
	}()
	f()
}

func main() {
	s := []int{1, 2, 3, 4, 5}
	a := arr(s)
	a[0] = 9
	println(a[0], a[1], a[3], s[0])
	p := ptr(s[1:])
	p[0] = 7
	println(p[0], p[3], s[1], len(p))
	q := named(s)
	println(q[2], len(q))
	var nils []int
	println(ptr0(nils) == nil, ptr0(s[:0]) == nil, len(arr0(nils)))
	ps := []pair{{1, "x"}, {2, "y"}, {3, "z"}}
	pa := pairs(ps[1:])
	ps[1].s = "changed"
	println(pa[0].a, pa[0].s, pa[1].s)
	b := []byte("hello")
	bp := bytes3(b[2:])
	bp[0] = 'L'
	println(string(b))
	try("short array", func() { _ = arr(s[:3]) })
	try("short ptr", func() { _ = ptr(s[4:]) })
	try("nil ptr", func() { _ = ptr(nils) })
}
