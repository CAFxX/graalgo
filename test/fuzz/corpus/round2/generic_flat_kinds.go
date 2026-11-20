// Generic code instantiated with complex64/128, float32, arrays, structs
// containing arrays, and interfaces: == under comparable (NaN, -0, runtime
// panics for any holding slices), generic map keys, zero values, copies.
package main

import (
	"cmp"
	"math"
	"slices"
	"strconv"
)

type grid [2][2]float32

type rec struct {
	tag  string
	cell [3]int16
	w    complex64
}

type number interface {
	~int8 | ~float32 | ~float64 | ~complex64 | ~complex128
}

func eq[T comparable](a, b T) bool { return a == b }

func index[T comparable](xs []T, v T) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

func count[K comparable](keys []K) int {
	m := map[K]int{}
	for _, k := range keys {
		m[k]++
	}
	return len(m)
}

func sum[T number](xs ...T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s
}

func mulAll[T number](xs []T, f T) {
	for i := range xs {
		xs[i] *= f
	}
}

func swap[T any](p, q *T) { *p, *q = *q, *p }

func zero[T any]() (z T) { return }

type box[T any] struct {
	v    T
	next *box[T]
}

func (b *box[T]) push(v T) *box[T] { return &box[T]{v, b} }

func (b *box[T]) all() []T {
	var out []T
	for p := b; p != nil; p = p.next {
		out = append(out, p.v)
	}
	return out
}

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(name, "->", r.(interface{ Error() string }).Error())
		}
	}()
	f()
}

func main() {
	nan := float32(math.NaN())
	negz := float32(math.Copysign(0, -1))
	println("eq f32 nan", eq(nan, nan), "negz", eq(negz, 0))
	println("eq c64", eq(complex64(complex(1, nan)), complex64(complex(1, nan))), eq(complex(negz, 1), complex64(1i)))
	println("eq grid", eq(grid{{1, 2}, {3, negz}}, grid{{1, 2}, {3, 0}}), eq(grid{{nan}}, grid{{nan}}))
	r1 := rec{"a", [3]int16{1, 2, 3}, 1 + 2i}
	r2 := r1
	r2.cell[2] = 4
	println("eq rec", eq(r1, r1), eq(r1, r2), r1.cell[2])
	println("eq any", eq[any](1, 1), eq[any](1, int8(1)), eq[any](grid{}, grid{}))
	try("eq any slice", func() { eq[any]([]int{}, []int{}) })
	try("eq any mixed", func() { println("mixed", eq[any]([]int{}, 1)) })
	try("eq [1]any", func() { eq([1]any{map[int]int{}}, [1]any{map[int]int{}}) })

	println("index", index([]grid{{}, {{1}}, {{nan}}}, grid{{1}}), index([]grid{{{nan}}}, grid{{nan}}))
	println("index rec", index([]rec{r2, r1}, r1))
	println("count f32", count([]float32{nan, nan, 0, negz, 1}))
	println("count c128", count([]complex128{complex(math.NaN(), 0), 0, complex(0, -0.0), 1i}))
	println("count arr", count([][2]float64{{1, 2}, {1, 2}, {math.NaN(), 2}, {math.NaN(), 2}}))
	println("count any", count([]any{1, 1.0, "1", int8(1), grid{}, grid{}, nil, nil}))
	try("count any unhashable", func() { count([]any{1, []int{}}) })

	println("sum int8", sum[int8](100, 27, 1))
	println("sum f32", strconv.FormatFloat(float64(sum[float32](0.1, 0.2, 0.3)), 'g', -1, 32))
	s64 := sum[complex64](complex(0.1, 0.7), complex(0.2, 0.1), complex(1e8, 0))
	println("sum c64", real(s64), imag(s64))
	c := []complex64{complex(1, 2), complex(0.1, 0.3)}
	mulAll(c, complex(0.7, -0.9))
	println("mul c64", math.Float32bits(real(c[0])), math.Float32bits(imag(c[1])))
	type myf float64
	println("sum myf", float64(sum[myf](1.5, 2.25)))

	g1, g2 := grid{{1, 2}, {3, 4}}, grid{{5, 6}, {7, 8}}
	swap(&g1, &g2)
	swap(&g1[0], &g1[1])
	println("swap grid", g1[0][0], g1[1][1], g2[0][1])
	ra, rb := r1, r2
	swap(&ra.cell, &rb.cell)
	println("swap cell", ra.cell[2], rb.cell[2], r1.cell[2])

	println("zero", zero[complex64]() == 0, zero[grid]() == grid{}, zero[any]() == nil, zero[*rec]() == nil)
	zr := zero[rec]()
	println("zero rec", zr.tag == "", zr.cell[1], real(zr.w))

	var b *box[grid]
	for i := 0; i < 3; i++ {
		g := grid{{float32(i)}}
		b = b.push(g)
		g[0][0] = 99
	}
	for _, g := range b.all() {
		print(g[0][0], " ")
	}
	println()

	gs := []grid{{{3}}, {{1}}, {{2}}}
	slices.SortFunc(gs, func(a, b grid) int { return cmp.Compare(a[0][0], b[0][0]) })
	println("sorted", gs[0][0][0], gs[1][0][0], gs[2][0][0])
	fs := []float32{3, nan, -1, negz, 0, 2}
	slices.Sort(fs)
	for _, f := range fs {
		print(strconv.FormatFloat(float64(f), 'g', -1, 32), " ")
	}
	println()
	println("min/max", slices.Min([]float64{2, math.NaN(), 1}) != 0, max(float32(1), nan) == nan, min(negz, 0) == 0)
	println("signbit min", math.Signbit(float64(min(float32(0), negz))), math.Signbit(float64(max(negz, float32(0)))))
	println("compare nan", cmp.Compare(nan, 0), cmp.Compare(float32(0), nan), cmp.Compare(nan, nan), cmp.Less(nan, 0))
	println("contains", slices.Contains([]any{grid{}, 1}, any(grid{})), slices.Index([]float32{nan}, nan))
}
