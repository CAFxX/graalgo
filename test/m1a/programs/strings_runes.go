package main

func main() {
	s := "héllo, 世界\xff!"
	n := 0
	for i, r := range s {
		n += i + int(r)
	}
	bs := []byte(s)
	rs := []rune(s)
	bs[0] = 'H'
	rs[1] = 'E'
	println(len(s), len(bs), len(rs), n, string(bs[:6]), string(rs[:5]), s[1], s[:1]+s[3:6])
	println(string(rune(0x4e16)), string(rune(-1)) == "�", s < "i", s >= "h", "a"+"b" == "ab")
	var b []byte
	b = append(b, "abc"...)
	b = append(b, 'd')
	k := copy(b[1:], "XY")
	println(string(b), k, len(b), min("b", "a", "c"), max(1.5, 2), min(3, 1, 2))
	u8 := uint8(200)
	i8 := int8(100)
	u8 += 100
	i8 += 100
	var u16 uint16 = 1 << 15
	u16 <<= 1
	var i32 int32 = -1 << 31
	println(u8, i8, u16, -i32, i32/-1, uint32(i32)>>31, 7>>1, -7>>1, -7/2, -7%2, int64(i32)<<32*2)
	x := 1
	sh := 70
	println(x<<sh, x<<uint(sh-10) != 0, ^x, x&^3, 0xff^0x0f)
	c := complex(1, 2)
	c *= c
	println(real(c), imag(c), c == complex(-3, 4))
	f := 0.1
	g := float32(f)
	println(f+0.2, float64(g), g*3, int(f*29), int(-f*29), uint8(int(f*3007)), f == 0.1)
lbl:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			switch {
			case j == 1:
				continue lbl
			case i == 2:
				break lbl
			}
			print(i, j, " ")
		}
	}
	println()
	fs := make([]func() int, 0)
	for i := 0; i < 3; i++ {
		fs = append(fs, func() int { i *= 2; return i })
	}
	println(fs[0](), fs[1](), fs[2](), fs[2]())
	m := map[string][]int{}
	m["a"] = append(m["a"], 1, 2)
	m["b"] = nil
	delete(m, "zz")
	total := 0
	for k, v := range m {
		total += len(k) + len(v)
		delete(m, k)
	}
	clear(m)
	println(total, len(m), m["a"] == nil)
}
