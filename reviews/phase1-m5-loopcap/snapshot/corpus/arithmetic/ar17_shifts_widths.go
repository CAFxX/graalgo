package main

func main() {
	var i8 int8 = 1
	a := i8 << 7
	b := i8 << 8
	var u16 uint16 = 0x8000
	c := u16 >> 15
	d := u16 >> 16
	e := u16 << 1
	var i32 int32 = -1
	f := i32 >> 32
	g := i32 << 32
	h := i32 >> 31
	var u32 uint32 = 0x80000000
	k := u32 >> 31
	m := u32 >> 32
	var i16 int16 = 0x4000
	n := i16 << 2
	o := i16 >> 1
	a = a
	b = b
	c = c
	d = d
	e = e
	f = f
	g = g
	h = h
	k = k
	m = m
	n = n
	o = o
}

// observe: a b c d e f g h k m n o
