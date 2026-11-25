package main

func main() {
	var i8 int8 = -1
	a := uint8(i8)
	var u64 uint64 = 0xFFFFFFFFFFFFFFFF
	b := int64(u64)
	var i32 int32 = -70000
	c := uint16(i32)
	d := int16(i32)
	e := int(uint8(200))
	f := int(int8(-128))
	var neg1 int8 = -1
	g := uint(neg1)
	h := int32(uint16(65535))
	var u255 uint8 = 255
	k := int8(u255)
	var neg5 int64 = -5
	m := uint64(neg5)
	var neg1i int = -1
	n := uintptr(neg1i)
	o := int(uintptr(42))
	var neg1_16 int16 = -1
	p := uint16(neg1_16)
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
	p = p
}

// observe: a b c d e f g h k m n o p
