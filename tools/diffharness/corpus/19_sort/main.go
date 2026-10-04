package main

import (
	"fmt"
	"sort"
)

func main() {
	nums := []int{5, 3, 8, 1, 9, 2}
	sort.Ints(nums)
	fmt.Println(nums)
	fmt.Println(sort.SearchInts(nums, 8))
	names := []string{"delta", "alpha", "charlie", "bravo"}
	sort.Strings(names)
	fmt.Println(names)
	type pair struct {
		k string
		v int
	}
	ps := []pair{{"b", 2}, {"a", 3}, {"c", 1}}
	sort.Slice(ps, func(i, j int) bool { return ps[i].v < ps[j].v })
	fmt.Println(ps)
	fmt.Println(sort.IntsAreSorted(nums))
}
