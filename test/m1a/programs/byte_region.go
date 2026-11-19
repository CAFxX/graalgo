// Byte-sized leaves (uint8, int8, bool) live in the byte region of a flat
// object (SPEC §3.4, GG-0072; ADR-0015 decision 3): fields and array elements
// alike, with one slice shape over made arrays and inlined [N]byte fields.
package main

type inner struct {
	ok   bool
	n    int8
	tag  [3]byte
	wide int32
}

type rec struct {
	a    uint8
	x    int64
	in   inner
	name string
	b    bool
	arr  [2]inner
	buf  [8]byte
	neg  int8
}

type key struct {
	hi, lo byte
	on     bool
}

var global rec
var gbytes [4]byte
var gflag bool

func fill(p *rec, seed int) {
	p.a = uint8(200 + seed)
	p.x = int64(seed) * 1000
	p.in = inner{true, int8(-100 - seed), [3]byte{byte(seed), 0xFF, 0x80}, int32(-seed)}
	p.name = "rec"
	p.b = seed%2 == 0
	p.arr[1] = inner{seed%3 == 0, int8(seed), [3]byte{1, 2, byte(250 + seed)}, 7}
	for i := range p.buf {
		p.buf[i] = byte(i*37 + seed)
	}
	p.neg = int8(-seed)
}

func show(p *rec) {
	println(p.a, p.x, p.in.ok, p.in.n, p.in.tag[0], p.in.tag[1], p.in.tag[2], p.in.wide, p.name, p.b)
	println(p.arr[0].ok, p.arr[0].n, p.arr[1].ok, p.arr[1].n, p.arr[1].tag[2], p.arr[1].wide, p.neg)
	for _, c := range p.buf {
		print(c, " ")
	}
	println()
}

func sum(b []byte) int {
	t := 0
	for i, c := range b {
		t += int(c) * (i + 1)
	}
	return t
}

func main() {
	// Struct with interleaved byte and wide leaves; value copies are independent.
	var r rec
	fill(&r, 3)
	show(&r)
	c := r
	c.in.tag[1] = 9
	c.buf[0] = 1
	c.b = !c.b
	c.neg = 127
	show(&r)
	show(&c)
	println(r == c, r.in == c.in, r.arr == c.arr, r.buf == c.buf)

	// Globals, escaping scalars.
	fill(&global, 5)
	show(&global)
	gbytes[2] = 0xAB
	gflag = true
	println(gbytes[0], gbytes[2], gflag)
	bv := new(bool)
	iv := new(int8)
	uv := new(uint8)
	*bv, *iv, *uv = true, -128, 255
	*iv--
	*uv++
	println(*bv, *iv, *uv)
	esc := byte(250)
	inc := func() { esc += 10 }
	inc()
	println(esc)

	// One slice shape: over a made array and over an inlined [N]byte field (aliasing).
	made := make([]byte, 4, 16)
	over := r.buf[2:6]
	println(len(over), cap(over), sum(made), sum(over))
	over[0] = 0xEE
	println(r.buf[2], over[0])
	n := copy(made, over)
	println(n, made[0], made[3])
	made = append(made, over...)
	made = append(made, 0x7F, 0x80, 0xFF)
	made = append(made, "héllo"...)
	println(len(made), sum(made))
	over = append(over, 1, 2) // cap 6: writes through into r.buf
	println(r.buf[6], r.buf[7], len(over), cap(over))
	over = append(over, 3) // grows: no longer aliases
	over[0] = 0
	println(r.buf[2], over[0], len(over))
	clear(made[1:3])
	println(made[0], made[1], made[2], made[3])
	tag := r.in.tag[:]
	tag[2] = 0x11
	println(r.in.tag[2], len(tag), cap(tag))
	inl := r.arr[1].tag[1:]
	inl[1]++
	println(r.arr[1].tag[2])

	// Slices of other byte-sized kinds and of structs with byte leaves.
	flags := make([]bool, 3)
	flags[1] = true
	flags = append(flags, true, false)
	small := []int8{-1, -128, 127}
	small = append(small, small...)
	println(flags[0], flags[1], flags[3], flags[4], len(flags), small[1], small[4], small[5])
	recs := make([]inner, 2)
	recs[1].tag[0] = 0xC8
	recs[1].n = -5
	recs = append(recs, inner{true, -7, [3]byte{9, 8, 7}, 123})
	recs2 := make([]inner, 3)
	copy(recs2, recs)
	recs[2].tag[1] = 0
	println(recs2[1].tag[0], recs2[1].n, recs2[2].ok, recs2[2].tag[1], recs2[2].wide, recs[2].tag[1])
	grid := [3][3]bool{}
	grid[1][2] = true
	row := grid[1][:]
	row[0] = true
	println(grid[1][0], grid[1][1], grid[1][2], grid[2][2])

	// string <-> []byte, substrings sharing storage, range over a substring.
	s := "héllo, wörld"
	b := []byte(s)
	b[0] = 'H'
	t := string(b)
	sub := t[1:9]
	println(s, t, sub, len(sub), sub[0], sub[1], sub == "éllo, w", sub < t, t[7:] == "wörld")
	for i, ch := range sub[2:] {
		print(i, ":", ch, " ")
	}
	println()
	b[1] = 'e' // the strings made from b do not change
	println(t, sub, string(b[:6]), string(b[3:3]) == "")
	dst := make([]byte, 5)
	n1 := copy(dst, sub[1:])
	s1 := string(dst)
	n2 := copy(dst[:2], "xyz")
	println(n1, s1, n2, string(dst))
	cat := sub + t[:2] + string(rune(0x263A))
	println(cat, len(cat), []rune(sub)[1], string([]rune{72, 0xE9}))

	// Maps: byte-sized values, struct values and keys with byte leaves.
	mb := map[int]byte{}
	mb[1] = 200
	mb[1] += 100
	mb[2]--
	mf := map[string]bool{"a": true}
	mf["b"] = !mf["zz"]
	ms := map[int]inner{}
	ms[7] = inner{true, -3, [3]byte{1, 2, 3}, 4}
	v := ms[7]
	v.tag[0] = 99
	mk := map[key]int8{{1, 2, true}: -9}
	mk[key{1, 2, true}]--
	mk[key{1, 2, false}] = 5
	arrk := map[[2]byte]bool{{1, 2}: true}
	println(mb[1], mb[2], mb[3], mf["a"], mf["b"], mf["c"], ms[7].tag[0], v.tag[0], ms[8].ok)
	println(mk[key{1, 2, true}], mk[key{1, 2, false}], len(mk), arrk[[2]byte{1, 2}], arrk[[2]byte{2, 1}])
	delete(mb, 1)
	println(len(mb), mb[1])

	// Pointers to byte leaves: interior pointers, equality, through slices.
	p0 := &r.buf[0]
	p3 := &r.buf[3]
	q3 := &r.buf[3:][0]
	pa := &r.a
	pok := &r.arr[1].ok
	*p3 = 77
	*pa += 100
	*pok = !*pok
	println(*p0, r.buf[3], p3 == q3, p0 == p3, *pa, r.a, r.arr[1].ok)
	pin := &r.in
	pin.n = 55
	ptag := &pin.tag
	ptag[0] = 0x42
	println(r.in.n, r.in.tag[0], len(ptag), &pin.tag[0] == &r.in.tag[0])

	// Interfaces and channels carry byte-sized values and structs with byte leaves.
	var e any = r.in
	r.in.tag[0] = 0
	println(e.(inner).tag[0], e == any(r.in), any(byte(7)) == any(byte(7)), any(int8(7)) == any(byte(7)))
	ch := make(chan inner, 1)
	ch <- r.arr[1]
	got := <-ch
	println(got.tag[2], got.n, got.ok)
}
