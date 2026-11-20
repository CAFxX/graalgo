// Value receivers called through interfaces: a method that only reads its
// receiver may get the interface's data word itself (lane perf-calls); one
// that writes its receiver, takes its address, slices it or calls a pointer
// method on it must get a private copy, so the interface's value never
// changes. Monomorphic, polymorphic and megamorphic call sites, hot enough
// to compile.
package main

type I interface{ M() int }

type ro struct{ a, b int }
type wr struct{ a, b int }
type addr struct{ a [4]int }
type sl struct{ a [4]int }
type pm struct{ a int }
type inc struct{ a int }
type rng struct{ a [3]int }

func (s ro) M() int { return s.a + s.b }

func (s wr) M() int {
	s.a += 10
	return s.a
}

func bump(p *int) { *p += 100 }

func (s addr) M() int {
	bump(&s.a[1])
	return s.a[1]
}

func (s sl) M() int {
	x := s.a[:]
	x[2] += 1000
	return s.a[2]
}

func (s *pm) grow() { s.a += 7 }

func (s pm) M() int {
	s.grow()
	return s.a
}

func (s inc) M() int {
	s.a++
	return s.a
}

func (s rng) M() int {
	t := 0
	for i := range s.a {
		s.a[i] = i * 3
		t += s.a[i]
	}
	return t
}

func run(xs []I) int {
	t := 0
	for _, x := range xs {
		t += x.M()
	}
	return t
}

func main() {
	all := []I{ro{1, 2}, wr{3, 4}, addr{[4]int{1, 2, 3, 4}}, sl{[4]int{5, 6, 7, 8}}, pm{9}, inc{11}, rng{[3]int{1, 1, 1}}}
	for _, x := range all {
		mono := []I{x, x, x}
		t := 0
		for r := 0; r < 20000; r++ {
			t += run(mono)
		}
		println(t)
	}
	t := 0
	for r := 0; r < 20000; r++ {
		t += run(all)
	}
	println(t)
	for _, x := range all {
		println(x.M())
	}
}
