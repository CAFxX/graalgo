package main

// L11 (REVIEW-2026-11-18-05): index/suffixarray — zero corpus imports
// before this program. Substring search over a fixed byte string:
// Lookup for present and absent needles, FindAllIndex with limits,
// and the Bytes accessor.

import (
	"fmt"
	"index/suffixarray"
	"regexp"
	"sort"
)

func main() {
	data := []byte("the quick brown fox jumps over the lazy dog; the fox is quick")
	idx := suffixarray.New(data)
	fmt.Printf("len=%d\n", len(idx.Bytes()))
	for _, needle := range []string{"fox", "the", "quick", "cat", "dog", ""} {
		offs := idx.Lookup([]byte(needle), -1)
		sort.Ints(offs)
		fmt.Printf("lookup %q => %v\n", needle, offs)
	}
	fmt.Printf("lookup fox limit1 => %v\n", idx.Lookup([]byte("fox"), 1))
	fmt.Printf("lookup fox limit0 => %v\n", idx.Lookup([]byte("fox"), 0))
	// FindAllIndex: [start, end) pairs from a regexp, sorted.
	pairs := idx.FindAllIndex(regexp.MustCompile("the"), -1)
	fmt.Printf("findall the => %v\n", pairs)
	fmt.Printf("findall missing => %v\n", idx.FindAllIndex(regexp.MustCompile("zzz"), -1))
	fmt.Printf("findall fox-limit1 => %v\n", idx.FindAllIndex(regexp.MustCompile("fox"), 1))
	// Empty data.
	empty := suffixarray.New([]byte{})
	fmt.Printf("empty lookup => %v\n", empty.Lookup([]byte("x"), -1))
}
