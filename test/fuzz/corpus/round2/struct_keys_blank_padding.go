// Map keys and == on structs with blank fields, padding, nested arrays,
// float/complex fields (NaN, -0), embedded interfaces and string fields
// built at run time; key overwrite keeps the original key (-0 then +0).
package main

import (
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

type padded struct {
	a int8
	_ int32
	b int64
	c bool
	_ [3]byte
	d int16
}

type fkey struct {
	f float64
	c complex64
	s string
}

type nested struct {
	arr [2]fkey
	k   any
}

type small struct {
	x uint8
	y uint16
}

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(name, "->", r.(interface{ Error() string }).Error())
		}
	}()
	f()
}

func keysOf[K comparable, V any](m map[K]V, f func(K) string) string {
	var out []string
	for k := range m {
		out = append(out, f(k))
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}

func main() {
	nan := math.NaN()
	negz := math.Copysign(0, -1)

	pm := map[padded]int{}
	pm[padded{a: 1, b: 2, c: true, d: 3}] = 1
	x := padded{a: 1, b: 2}
	x.c = true
	x.d = 3
	pm[x]++
	println("padded", len(pm), pm[x], x == padded{a: 1, b: 2, c: true, d: 3})
	arr := [3]padded{x, x, x}
	arr[1].d = 4
	println("padded arrays", arr[0] == arr[2], arr[0] == arr[1], [3]padded{x, x, x} == arr)

	fm := map[fkey]string{}
	fm[fkey{f: negz, s: "a"}] = "negz"
	fm[fkey{f: 0, s: "a"}] = "zero"
	fm[fkey{f: nan, s: "a"}] = "nan1"
	fm[fkey{f: nan, s: "a"}] = "nan2"
	fm[fkey{c: complex(float32(negz), 0), s: "c"}] = "c-negz"
	fm[fkey{c: 0, s: "c"}] = "c-zero"
	built := strings.Repeat("a", 1)
	fm[fkey{s: built}] = "built"
	println("fkey len", len(fm), fm[fkey{s: "a"}], fm[fkey{s: "c"}])
	var ksign, csign bool
	for k := range fm {
		if k.s == "a" && k.f == 0 {
			ksign = math.Signbit(k.f)
		}
		if k.s == "c" {
			csign = math.Signbit(float64(real(k.c)))
		}
	}
	println("kept key signbits", ksign, csign)
	println("fkey vals", keysOf(fm, func(k fkey) string { return fm[k] + "|" + strconv.FormatFloat(k.f, 'g', -1, 64) }))

	nm := map[nested]int{}
	n1 := nested{arr: [2]fkey{{f: 1}, {s: "z"}}, k: small{1, 2}}
	n2 := n1
	n2.k = small{1, 2}
	nm[n1] = 1
	nm[n2] = 2
	n3 := n1
	n3.k = [2]small{}
	nm[n3] = 3
	n4 := n1
	n4.arr[0].f = nan
	nm[n4] = 4
	nm[n4] = 5
	println("nested len", len(nm), nm[n1], nm[n3], n1 == n2, n4 == n4)
	try("nested unhashable", func() {
		bad := n1
		bad.k = []small{}
		nm[bad] = 6
	})
	try("nested cmp unhashable", func() {
		bad := n1
		bad.k = map[int]int{}
		println(bad == n1)
	})
	try("nested cmp same unhashable", func() {
		bad := n1
		bad.k = map[int]int{}
		println(bad == bad)
	})
	cl := maps.Clone(nm)
	println("clone", len(cl), cl[n1])
	for k := range cl {
		delete(cl, k)
	}
	println("delete-all leaves nan", len(cl))
	clear(cl)
	println("clear", len(cl))

	sm := map[small]int{}
	for i := 0; i < 1000; i++ {
		sm[small{uint8(i), uint16(i % 7)}]++
	}
	total := 0
	for _, v := range sm {
		total += v
	}
	println("small", len(sm), total, sm[small{3, 3}], sm[small{4, 4}])

	type arrKey [2]struct {
		f float32
		_ int8
	}
	ak := map[arrKey]int{}
	var k1 arrKey
	k1[0].f = float32(negz)
	ak[k1] = 1
	var k2 arrKey
	ak[k2] = 2
	println("arrKey", len(ak), ak[k1])
	for k := range ak {
		println("arrKey kept signbit", math.Signbit(float64(k[0].f)))
	}

	im := map[any]bool{}
	im[small{1, 2}] = true
	im[struct {
		x uint8
		y uint16
	}{1, 2}] = true
	im[padded{a: 1}] = true
	im[padded{a: 1}] = true
	println("iface struct keys", len(im))
}
