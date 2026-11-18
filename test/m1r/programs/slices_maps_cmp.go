package main

import (
	"cmp"
	"maps"
	"slices"
)

type P struct {
	Name string
	Age  int
}

func show[T any](label string, s []T, f func(T)) {
	print(label, " [")
	for _, v := range s {
		f(v)
		print(" ")
	}
	println("]", len(s))
}
func pi(i int)     { print(i) }
func ps(s string)  { print(s) }
func pp(p P)       { print(p.Name, ":", p.Age) }
func pf(f float64) { print(f) }

func main() {
	s := []int{5, 2, 8, 1, 9, 3, 7, 3, 0, -4, 6}
	t := slices.Clone(s)
	slices.Sort(t)
	show("sorted", t, pi)
	println(slices.IsSorted(t), slices.IsSorted(s), slices.Contains(s, 9), slices.Index(s, 3), slices.Max(s), slices.Min(s))
	i, ok := slices.BinarySearch(t, 7)
	println(i, ok, slices.Equal(s, t), slices.Compare(s, t), cmp.Compare(3, 5), cmp.Less("b", "a"), cmp.Compare(2.5, 2.5))
	slices.Reverse(t)
	show("rev", t, pi)
	show("compact", slices.Compact(slices.Clone(t)), pi)
	big := make([]int, 0, 500)
	x := uint32(12345)
	for range 500 {
		x = x*1664525 + 1013904223
		big = append(big, int(x>>16)%1000)
	}
	slices.Sort(big)
	println(slices.IsSorted(big), big[0], big[250], big[499])
	ps2 := []P{{"bob", 31}, {"alice", 25}, {"carol", 31}, {"dave", 25}, {"eve", 40}}
	slices.SortFunc(ps2, func(a, b P) int { return cmp.Or(cmp.Compare(a.Age, b.Age), cmp.Compare(a.Name, b.Name)) })
	show("people", ps2, pp)
	slices.SortStableFunc(ps2, func(a, b P) int { return -cmp.Compare(a.Age, b.Age) })
	show("stable", ps2, pp)
	j, found := slices.BinarySearchFunc(ps2, 31, func(p P, age int) int { return -cmp.Compare(p.Age, age) })
	println(j, found, slices.IndexFunc(ps2, func(p P) bool { return p.Name == "eve" }), slices.ContainsFunc(ps2, func(p P) bool { return p.Age > 50 }))
	strs := []string{"pear", "apple", "fig", "banana", "apple"}
	slices.Sort(strs)
	show("strs", strs, ps)
	strs = slices.Compact(strs)
	show("uniq", strs, ps)
	show("grow", slices.Grow(strs, 10)[:len(strs)], ps)
	show("concat", slices.Concat(s[:2], s[5:7], []int{100}), pi)
	show("repeat", slices.Repeat([]string{"x", "y"}, 3), ps)
	fl := []float64{2.5, -1, 3.25, 0}
	slices.Sort(fl)
	show("floats", fl, pf)
	println(slices.MaxFunc(ps2, func(a, b P) int { return cmp.Compare(a.Name, b.Name) }).Name, slices.EqualFunc(s, s, func(a, b int) bool { return a == b }))
	show("delfunc", slices.DeleteFunc(slices.Clone(s), func(v int) bool { return v%2 == 0 }), pi)
	show("clip", slices.Clip(s[:3]), pi)
	println(cap(slices.Clip(s[:3])), min(3, 1, 2), max(2.5, 1))

	m := map[string]int{"a": 1, "b": 2, "c": 3}
	m2 := maps.Clone(m)
	m2["d"] = 4
	println(maps.Equal(m, m2), len(m2), maps.EqualFunc(m, m, func(a, b int) bool { return a == b }))
	maps.DeleteFunc(m2, func(k string, v int) bool { return v%2 == 1 })
	println(len(m2), m2["b"], m2["d"])
	maps.Copy(m2, map[string]int{"z": 26})
	println(len(m2), m2["z"], m2["b"])
	show("ins", slices.Insert([]int{1, 2, 3}, 1, 9, 8), pi)
	show("del", slices.Delete([]int{1, 2, 3, 4, 5}, 1, 3), pi)
	show("repl", slices.Replace([]int{1, 2, 3, 4, 5}, 1, 3, 7, 7, 7), pi)
}
