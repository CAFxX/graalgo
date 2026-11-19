// Targets methods on non-struct named types (slices, maps, funcs, ints,
// arrays, strings), method values bound at evaluation time (receiver copy
// for value receivers, pointer for pointer receivers), method expressions,
// and interfaces embedded in structs dispatching to the dynamic value.
package main

import (
	"sort"
	"strconv"
	"strings"
)

type IntList []int

func (l IntList) Sum() int {
	s := 0
	for _, v := range l {
		s += v
	}
	return s
}
func (l *IntList) Push(v int) { *l = append(*l, v) }
func (l IntList) Set0(v int)  { l[0] = v } // shares backing array

type Counter map[string]int

func (c Counter) Add(k string) { c[k]++ }
func (c Counter) Keys() string {
	ks := make([]string, 0, len(c))
	for k := range c {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

type Op func(int, int) int

func (o Op) Apply3(a, b, c int) int { return o(o(a, b), c) }

type Celsius float64

func (c Celsius) F() float64      { return float64(c)*9/5 + 32 }
func (c *Celsius) Warm(d float64) { *c += Celsius(d) }

type Vec [3]int

func (v Vec) Dot(w Vec) int { return v[0]*w[0] + v[1]*w[1] + v[2]*w[2] }
func (v *Vec) Scale(k int) {
	for i := range v {
		v[i] *= k
	}
}
func (v Vec) ScaleCopy(k int) Vec {
	v.Scale(k)
	return v
}

type Str string

func (s Str) Up() string { return strings.ToUpper(string(s)) }

type Bits uint8

func (b Bits) Has(i uint) bool { return b&(1<<i) != 0 }
func (b Bits) String() string {
	s := ""
	for i := uint(0); i < 8; i++ {
		if b.Has(i) {
			s += "1"
		} else {
			s += "0"
		}
	}
	return s
}

type Stringer interface{ String() string }

type Logger struct {
	Stringer // embedded interface
	prefix   string
}

func (l Logger) Log() string { return l.prefix + l.String() }

type Override struct {
	Stringer
}

func (Override) String() string { return "override" }

func safe(f func() string) (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = "panic:" + r.(interface{ Error() string }).Error()
		}
	}()
	return f()
}

func main() {
	l := IntList{1, 2, 3}
	sum := l.Sum // bound to copy of slice header
	l.Push(4)
	println(sum(), l.Sum(), len(l))
	l.Set0(10)
	println(sum(), l.Sum())
	push := l.Push // binds &l
	push(5)
	push(6)
	println(len(l), l.Sum())
	pushExpr := (*IntList).Push
	pushExpr(&l, 7)
	println(IntList.Sum(l), len(l))

	c := Counter{}
	add := c.Add
	add("b")
	add("a")
	c.Add("b")
	println(c.Keys(), c["b"], Counter.Keys(c))

	var plus Op = func(a, b int) int { return a + b }
	var mul Op = func(a, b int) int { return a * b }
	println(plus.Apply3(1, 2, 3), mul.Apply3(2, 3, 4), Op.Apply3(plus, 4, 5, 6))
	ap := mul.Apply3
	mul = plus
	println(ap(2, 2, 2), mul.Apply3(2, 2, 2))

	t := Celsius(100)
	f := t.F
	t.Warm(10)
	println(int(f()), int(t.F()))
	w := t.Warm
	w(5)
	w(5)
	println(int(t))

	v := Vec{1, 2, 3}
	dot := v.Dot
	v.Scale(2)
	println(dot(Vec{1, 1, 1}), v.Dot(Vec{1, 1, 1}))
	sc := v.ScaleCopy(10)
	println(v[0], sc[0], sc[2])
	arr := [2]Vec{{1, 1, 1}, {2, 2, 2}}
	arr[1].Scale(3)
	println(arr[1][0], arr[0].Dot(arr[1]))
	vs := []Vec{{1, 0, 0}}
	vs[0].Scale(9)
	println(vs[0][0])
	scale := (*Vec).Scale
	scale(&vs[0], 2)
	println(vs[0][0])

	var s Str = "hello"
	up := s.Up
	s = "bye"
	println(up(), s.Up(), Str.Up("x"))

	var b Bits = 0b10100101
	println(b.String(), b.Has(0), b.Has(1), Bits(255).String())
	var st Stringer = b
	b = 0
	println(st.String())

	lg := Logger{Stringer: Bits(3), prefix: "bits="}
	println(lg.Log())
	lg.Stringer = Bits(128)
	println(lg.Log())
	lg2 := lg
	lg.Stringer = Override{}
	println(lg.Log(), lg2.Log())
	var empty Logger
	println(safe(empty.Log))
	ov := Override{}
	println(ov.String(), safe(func() string { return ov.Stringer.String() }))

	// method value from interface: binds dynamic value now
	var st2 Stringer = Bits(1)
	sf := st2.String
	st2 = Bits(2)
	println(sf(), st2.String())

	// method value on nil interface panics at evaluation
	println(safe(func() string {
		var ni Stringer
		g := ni.String
		return "bound:" + g()
	}))

	// map of method values
	ops := map[string]func(int, int, int) int{"plus": plus.Apply3, "mul": Op(func(a, b int) int { return a*b + 1 }).Apply3}
	keys := []string{"mul", "plus"}
	for _, k := range keys {
		println(k, ops[k](1, 2, 3))
	}

	// interface conversion of named non-struct values with pointer methods
	var warmer interface{ Warm(float64) } = &t
	warmer.Warm(1)
	println(strconv.Itoa(int(t)))
}
