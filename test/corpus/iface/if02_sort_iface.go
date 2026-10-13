package main

import "fmt"

// sort.Interface inlined: import "sort" is out of scope (the frontend
// importer resolves only the synthetic fmt/sync/runtime overlays), so the
// test defines the interface and a small sort over it. The Less/Swap/Len
// call sites inside Sort each see two dynamic types (bimorphic).
type Interface interface {
	Len() int
	Less(i, j int) bool
	Swap(i, j int)
}

// Insertion sort over the interface (small n; the dispatch is the point).
func Sort(data Interface) {
	n := data.Len()
	for i := 1; i < n; i++ {
		for j := i; j > 0 && data.Less(j, j-1); j-- {
			data.Swap(j, j-1)
		}
	}
}

type IntSlice []int

func (s IntSlice) Len() int            { return len(s) }
func (s IntSlice) Less(i, j int) bool  { return s[i] < s[j] }
func (s IntSlice) Swap(i, j int)       { s[i], s[j] = s[j], s[i] }

type RevInts []int

func (s RevInts) Len() int            { return len(s) }
func (s RevInts) Less(i, j int) bool  { return s[i] > s[j] }
func (s RevInts) Swap(i, j int)       { s[i], s[j] = s[j], s[i] }

func printAll(s IntSlice) {
	for i := 0; i < len(s); i++ {
		fmt.Println(s[i])
	}
}

func printAllRev(s RevInts) {
	for i := 0; i < len(s); i++ {
		fmt.Println(s[i])
	}
}

func main() {
	a := IntSlice{5, 2, 8, 1, 9, 3}
	Sort(a)
	printAll(a)
	r := RevInts{5, 2, 8, 1, 9, 3}
	Sort(r)
	printAllRev(r)
}
