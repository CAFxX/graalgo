// Generates gc-computed expected vectors for GoFloatFmtTest.
// Every expected string below comes from strconv (gc 1.27.1), NOT from
// the implementation under test. Values are identified by IEEE bit
// pattern so the Java test can reconstruct them exactly.
package main

import (
	"fmt"
	"math"
	"strconv"
)

func f64(bits uint64, verb byte, prec int) {
	v := math.Float64frombits(bits)
	fmt.Printf("F64 0x%016X %c %d %q\n", bits, verb, prec, strconv.FormatFloat(v, verb, prec, 64))
}

func f32(bits uint32, verb byte, prec int) {
	v := float64(math.Float32frombits(bits))
	fmt.Printf("F32 0x%08X %c %d %q\n", bits, verb, prec, strconv.FormatFloat(v, verb, prec, 32))
}

func c128(reBits, imBits uint64, verb byte, prec int) {
	v := complex(math.Float64frombits(reBits), math.Float64frombits(imBits))
	fmt.Printf("C128 0x%016X 0x%016X %c %d %q\n", reBits, imBits, verb, prec, strconv.FormatComplex(v, verb, prec, 128))
}

func c64(reBits, imBits uint32, verb byte, prec int) {
	v := complex(float64(math.Float32frombits(reBits)), float64(math.Float32frombits(imBits)))
	fmt.Printf("C64 0x%08X 0x%08X %c %d %q\n", reBits, imBits, verb, prec, strconv.FormatComplex(v, verb, prec, 64))
}

func main() {
	// --- float64 shortest %g (prec -1) ---
	for _, bits := range []uint64{
		0x0000000000000000, 0x8000000000000000, // +0, -0
		0x3FF0000000000000, 0xBFF0000000000000, // 1, -1
		0x3FB999999999999A, // 0.1
		0x3FB9999999999999, // 0.09999999999999999 (nearest below 0.1)
		0x3FB999999999999B, // 0.10000000000000002 (nearest above 0.1)
		0x444B1AE4D6E2EF50, // 1e21
		0x4415AF1D78B58C40, // 1e20
		0x40FE240000000000, // 123456.0
		0x4132D68700000000, // 1234567.0
		0x3F1A36E2EB1C432D, // 1e-4
		0x3EE4F8B588E368F1, // 1e-5
		0x419D6F34547E6B75, // 123456789.123456789-ish
		0x7FEFFFFFFFFFFFFF, // max float64
		0x0010000000000000, // min normal
		0x000FFFFFFFFFFFFF, // largest subnormal
		0x0000000000000001, // 5e-324 (min subnormal)
		0x0000000000000002, // 9.881312916824931e-324
		0x0000000000000003,
		0x0000000000000004,
		0x0000000000000005,
		0x000000000000000A,
		0x000000000000000F,
		0x0000000000000010,
		0x0000000000000100,
		0x0000000000000001 | 0x0008000000000000, // 1e-323-ish region
		0x3FC3333333333333, // 0.15
		0x3FD3333333333333, // 0.3
		0x3FD5555555555555, // 1/3
		0x3FE5555555555555, // 2/3
		0x400921FB54442D18, // pi
		0x430C6BF526340004, // 1.0000000000000005e15
		0x3FF8000000000000, // 1.5
		0xC004000000000000, // -2.5
		0x7FF8000000000000, // NaN
		0x7FF0000000000000, // +Inf
		0xFFF0000000000000, // -Inf
		0x0010000000000001, // min normal + 1 ulp
		0x0000000000000008, // 8 * min subnormal
		0x3EE4F8B588E368F0, // just below 1e-5
		0x3EE4F8B588E368F2, // just above 1e-5
		0x412E847F00000000, // 999999.5
		0x4415AF1D78B58C3F, // just below 1e20
		0x4415AF1D78B58C41, // just above 1e20
	} {
		f64(bits, 'g', -1)
	}
	// --- float64 %g boundaries / fixed precision ---
	for _, tc := range [][2]interface{}{
		{uint64(0x4132D68700000000), 6}, // 1234567 %g prec 6 -> e form
		{uint64(0x40FE240000000000), 6}, // 123456 %g prec 6 -> f form
		{uint64(0x40F86A0000000000), 6}, // 100000 %g prec 6 -> f form
		{uint64(0x4132D68700000000), 7}, // 1234567 %g prec 7 -> f form
		{uint64(0x4132D68700000000), 5}, // 1234567 %g prec 5
		{uint64(0x4132D68700000000), 1}, // 1234567 %g prec 1
		{uint64(0x4132D68700000000), 0}, // 1234567 %g prec 0 -> 1 sig digit
		{uint64(0x3F1A36E2EB1C432D), 6}, // 1e-4 %g prec 6 -> f form
		{uint64(0x3EE4F8B588E368F1), 6}, // 1e-5 %g prec 6 -> e form
		{uint64(0x3F1A36E2EB1C432D), 1}, // 1e-4 %g prec 1
		{uint64(0x3FB999999999999A), 0}, // 0.1 %g prec 0
		{uint64(0x3FB999999999999A), 1}, // 0.1 %g prec 1
		{uint64(0x3FB999999999999A), 2}, // 0.1 %g prec 2
		{uint64(0x3FB999999999999A), 17}, // 0.1 %g prec 17
		{uint64(0x3FB999999999999A), 21}, // 0.1 %g prec 21
		{uint64(0x40F86A0000000000), 5}, // 100000 %g prec 5 -> e form (exp 5 >= 5)
		{uint64(0x40F86A0000000000), 4}, // 100000 %g prec 4
		{uint64(0x412E847F00000000), 6}, // 999999.5 %g prec 6
		{uint64(0x412E847F00000000), 7},
		{uint64(0x3FF8000000000000), 1}, // 1.5 %g prec 1 -> "2"? (rounds to 1 sig digit)
		{uint64(0x3FF8000000000000), 0}, // 1.5 %g prec 0
		{uint64(0x4004000000000000), 1}, // 2.5 %g prec 1
		{uint64(0x4008000000000000), 1}, // 3.5 %g prec 1
		{uint64(0x3FF0000000000000), 17}, // 1 %g prec 17
		{uint64(0x3FF0000000000000), 21},
	} {
		bits := tc[0].(uint64)
		prec := tc[1].(int)
		f64(bits, 'g', prec)
		f64(bits, 'G', prec)
	}
	// --- float64 %e / %f fixed precision (incl. half-to-even, carries, zero) ---
	for _, tc := range [][2]interface{}{
		{uint64(0x4008000000000000), 0}, // 3.5 %f prec 0 -> "4" (half-even up)
		{uint64(0x4004000000000000), 0}, // 2.5 %f prec 0 -> "2" (half-even down)
		{uint64(0x3FC0000000000000), 2}, // 0.125 %f prec 2 -> "0.12"
		{uint64(0x3FD8000000000000), 2}, // 0.375 %f prec 2 -> "0.38"
		{uint64(0x3FE4000000000000), 2}, // 0.625 %f prec 2 -> "0.62"
		{uint64(0x3FE8000000000000), 2}, // 0.75 %f prec 2 -> "0.75" exact
		{uint64(0x3FEC000000000000), 2}, // 0.875 %f prec 2 -> "0.88"
		{uint64(0x3FC0000000000000), 0}, // 0.125 %f prec 0 -> "0"
		{uint64(0x3FD8000000000000), 0}, // 0.375 %f prec 0 -> "0"
		{uint64(0x3FE4000000000000), 0}, // 0.625 %f prec 0 -> "1"
		{uint64(0x3FE8000000000000), 0}, // 0.75 %f prec 0 -> "1"
		{uint64(0x0000000000000001), 2}, // 5e-324 %f prec 2 -> "0.00"
		{uint64(0x0000000000000001), 0}, // 5e-324 %f prec 0 -> "0"
		{uint64(0x0000000000000001), 6}, // 5e-324 %e prec 6
		{uint64(0x0000000000000002), 6}, // 9.88e-324 %e prec 6
		{uint64(0x0000000000000002), 2}, // 9.88e-324 %e prec 2
		{uint64(0x0000000000000002), 0}, // 9.88e-324 %e prec 0
		{uint64(0x000FFFFFFFFFFFFF), 6}, // largest subnormal %e prec 6
		{uint64(0x0010000000000000), 6}, // min normal %e prec 6
		{uint64(0x0000000000000001), 324}, // 5e-324 %f prec 324 -> full expansion tail
		{uint64(0x0000000000000001), 323},
		{uint64(0x4132D68700000000), 0}, // 1234567 %e prec 0
		{uint64(0x4132D68700000000), 1}, // %e prec 1
		{uint64(0x4132D68700000000), 6}, // %e prec 6
		{uint64(0x4132D68700000000), 2}, // %f prec 2
		{uint64(0x4132D68700000000), 6}, // %f prec 6
		{uint64(0x3F1A36E2EB1C432D), 6}, // 1e-4 %e prec 6
		{uint64(0x3F1A36E2EB1C432D), 6}, // 1e-4 %f prec 6 (dup fmt below)
		{uint64(0x01A56E1FC2F8F359), 2}, // 1e-300 %f prec 2 -> "0.00"
		{uint64(0x01A56E1FC2F8F359), 6}, // 1e-300 %e prec 6
		{uint64(0x412E847F00000000), 0}, // 999999.5 %f prec 0 -> "1000000"
		{uint64(0x412E847F00000000), 0}, // %e prec 0 -> "1e+06"
		{uint64(0x430C6BF526340004), 6}, // 1.0000000000000005e15 %f prec 6
		{uint64(0x7FEFFFFFFFFFFFFF), 6}, // max %e prec 6
		{uint64(0x7FEFFFFFFFFFFFFF), 2}, // max %f prec 2
		{uint64(0xBFF0000000000000), 2}, // -1 %f prec 2
		{uint64(0x8000000000000000), 2}, // -0 %f prec 2
		{uint64(0x8000000000000000), 0}, // -0 %e prec 0
		{uint64(0x3FDCCCCCCCCCCCCD), 2}, // 0.45 %f prec 2
		{uint64(0x3FD6666666666666), 2}, // 0.35 %f prec 2
		{uint64(0x3FD999999999999A), 2}, // 0.4 %f prec 2
		{uint64(0x3FE6666666666666), 2}, // 0.7 %f prec 2
		{uint64(0x3FF199999999999A), 2}, // 1.1 %f prec 2
		{uint64(0x3FF3333333333333), 2}, // 1.2 %f prec 2
		{uint64(0x3FF8000000000000), 2}, // 1.5 %f prec 2
		{uint64(0x3FF999999999999A), 2}, // 1.6 %f prec 2
	} {
		bits := tc[0].(uint64)
		prec := tc[1].(int)
		f64(bits, 'e', prec)
		f64(bits, 'f', prec)
		f64(bits, 'E', prec)
	}
	// --- float64 %e/%f of zero / specials at fixed precision ---
	for _, bits := range []uint64{0x0000000000000000, 0x8000000000000000, 0x7FF8000000000000, 0x7FF0000000000000, 0xFFF0000000000000} {
		f64(bits, 'e', 2)
		f64(bits, 'f', 2)
		f64(bits, 'E', 2)
		f64(bits, 'g', 2)
		f64(bits, 'G', 2)
	}
	// --- float32 shortest + fixed ---
	for _, bits := range []uint32{
		0x00000000, 0x80000000, // +0, -0
		0x3F800000, 0xBF800000, // 1, -1
		0x3DCCCCCD, // 0.1f
		0x3DCCCCCC, // nearest below 0.1f
		0x3DCCCCCE, // nearest above 0.1f
		0x7F7FFFFF, // max float32
		0x00800000, // min normal
		0x007FFFFF, // largest subnormal
		0x00000001, // 1.4e-45 (min subnormal)
		0x00000002,
		0x00000003,
		0x00000004,
		0x00000005,
		0x0000000A,
		0x00000010,
		0x00000100,
		0x3FC00000, // 1.5f
		0x4B000000, // 16777216.0f (2^24)
		0x4B000001, // 16777218.0f (2^24+2; 16777217 rounds to 2^24)
		0x7F800000, // +Inf
		0xFF800000, // -Inf
		0x7FC00000, // NaN
		0x33D6BF95, // 1e-7f
		0x322BCC77, // 1e-8-ish
	} {
		f32(bits, 'g', -1)
		f32(bits, 'e', 2)
		f32(bits, 'f', 2)
		f32(bits, 'g', 6)
		f32(bits, 'G', -1)
	}
	// float32 half-to-even fixed precision
	for _, tc := range [][2]interface{}{
		{uint32(0x3E800000), 0}, // 0.25f %f prec 0 -> "0"
		{uint32(0x3EC00000), 0}, // 0.375f %f prec 0 -> "0"
		{uint32(0x3F000000), 0}, // 0.5f %f prec 0 -> "0" (half-even)
		{uint32(0x3F400000), 0}, // 0.75f %f prec 0 -> "1"
		{uint32(0x40000000), 0}, // 2.0f %f prec 0 -> "2"
		{uint32(0x40400000), 0}, // 3.0f %f prec 0 -> "3"
		{uint32(0x40400000), 1}, // 3.0f %e prec 1
		{uint32(0x00000001), 2}, // 1.4e-45 %f prec 2 -> "0.00"
		{uint32(0x00000001), 6}, // 1.4e-45 %e prec 6
		{uint32(0x00000002), 6}, // 2.8e-45 %e prec 6
		{uint32(0x00000003), 6}, // 4.2e-45 %e prec 6
	} {
		bits := tc[0].(uint32)
		prec := tc[1].(int)
		f32(bits, 'e', prec)
		f32(bits, 'f', prec)
		f32(bits, 'g', prec)
	}
	// --- hex float %x / %X ---
	for _, bits := range []uint64{
		0x3FF8000000000000, // 1.5
		0xBFF8000000000000, // -1.5
		0x3FB999999999999A, // 0.1
		0x0000000000000000, // 0
		0x8000000000000000, // -0
		0x4000000000000000, // 2.0
		0x3FE0000000000000, // 0.5
		0x3FD5555555555555, // 1/3
		0x0000000000000001, // 5e-324
		0x0010000000000000, // min normal
		0x000FFFFFFFFFFFFF, // largest subnormal
		0x7FEFFFFFFFFFFFFF, // max
		0x7FF8000000000000, // NaN
		0x7FF0000000000000, // +Inf
		0xFFF0000000000000, // -Inf
		0x4415AF1D78B58C40, // 1e20
		0x3EE4F8B588E368F1, // 1e-5
	} {
		for _, prec := range []int{-1, 0, 1, 2, 6, 13, 14, 15, 16, 20} {
			f64(bits, 'x', prec)
		}
		f64(bits, 'X', -1)
		f64(bits, 'X', 2)
		f64(bits, 'X', 6)
	}
	// float32 hex
	for _, bits := range []uint32{0x3FC00000, 0x3DCCCCCD, 0x00000001, 0x00800000, 0x7F7FFFFF, 0x00000000, 0x80000000, 0x7F800000, 0x7FC00000} {
		for _, prec := range []int{-1, 0, 2, 6, 13, 15} {
			f32(bits, 'x', prec)
		}
		f32(bits, 'X', -1)
	}
	// --- binary %b ---
	for _, bits := range []uint64{
		0x3FF8000000000000, // 1.5 -> "3p-01"
		0xBFF8000000000000, // -1.5
		0x3FB999999999999A, // 0.1
		0x3FF0000000000000, // 1.0
		0x3FE0000000000000, // 0.5
		0x4000000000000000, // 2.0
		0x0000000000000001, // 5e-324
		0x0000000000000000, // 0
		0x8000000000000000, // -0
		0x7FF8000000000000, // NaN
		0x7FF0000000000000, // +Inf
		0xFFF0000000000000, // -Inf
	} {
		f64(bits, 'b', -1)
	}
	for _, bits := range []uint32{0x3FC00000, 0x3DCCCCCD, 0x00000001, 0x00000000, 0x7F800000, 0x7FC00000} {
		f32(bits, 'b', -1)
	}
	// --- complex ---
	c128(0x3FF8000000000000, 0xC004000000000000, 'g', -1) // (1.5-2.5i)
	c128(0x0000000000000000, 0x0000000000000000, 'g', -1) // (0+0i)
	c128(0x8000000000000000, 0x0000000000000000, 'g', -1) // (-0+0i)
	c128(0x0000000000000000, 0x8000000000000000, 'g', -1) // (0-0i)
	c128(0x3FF0000000000000, 0x0000000000000000, 'g', -1) // (1+0i)
	c128(0x0000000000000000, 0x3FF0000000000000, 'g', -1) // (0+1i)
	c128(0x7FF8000000000000, 0x3FF0000000000000, 'g', -1) // (NaN+1i)
	c128(0x7FF0000000000000, 0xFFF0000000000000, 'g', -1) // (+Inf-Infi)
	c128(0x3EE4F8B588E368F1, 0x4132D68700000000, 'g', -1) // (1e-5+1234567i)
	c128(0x3FF8000000000000, 0xC004000000000000, 'g', 6)
	c128(0x3FF8000000000000, 0xC004000000000000, 'e', 2)
	c128(0x3FF8000000000000, 0xC004000000000000, 'f', 2)
	c128(0x0000000000000001, 0x0000000000000001, 'g', -1) // subnormal complex
	c128(0x0000000000000001, 0x0000000000000001, 'e', 2)
	c64(0x3FC00000, 0xC0200000, 'g', -1) // complex64 (1.5-2.5i)
	c64(0x00000000, 0x00000000, 'g', -1)
	c64(0x00000001, 0x00000001, 'g', -1) // subnormal complex64
	c64(0x3FC00000, 0xC0200000, 'e', 2)
	c64(0x3FC00000, 0xC0200000, 'f', 2)
	c64(0x7FC00000, 0x3F800000, 'g', -1) // (NaN+1i) complex64
	c64(0x7F800000, 0xFF800000, 'g', -1) // (+Inf-Infi) complex64
	// --- want<=0 regression (S-04 red find): %f where dp+prec <= 0 ---
	// The port's early return assumed "rounds to zero"; gc rounds UP when
	// the value is >= half a unit (exact ties go to even, i.e. zero).
	for _, tc := range [][2]interface{}{
		{uint64(0x3FE0000000000000), 0}, // 0.5 %f 0 -> "0" (exact tie -> even)
		{uint64(0x3FE0000000000001), 0}, // just above 0.5 -> "1"
		{uint64(0x3FDFFFFFFFFFFFFF), 0}, // just below 0.5 -> "0"
		{uint64(0x3FECCCCCCCCCCCCD), 0}, // 0.9 %f 0 -> "1"
		{uint64(0x3FEF5C28F5C28F5C), 0}, // 0.99 %f 0 -> "1"
		{uint64(0x3FC0000000000000), 0}, // 0.125 %f 0 -> "0"
		{uint64(0x3FD8000000000000), 0}, // 0.375 %f 0 -> "0"
		{uint64(0x3FE4000000000000), 0}, // 0.625 %f 0 -> "1" (RED on unpatched)
		{uint64(0x3FE8000000000000), 0}, // 0.75 %f 0 -> "1"
		{uint64(0x3F747AE147AE147B), 1}, // 0.005 %f 1 -> "0.0" (0.005 is just above the 0.05 tie; see gc)
		{uint64(0x3F747AE147AE147C), 1}, // next float64 above 0.005, %f 1
		{uint64(0x3FA999999999999A), 1}, // 0.05 %f 1 -> "0.1" (exact tie -> even? 0.05 is 0.05000000000000000277 -> "0.1")
		{uint64(0x3F9999999999999A), 1}, // 0.025 %f 1 -> "0.0"
		{uint64(0x3FB999999999999A), 1}, // 0.1 %f 1 -> "0.1"
		{uint64(0x3FD0000000000000), 1}, // 0.25 %f 1 -> "0.2" (exact tie -> even)
		{uint64(0x3FE8000000000000), 1}, // 0.75 %f 1 -> "0.8" (exact tie -> even)
		{uint64(0x3FC3333333333333), 1}, // 0.15 %f 1 (0.15 is 0.14999... -> "0.1")
		{uint64(0x3F747AE147AE147B), 2}, // 0.005 %f 2 -> "0.01" (just above tie)
		{uint64(0x3F70624DD2F1A9FC), 2}, // 0.004 %f 2 -> "0.00"
		{uint64(0x3F789374BC6A7EFA), 2}, // 0.006 %f 2 -> "0.01"
		{uint64(0x3F43A92A30553261), 2}, // 0.0006 %f 2 -> "0.00"
		{uint64(0x3F0F75104D551D69), 2}, // 0.00006 %f 2 -> "0.00"
		{uint64(0x3F70000000000000), 2}, // 0.00390625 %f 2 -> "0.00"
		{uint64(0x3F747AE147AE147B), 3}, // 0.005 %f 3 -> "0.005"
	} {
		bits := tc[0].(uint64)
		prec := tc[1].(int)
		f64(bits, 'f', prec)
	}

}

