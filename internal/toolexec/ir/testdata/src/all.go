// Package all exercises every GIR node kind: the golden vector all.gir is
// emitted from this file, and the Go and Java test suites decode it.
package all

import (
	"unsafe"

	"example.com/dep"
)

//go:linkname runtimeNano runtime.nanotime
func runtimeNano() int64

const big = 1 << 70 >> 68

var (
	b                  = a + 1 // initialized after a (Info.InitOrder)
	a                  = seed()
	_                  = sink(b)
	pair, k            = two()
	ch                 = make(chan int, 4)
	recvCh  <-chan int = ch
)

func seed() int          { return 41 }
func sink(int) bool      { return true }
func two() (int, string) { return 1, "x" }

func init() { println("init", a, b, big) }

type Point struct{ X, Y int }

func (p Point) Sum() int     { return p.X + p.Y }
func (p *Point) Scale(k int) { p.X *= k; p.Y *= k }

type Inner struct{ N int }

func (i *Inner) Bump() { i.N++ }

type Outer struct {
	Inner
	Name string
}

type Shape interface{ Sum() int }

type List[T any] struct {
	items []T
}

func (l *List[T]) Push(v T) { l.items = append(l.items, v) }
func (l *List[T]) Len() int { return len(l.items) }

type Number interface{ ~int | ~float64 }

func Max[T Number](xs ...T) T {
	m := xs[0]
	for _, x := range xs[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

type Stringer interface{ String() string }

type name string

func (n name) String() string { return string(n) }

func Show[T Stringer](v T) string { return v.String() }

func SizeOf[T any](v T) uintptr { return unsafe.Sizeof(v) + unsafe.Alignof(v) }

type pairT[T any] struct{ A, B T }

func OffB[T any](p pairT[T]) uintptr { return unsafe.Offsetof(p.B) }

func pt() (int, *Point) { return 1, nil }

func boxed() (any, Shape) { return pt() }

func Closures() func() int {
	n := 0
	inc := func() int {
		n++
		return func() int { return n }()
	}
	return inc
}

func Seq(yield func(int) bool) {
	for i := range 3 {
		if !yield(i) {
			return
		}
	}
}

func Control(xs []int, m map[string]int, s string) (total int) {
	defer func() {
		if r := recover(); r != nil {
			total = -1
		}
	}()
	var i interface{} = xs
	if v, ok := i.([]int); ok {
		total += len(v)
	}
	switch t := i.(type) {
	case nil:
	case []int, string:
		_ = t
	default:
		total++
	}
	switch {
	case total > 3:
		total--
		fallthrough
	case total > 1:
		total++
	default:
	}
	switch total {
	case 1, 2:
	}
	var e error
	if e == nil && i != nil && i != 3 {
		total <<= 1
	}
outer:
	for j := 0; j < len(xs); j++ {
		for _, x := range xs {
			if x == 0 {
				continue outer
			}
			if x < 0 {
				break outer
			}
		}
	}
	for k, v := range m {
		_, _ = k, v
	}
	for range s {
	}
	for x := range Seq {
		total += x
	}
	if v, ok := m["a"]; ok {
		total += v
	}
	c := make(chan int, 1)
	c <- 1
	select {
	case v, ok := <-c:
		_, _ = v, ok
	case c <- 2:
	default:
	}
	go func() {}()
	n := 0
loop:
	n++
	if n < 3 {
		goto loop
	}
	arr := [4]int{1, 3: 4}
	sl := arr[1:3:4]
	p := &Point{X: 1, Y: 2}
	p.Scale(2)
	o := Outer{Inner: Inner{N: 1}, Name: "o"}
	o.Bump()
	o2 := Outer{N: 5}
	_ = o2
	f := p.Sum
	g := (*Point).Scale
	h := Shape.Sum
	var sh Shape = *p
	hv := sh.Sum
	g(p, 3)
	total += f() + h(sh) + hv() + sh.Sum()
	l := &List[int]{}
	l.Push(1)
	total += l.Len() + Max(1, 2) + Max[int](3) + int(Max(1.5, 2.5))
	_ = Show(name("x"))
	ps := []*Point{{1, 2}, {X: 3}}
	mm := map[Point]string{{1, 2}: "a"}
	sl = append(sl, 1, 2)
	sl = append(sl, sl...)
	copy(sl, arr[:])
	delete(m, "x")
	clear(mm)
	fl := 1.5
	z := complex(fl, fl)
	{
		dep.Counter++
		total += len(dep.Hello()) + dep.Wrap(2).Get()
	}
	total += int(real(z)+imag(z)) + min(1, total) + max(total, 2) + cap(sl) + len(ps)
	np := new(int)
	*np = ^total
	total = -*np + +total
	bs := unsafe.Slice(&arr[0], 2)
	str := unsafe.String(unsafe.StringData(s), len(s))
	_ = unsafe.Add(unsafe.Pointer(unsafe.SliceData(bs)), 8)
	_ = str
	var ro <-chan int = c
	_ = ro
	close(c)
	x, y := boxed()
	_, _ = x, y
	b := !(total > 0) || total < 0
	_ = b
	print(SizeOf(total), OffB(pairT[int]{}))
	if total > 100 {
		panic("big")
	}
	type local struct{ v int }
	lv := local{v: 1}
	total += lv.v
	return
}
