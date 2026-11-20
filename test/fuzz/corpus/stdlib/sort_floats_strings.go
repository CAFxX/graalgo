// Targets sort.Float64s / Float64Slice with NaN, -0, +-Inf and float32
// values (NaN ordering, -0 vs +0 bit patterns preserved), SearchFloat64s,
// sort.Strings / SearchStrings with non-ASCII, empty and prefix strings,
// and StringSlice.Less; prints bit patterns so sign of zero is observable.
package main

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

type f32s []float32

func (a f32s) Len() int           { return len(a) }
func (a f32s) Less(i, j int) bool { return a[i] < a[j] || (a[i] != a[i] && a[j] == a[j]) }
func (a f32s) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

func pbits(a []float64) {
	for _, v := range a {
		print(math.Float64bits(v), " ")
	}
	println()
}

func main() {
	nan := math.NaN()
	negz := math.Copysign(0, -1)
	inf := math.Inf(1)
	a := []float64{3, nan, -1, negz, 0, inf, -inf, nan, 2.5, negz, -0.5, 1e-310, -1e-310, math.MaxFloat64, math.SmallestNonzeroFloat64}
	b := append([]float64(nil), a...)
	sort.Float64s(b)
	pbits(b)
	println(sort.Float64sAreSorted(b), sort.Float64sAreSorted(a))
	for _, v := range b {
		print(math.IsNaN(v), math.Signbit(v), " ")
	}
	println()
	for _, p := range []float64{-inf, -1, negz, 0, 2.5, 3, inf, nan, 1e300} {
		print(sort.SearchFloat64s(b, p), " ")
	}
	println()
	c := append([]float64(nil), a...)
	sort.Sort(sort.Reverse(sort.Float64Slice(c)))
	pbits(c)
	fs := sort.Float64Slice{nan, 1, nan}
	println(fs.Less(0, 1), fs.Less(1, 0), fs.Less(0, 2), fs.Search(1))

	// many NaNs mixed into a large slice
	var big []float64
	s := uint64(99)
	for i := 0; i < 3000; i++ {
		s = s*2862933555777941757 + 3037000493
		switch s % 11 {
		case 0:
			big = append(big, nan)
		case 1:
			big = append(big, negz)
		case 2:
			big = append(big, 0)
		default:
			big = append(big, float64(int64(s>>20)%2000-1000)/7)
		}
	}
	sort.Float64s(big)
	var h uint64 = 1469598103934665603
	nn := 0
	for _, v := range big {
		if v != v {
			nn++
			continue
		}
		h = (h ^ math.Float64bits(v)) * 1099511628211
	}
	println("big", h, nn, sort.Float64sAreSorted(big), math.IsNaN(big[0]), math.IsNaN(big[nn-1]), math.IsNaN(big[nn]))

	f := f32s{float32(nan), 1.5, -2, float32(negz), 0, float32(inf), 1e-40, -1e-40}
	sort.Sort(f)
	for _, v := range f {
		print(math.Float32bits(v), " ")
	}
	println()

	st := []string{"b", "", "a", "ab", "aa", "a\x00", "\xff", "\u00e9", "e", "E", "zz", "z", "\u65e5\u672c", "\u65e5", "\u00e9\u0301", "A", "aB", "Ab"}
	sort.Strings(st)
	for _, v := range st {
		print(strconv.Quote(v), " ")
	}
	println()
	println(sort.StringsAreSorted(st))
	for _, p := range []string{"", "a", "aa", "b", "zzz", "\u00e9", "\xff\xff"} {
		print(sort.SearchStrings(st, p), " ")
	}
	println()
	ss := sort.StringSlice{"x", "X", "xx"}
	println(ss.Less(0, 1), ss.Less(0, 2), ss.Search("xa"))
	ss.Sort()
	println(strings.Join(ss, ","))

	// many strings of increasing length with shared prefixes
	var words []string
	for i := 0; i < 2000; i++ {
		w := strings.Repeat(string(rune('a'+i%5)), i%7) + string(rune('A'+(i*7)%26))
		words = append(words, w)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(words)))
	h = 1469598103934665603
	for _, w := range words {
		for j := 0; j < len(w); j++ {
			h = (h ^ uint64(w[j])) * 1099511628211
		}
		h = (h ^ 0xff) * 1099511628211
	}
	println("words", h, words[0], words[len(words)-1], sort.IsSorted(sort.Reverse(sort.StringSlice(words))))
}
