// Targets anonymous structs (literals, nested, in slices/maps, as map keys
// and func params), struct conversion ignoring tags, zero-size structs and
// arrays in fields/slices/maps/channels-free containers, arrays of
// zero-size elements, and struct fields of array-of-struct type.
package main

import "strconv"

type Tagged struct {
	A int    `json:"a"`
	B string `json:"b"`
}

type Plain struct {
	A int
	B string
}

type Z struct{}

type WithZ struct {
	Z
	Pad [0]int64
	V   int
	E   [3]Z
}

func (Z) Hello() string { return "hello from Z" }

//go:noinline
func takeAnon(p struct {
	X, Y int
	Tag  string
}) int {
	p.X *= 10
	return p.X + p.Y + len(p.Tag)
}

//go:noinline
func makeAnon(n int) struct {
	N   int
	Arr [2]struct{ Lo, Hi int }
} {
	var r struct {
		N   int
		Arr [2]struct{ Lo, Hi int }
	}
	r.N = n
	r.Arr[0].Lo, r.Arr[0].Hi = n-1, n+1
	r.Arr[1] = struct{ Lo, Hi int }{n * 2, n * 3}
	return r
}

func main() {
	pt := struct {
		X, Y int
		Tag  string
	}{1, 2, "abc"}
	println(takeAnon(pt), pt.X)

	r := makeAnon(5)
	r2 := r
	r2.Arr[1].Hi = 99
	println(r.N, r.Arr[0].Lo, r.Arr[0].Hi, r.Arr[1].Lo, r.Arr[1].Hi, r2.Arr[1].Hi)
	println(r == makeAnon(5), r2 == r)

	// slices and maps of anonymous structs
	people := []struct {
		Name string
		Age  int
	}{{"bob", 30}, {"al", 25}, {"cy", 35}}
	people[1].Age++
	oldest := people[0]
	for _, p := range people {
		if p.Age > oldest.Age {
			oldest = p
		}
	}
	println(oldest.Name, oldest.Age, people[1].Age)

	grid := map[struct{ R, C int }]string{}
	grid[struct{ R, C int }{1, 2}] = "a"
	grid[struct{ R, C int }{2, 1}] = "b"
	grid[struct{ R, C int }{1, 2}] += "!"
	println(len(grid), grid[struct{ R, C int }{1, 2}], grid[struct{ R, C int }{2, 1}], grid[struct{ R, C int }{0, 0}] == "")

	// conversion ignoring tags; struct -> anonymous -> struct
	t := Tagged{7, "seven"}
	p := Plain(t)
	p.A = 8
	an := struct {
		A int
		B string
	}(p)
	back := Tagged(an)
	println(t.A, p.A, an.A, back.A, back.B, Plain(back) == p)
	ptrConv := (*Plain)(&t)
	ptrConv.B = "via-ptr"
	println(t.B)

	// zero-size types
	var wz WithZ
	wz.V = 3
	println(wz.Hello(), wz.V, len(wz.E), len(wz.Pad), wz.E[2].Hello())
	zs := make([]Z, 5)
	zs = append(zs, Z{}, Z{})
	println(len(zs), cap(zs) >= 7, zs[6] == Z{})
	za := [1000]Z{}
	println(len(za), za[999] == za[0])
	zm := map[Z]int{}
	zm[Z{}]++
	zm[Z{}]++
	println(len(zm), zm[Z{}])
	set := map[string]struct{}{}
	for _, w := range []string{"a", "b", "a", "c", "b"} {
		set[w] = struct{}{}
	}
	_, hasA := set["a"]
	_, hasD := set["d"]
	println(len(set), hasA, hasD)
	e1 := [0]int{}
	e2 := [0]int{}
	println(e1 == e2, len(e1))
	var zz struct {
		a [0]string
		b struct{}
	}
	println(len(zz.a), zz.b == struct{}{})
	type ZPair struct {
		L, R Z
	}
	var zp ZPair
	println(zp.L == zp.R, any(zp) == any(ZPair{}), any(Z{}) == any(struct{}{}))

	// anonymous struct embedding a named type
	emb := struct {
		Plain
		Extra int
	}{Plain{1, "one"}, 2}
	emb.A += 10
	println(emb.A, emb.B, emb.Extra, emb.Plain.A)

	// nested anonymous structs with arrays of anonymous structs
	cfg := struct {
		Name  string
		Items [3]struct {
			K string
			V float64
		}
		Inner struct{ Flags [4]bool }
	}{Name: "cfg"}
	for i := range cfg.Items {
		cfg.Items[i].K = "k" + strconv.Itoa(i)
		cfg.Items[i].V = float64(i) / 4
	}
	cfg.Inner.Flags[2] = true
	cp := cfg
	cp.Items[0].K = "changed"
	cp.Inner.Flags[2] = false
	println(cfg.Items[0].K, cp.Items[0].K, cfg.Items[2].V, cfg.Inner.Flags[2], cp.Inner.Flags[2], cfg == cp)
	cp.Items[0].K = "k0"
	cp.Inner.Flags[2] = true
	println(cfg == cp)

	// func returning anonymous struct pointer; fields via pointer
	ap := &struct{ N [2]int }{}
	ap.N[1] = 4
	q := ap
	q.N[0] = 3
	println(ap.N[0], ap.N[1])
	// struct with interface field compared after assignment of arrays
	type holder struct{ V any }
	h1 := holder{[2]Z{}}
	h2 := holder{[2]Z{}}
	h3 := holder{[2]struct{}{}}
	println(h1 == h2, h1 == h3)
}
