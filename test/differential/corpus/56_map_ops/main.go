// 56_map_ops: map insert/overwrite/delete/len, missing-key zero values,
// comma-ok, struct and array keys, NaN keys, nil-map read and write
// panic, iteration combined order-independently, clear.
package main

import (
	"fmt"
	"math"
)

type pt struct{ x, y int }

func sortedKeys(m map[string]int) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	for i := 1; i < len(ks); i++ {
		for j := i; j > 0 && ks[j] < ks[j-1]; j-- {
			ks[j], ks[j-1] = ks[j-1], ks[j]
		}
	}
	return ks
}

func main() {
	m := map[string]int{"a": 1, "b": 2}
	m["c"] = 3
	m["a"] += 10
	delete(m, "b")
	delete(m, "zz")
	v, ok := m["b"]
	w, ok2 := m["a"]
	fmt.Println(len(m), v, ok, w, ok2, m["missing"])
	for _, k := range sortedKeys(m) {
		fmt.Println(k, m[k])
	}
	pm := map[pt]string{{1, 2}: "a", {2, 1}: "b"}
	pm[pt{1, 2}] = "c"
	fmt.Println(len(pm), pm[pt{1, 2}], pm[pt{2, 1}], pm[pt{0, 0}] == "")
	am := map[[2]int]int{}
	for i := 0; i < 10; i++ {
		am[[2]int{i % 3, i % 2}]++
	}
	fmt.Println(len(am), am[[2]int{0, 0}], am[[2]int{2, 1}])
	fm := map[float64]int{}
	nan := math.NaN()
	fm[nan] = 1
	fm[nan] = 2
	fm[0.0] = 3
	fm[math.Copysign(0, -1)] = 4
	fmt.Println(len(fm), fm[0])
	im := map[int]int{}
	for i := 0; i < 1000; i++ {
		im[i*7%1000] = i
	}
	sum, ksum := 0, 0
	for k, v := range im {
		sum += v
		ksum += k
	}
	fmt.Println(len(im), sum, ksum)
	for k := range im {
		if k%2 == 0 {
			delete(im, k)
		}
	}
	fmt.Println(len(im))
	clear(im)
	fmt.Println(len(im))
	var nm map[string]int
	fmt.Println(nm["x"], len(nm))
	defer func() { fmt.Println("recovered:", recover()) }()
	nm["x"] = 1
}
