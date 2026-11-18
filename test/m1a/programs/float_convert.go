package main

// Float to narrow unsigned integer conversions of out-of-range values follow
// gc/amd64: uint8 and uint16 go through the 32-bit conversion (CVTTSD2SL,
// 0x80000000 when out of int32 range) before truncation; uint32 through the
// 64-bit one.

//go:noinline
func f(x float64) float64 { return x }

//go:noinline
func g(x float32) float32 { return x }

func main() {
	for _, v := range []float64{3000000001.0, -3e9, 5e9 + 7, 70000.5, -1.5, 255.9, 65536, 1e19, -1e19} {
		x := f(v)
		println(uint8(x), uint16(x), uint32(x), int8(x), int16(x), int32(x))
	}
	for _, v := range []float32{3000000001.0, -3e9, 70000.5} {
		x := g(v)
		println(uint8(x), uint16(x), uint32(x), int8(x), int16(x))
	}
}
