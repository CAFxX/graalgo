// Conversions between named struct types (tags ignored), named arrays and
// named element types, named funcs/maps/chans; method sets after
// conversion; type switches distinguishing identical underlying types.
package main

import "strconv"

type celsius float64
type kelvin float64

type pA struct {
	X int    `json:"x"`
	Y string `k:"v"`
}

type pB struct {
	X int
	Y string
}

type vec [3]celsius
type vecK [3]celsius
type lst []celsius
type dict map[string]pA
type op func(int) int
type ich chan int

func (c celsius) String() string { return strconv.FormatFloat(float64(c), 'f', 1, 64) + "C" }
func (k kelvin) String() string  { return strconv.FormatFloat(float64(k), 'f', 1, 64) + "K" }
func (a pA) Name() string        { return "pA:" + a.Y }
func (b pB) Name() string        { return "pB:" + b.Y }
func (v vec) Sum() celsius       { return v[0] + v[1] + v[2] }
func (v *vecK) Scale(f celsius) {
	for i := range v {
		v[i] *= f
	}
}
func (o op) twice(x int) int { return o(o(x)) }

type namer interface{ Name() string }
type stringer interface{ String() string }

func kind(x any) string {
	switch v := x.(type) {
	case celsius:
		return "celsius " + v.String()
	case kelvin:
		return "kelvin " + v.String()
	case float64:
		return "float64"
	case pA:
		return "pA"
	case pB:
		return "pB"
	case struct {
		X int
		Y string
	}:
		return "anon"
	case vec:
		return "vec"
	case vecK:
		return "vecK"
	case [3]celsius:
		return "[3]celsius"
	default:
		return "other"
	}
}

func main() {
	c := celsius(21.5)
	k := kelvin(c + 273.15)
	println(kind(c), kind(k), kind(float64(k)))
	var s stringer = c
	s2 := stringer(kelvin(s.(celsius)))
	println(s.String(), s2.String())

	a := pA{1, "a"}
	b := pB(a)
	b.Y = "b"
	anon := struct {
		X int
		Y string
	}(b)
	println(kind(a), kind(b), kind(anon), a.Y, b.Y, anon.Y)
	var n namer = pB(a)
	println(n.Name(), namer(pA(b)).Name())
	println("anon to pA eq", pA(anon) == pA{1, "b"}, any(pA(anon)) == any(pB(anon)))

	v := vec{1, 2, 3}
	vk := vecK(v)
	vk.Scale(2)
	println(kind(v), kind(vk), kind([3]celsius(v)), v.Sum().String(), vec(vk).Sum().String())
	raw := [3]celsius(vk)
	raw[0] = -1
	println("raw copy", vk[0].String(), raw[0].String())
	pv := (*vecK)(&v)
	pv.Scale(10)
	println("ptr conv aliases", v[2].String(), v.Sum().String())

	l := lst{1, 2}
	sl := []celsius(l)
	sl[0] = 9
	println("slice conv aliases", l[0].String(), len(sl))
	fl := []celsius(v[:])
	fl[1] = 0
	println("array slice", v[1].String())

	d := dict{"k": a}
	md := map[string]pA(d)
	md["j"] = pA{2, "j"}
	println("map conv aliases", len(d), d["j"].Name())

	sq := op(func(x int) int { return x * x })
	f := (func(int) int)(sq)
	println("func conv", sq.twice(3), f(5), op(f).twice(2))

	ch := make(ich, 2)
	var rch <-chan int = ch
	ch <- 7
	println("chan conv", <-(chan int)(ch), len(rch))

	type fahr celsius
	fh := fahr(c*9/5 + 32)
	println("nested named", float64(fh), kind(celsius(fh)))
	vals := []any{c, k, float64(c), a, b, anon, v, vk, [3]celsius{}, fh}
	cnt := map[string]int{}
	for _, x := range vals {
		cnt[kind(x)]++
	}
	println("other count", cnt["other"], "kinds", len(cnt))
	println("eq named any", any(c) == any(celsius(21.5)), any(c) == any(21.5), any(vec{}) == any(vecK{}))
	type bits uint8
	x := bits(200)
	println("named int ops", uint8(x+100), int8(x), bits(int8(x))>>1, -x)
}
