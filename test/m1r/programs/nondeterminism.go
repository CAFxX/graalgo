// The nondeterminism Go promises (m3-plan G6 mutants M-SELECT-01,
// M-MAPITER-01): a select with several ready cases picks one at random, and
// every range over a map starts at a random entry. Each property is printed
// as a boolean that gc makes true with probability 1 - 2^-60 or better; a
// biased select or a fixed iteration order prints false. (In test/m1r, not
// test/m1a: the reference interpreter girinterp, which differentials every
// m1a program, picks select cases in order by design.)
package main

func main() {
	a, b := make(chan int, 1), make(chan int, 1)
	na, nb := 0, 0
	for i := 0; i < 2000; i++ {
		a <- 1
		b <- 1
		select {
		case <-a:
			na++
			<-b
		case <-b:
			nb++
			<-a
		}
	}
	println("select picks both ready cases:", na > 200 && nb > 200, na+nb)

	m := map[int]int{}
	for i := 0; i < 16; i++ {
		m[i] = i
	}
	firsts := map[int]bool{}
	for r := 0; r < 64; r++ {
		for k := range m {
			firsts[k] = true
			break
		}
	}
	println("map range start varies:", len(firsts) > 1)

	// Deleting entries not yet reached during a range: none of them is
	// produced later, and every surviving entry is produced exactly once.
	seen := map[int]int{}
	deleted := map[int]bool{}
	for k := range m {
		seen[k]++
		for j := 0; j < 16; j++ {
			if _, ok := seen[j]; !ok && !deleted[j] && j%3 == 0 {
				delete(m, j)
				deleted[j] = true
			}
		}
	}
	bad := 0
	for k, n := range seen {
		if n != 1 || deleted[k] {
			bad++
		}
	}
	println("delete during range:", bad, len(seen)+len(deleted) <= 16)
}
