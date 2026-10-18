// Package ms is the white-box differential driver for the synthetic math
// package re-test (see synth_math_diff_test.go). It is copied into a
// temporary module alongside the remapped syntheticMathSource and asserts
// BIT-EXACTNESS vs the gc standard library on adversarial + random inputs.
//
// This is the mechanical re-test trigger for the synthetic math package's
// accuracy contract (see gen_synth_math.py header): re-run when the Go
// toolchain, the JDK, or the generator changes.
package ms

import (
	"math"
	"math/rand"
	"testing"
)

// adv1 is the adversarial unary input corpus: zeros, infinities, NaN,
// subnormals, normals at magnitude boundaries, and transcendental
// landmarks.
var adv1 = []float64{
	0, math.Copysign(0, -1), 1, -1, 0.5, -0.5, 2, -2,
	math.Pi, -math.Pi, math.E, -math.E,
	math.Pi / 2, math.Pi / 4, 3 * math.Pi / 2, 2 * math.Pi,
	1e-300, -1e-300, 1e-10, -1e-10,
	1e10, -1e10, 1e100, -1e100, 1e300, -1e300,
	math.MaxFloat64, -math.MaxFloat64,
	math.SmallestNonzeroFloat64, -math.SmallestNonzeroFloat64,
	2.2250738585072014e-308, -2.2250738585072014e-308, // min normal
	4.45014771701440276618e-308, // 2*min normal
	0.49999999999999994, 0.5000000000000001,
	0.9999999999999999, 1.0000000000000002,
	math.Inf(1), math.Inf(-1), math.NaN(),
	710.5, -745.2, 709.782712893384, // exp overflow/underflow edges
	1.4426950408889634, 0.6931471805599453, // log2e, ln2
}

func randFloat64(r *rand.Rand) float64 {
	switch r.Intn(8) {
	case 0: // subnormal / near-zero
		return (r.Float64() - 0.5) * 2e-308
	case 1: // tiny normal
		return (r.Float64() - 0.5) * 2e-300
	case 2: // unit range
		return (r.Float64() - 0.5) * 4
	case 3: // moderate
		return (r.Float64() - 0.5) * 2000
	case 4: // large
		return (r.Float64() - 0.5) * 2e100
	case 5: // huge
		return (r.Float64() - 0.5) * 2e300
	case 6: // near integers (exercises floor/ceil/round/trunc)
		return float64(r.Intn(2000000)-1000000) + r.Float64() - 0.5
	default:
		return r.NormFloat64() * 100
	}
}

// bitsEq compares float64s by exact bit pattern (NaN payloads included:
// the synthetic NaN uses gc's uvnan, so payloads must match too).
func bitsEq(a, b float64) bool {
	return math.Float64bits(a) == math.Float64bits(b)
}

func checkUnary(t *testing.T, name string, got func(float64) float64, want func(float64) float64) {
	t.Helper()
	r := rand.New(rand.NewSource(0x5EED))
	bad := 0
	inputs := make([]float64, 0, len(adv1)+2048)
	inputs = append(inputs, adv1...)
	for i := 0; i < 2048; i++ {
		inputs = append(inputs, randFloat64(r))
	}
	for _, x := range inputs {
		g, w := got(x), want(x)
		if !bitsEq(g, w) {
			if bad < 5 {
				t.Errorf("%s(%v): got bits %x, want bits %x (got %v, want %v)",
					name, x, math.Float64bits(g), math.Float64bits(w), g, w)
			}
			bad++
		}
	}
	if bad > 0 {
		t.Errorf("%s: %d/%d mismatches", name, bad, len(inputs))
	}
}

func checkBinary(t *testing.T, name string, got func(float64, float64) float64, want func(float64, float64) float64) {
	t.Helper()
	r := rand.New(rand.NewSource(0x5EED))
	bad := 0
	n := 0
	// Adversarial cross product on a reduced set + random pairs.
	small := adv1[:24]
	for _, x := range small {
		for _, y := range small {
			n++
			g, w := got(x, y), want(x, y)
			if !bitsEq(g, w) {
				if bad < 5 {
					t.Errorf("%s(%v,%v): got bits %x, want bits %x",
						name, x, y, math.Float64bits(g), math.Float64bits(w))
				}
				bad++
			}
		}
	}
	for i := 0; i < 2048; i++ {
		x, y := randFloat64(r), randFloat64(r)
		n++
		g, w := got(x, y), want(x, y)
		if !bitsEq(g, w) {
			if bad < 5 {
				t.Errorf("%s(%v,%v): got bits %x, want bits %x",
					name, x, y, math.Float64bits(g), math.Float64bits(w))
			}
			bad++
		}
	}
	if bad > 0 {
		t.Errorf("%s: %d/%d mismatches", name, bad, n)
	}
}

func TestDifferentialUnary(t *testing.T) {
	cases := []struct {
		name string
		got  func(float64) float64
		want func(float64) float64
	}{
		{"Abs", Abs, math.Abs},
		{"Acos", Acos, math.Acos},
		{"Acosh", Acosh, math.Acosh},
		{"Asin", Asin, math.Asin},
		{"Asinh", Asinh, math.Asinh},
		{"Atan", Atan, math.Atan},
		{"Atanh", Atanh, math.Atanh},
		{"Cbrt", Cbrt, math.Cbrt},
		{"Ceil", Ceil, math.Ceil},
		{"Cos", Cos, math.Cos},
		{"Cosh", Cosh, math.Cosh},
		{"Erf", Erf, math.Erf},
		{"Erfc", Erfc, math.Erfc},
		{"Erfinv", Erfinv, math.Erfinv},
		{"Erfcinv", Erfcinv, math.Erfcinv},
		{"Exp", Exp, math.Exp},
		{"Exp2", Exp2, math.Exp2},
		{"Expm1", Expm1, math.Expm1},
		{"Floor", Floor, math.Floor},
		{"Gamma", Gamma, math.Gamma},
		{"J0", J0, math.J0},
		{"J1", J1, math.J1},
		{"Y0", Y0, math.Y0},
		{"Y1", Y1, math.Y1},
		{"Log", Log, math.Log},
		{"Log10", Log10, math.Log10},
		{"Log1p", Log1p, math.Log1p},
		{"Log2", Log2, math.Log2},
		{"Logb", Logb, math.Logb},
		{"Round", Round, math.Round},
		{"RoundToEven", RoundToEven, math.RoundToEven},
		{"Sin", Sin, math.Sin},
		{"Sinh", Sinh, math.Sinh},
		{"Sqrt", Sqrt, math.Sqrt}, // remapped intrinsic; routes the call graph
		{"Tan", Tan, math.Tan},
		{"Tanh", Tanh, math.Tanh},
		{"Trunc", Trunc, math.Trunc},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkUnary(t, c.name, c.got, c.want)
		})
	}
}

func TestDifferentialBinary(t *testing.T) {
	cases := []struct {
		name string
		got  func(float64, float64) float64
		want func(float64, float64) float64
	}{
		{"Atan2", Atan2, math.Atan2},
		{"Copysign", Copysign, math.Copysign},
		{"Dim", Dim, math.Dim},
		{"Hypot", Hypot, math.Hypot},
		{"Max", Max, math.Max},
		{"Min", Min, math.Min},
		{"Mod", Mod, math.Mod},
		{"Nextafter", Nextafter, math.Nextafter},
		{"Pow", Pow, math.Pow},
		{"Remainder", Remainder, math.Remainder},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkBinary(t, c.name, c.got, c.want)
		})
	}
}

func TestDifferentialFMA(t *testing.T) {
	r := rand.New(rand.NewSource(0x5EED))
	bad := 0
	const n = 2048
	for i := 0; i < n; i++ {
		x, y, z := randFloat64(r), randFloat64(r), randFloat64(r)
		g, w := FMA(x, y, z), math.FMA(x, y, z)
		if !bitsEq(g, w) {
			if bad < 5 {
				t.Errorf("FMA(%v,%v,%v): got bits %x, want bits %x",
					x, y, z, math.Float64bits(g), math.Float64bits(w))
			}
			bad++
		}
	}
	// Adversarial: infinities, NaN, subnormals in each slot.
	for _, x := range []float64{0, 1, math.Inf(1), math.Inf(-1), math.NaN(), math.SmallestNonzeroFloat64} {
		for _, y := range []float64{0, 1, math.Inf(1), math.NaN()} {
			for _, z := range []float64{0, 1, math.Inf(-1), math.NaN()} {
				g, w := FMA(x, y, z), math.FMA(x, y, z)
				if !bitsEq(g, w) {
					t.Errorf("FMA(%v,%v,%v): got %x want %x", x, y, z,
						math.Float64bits(g), math.Float64bits(w))
					bad++
				}
			}
		}
	}
	if bad > 0 {
		t.Errorf("FMA: %d mismatches", bad)
	}
}

func TestDifferentialHelpers(t *testing.T) {
	// __frexp / __modf / __sincos are the real single-result
	// implementations behind the fail-fast Frexp/Modf/Sincos.
	t.Run("__frexp", func(t *testing.T) {
		r := rand.New(rand.NewSource(0x5EED))
		bad := 0
		inputs := append(append([]float64{}, adv1...), math.MaxFloat64)
		for i := 0; i < 2048; i++ {
			inputs = append(inputs, randFloat64(r))
		}
		for _, x := range inputs {
			p := __frexp(x)
			wf, we := math.Frexp(x)
			if !bitsEq(p.frac, wf) || p.exp != we {
				if bad < 5 {
					t.Errorf("__frexp(%v) = (%v,%d), want (%v,%d)",
						x, p.frac, p.exp, wf, we)
				}
				bad++
			}
		}
		if bad > 0 {
			t.Errorf("__frexp: %d mismatches", bad)
		}
	})
	t.Run("__modf", func(t *testing.T) {
		r := rand.New(rand.NewSource(0x5EED))
		bad := 0
		inputs := append([]float64{}, adv1...)
		for i := 0; i < 2048; i++ {
			inputs = append(inputs, randFloat64(r))
		}
		for _, x := range inputs {
			p := __modf(x)
			wi, wf := math.Modf(x)
			if !bitsEq(p.int, wi) || !bitsEq(p.frac, wf) {
				if bad < 5 {
					t.Errorf("__modf(%v) = (%v,%v), want (%v,%v)",
						x, p.int, p.frac, wi, wf)
				}
				bad++
			}
		}
		if bad > 0 {
			t.Errorf("__modf: %d mismatches", bad)
		}
	})
	t.Run("__sincos", func(t *testing.T) {
		r := rand.New(rand.NewSource(0x5EED))
		bad := 0
		inputs := append([]float64{}, adv1...)
		for i := 0; i < 2048; i++ {
			inputs = append(inputs, randFloat64(r))
		}
		for _, x := range inputs {
			p := __sincos(x)
			ws, wc := math.Sincos(x)
			if !bitsEq(p.sin, ws) || !bitsEq(p.cos, wc) {
				if bad < 5 {
					t.Errorf("__sincos(%v) = (%v,%v), want (%v,%v)",
						x, p.sin, p.cos, ws, wc)
				}
				bad++
			}
		}
		if bad > 0 {
			t.Errorf("__sincos: %d mismatches", bad)
		}
	})
}

func TestDifferentialScalar(t *testing.T) {
	// Ilogb, IsNaN, Signbit, IsInf, Ldexp, Pow10, Inf, Jn, Yn, NaN,
	// Nextafter32 and the bit-conversion intrinsics (remapped).
	r := rand.New(rand.NewSource(0x5EED))
	inputs := append([]float64{}, adv1...)
	for i := 0; i < 2048; i++ {
		inputs = append(inputs, randFloat64(r))
	}
	for _, x := range inputs {
		if got, want := Ilogb(x), math.Ilogb(x); got != want {
			t.Errorf("Ilogb(%v) = %d, want %d", x, got, want)
		}
		if got, want := IsNaN(x), math.IsNaN(x); got != want {
			t.Errorf("IsNaN(%v) = %v, want %v", x, got, want)
		}
		if got, want := Signbit(x), math.Signbit(x); got != want {
			t.Errorf("Signbit(%v) = %v, want %v", x, got, want)
		}
	}
	for _, n := range []int{-1000, -33, -1, 0, 1, 33, 1000} {
		if got, want := Inf(n), math.Inf(n); !bitsEq(got, want) {
			t.Errorf("Inf(%d) bits %x, want %x", n,
				math.Float64bits(got), math.Float64bits(want))
		}
		if got, want := Pow10(n), math.Pow10(n); !bitsEq(got, want) {
			t.Errorf("Pow10(%d) bits %x, want %x", n,
				math.Float64bits(got), math.Float64bits(want))
		}
	}
	for _, x := range inputs {
		for _, e := range []int{-1075, -1022, -53, -1, 0, 1, 53, 1023, 1024} {
			if got, want := Ldexp(x, e), math.Ldexp(x, e); !bitsEq(got, want) {
				t.Errorf("Ldexp(%v,%d) bits %x, want %x", x, e,
					math.Float64bits(got), math.Float64bits(want))
			}
		}
		if got, want := IsInf(x, 1), math.IsInf(x, 1); got != want {
			t.Errorf("IsInf(%v,1) = %v, want %v", x, got, want)
		}
		if got, want := IsInf(x, -1), math.IsInf(x, -1); got != want {
			t.Errorf("IsInf(%v,-1) = %v, want %v", x, got, want)
		}
	}
	for _, n := range []int{-3, -1, 0, 1, 2, 5, 10} {
		for _, x := range []float64{0.5, 1.5, 10, 100} {
			if got, want := Jn(n, x), math.Jn(n, x); !bitsEq(got, want) {
				t.Errorf("Jn(%d,%v) bits %x, want %x", n, x,
					math.Float64bits(got), math.Float64bits(want))
			}
			if got, want := Yn(n, x), math.Yn(n, x); !bitsEq(got, want) {
				t.Errorf("Yn(%d,%v) bits %x, want %x", n, x,
					math.Float64bits(got), math.Float64bits(want))
			}
		}
	}
	if got, want := NaN(), math.NaN(); !bitsEq(got, want) {
		t.Errorf("NaN() bits %x, want %x",
			math.Float64bits(got), math.Float64bits(want))
	}
	// Nextafter32 over float32 adversarial + random.
	r32 := rand.New(rand.NewSource(0x5EED))
	f32inputs := []float32{0, 1, -1, 0.5, 1e10, 1e-30,
		math.MaxFloat32, math.SmallestNonzeroFloat32,
		float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.NaN())}
	for i := 0; i < 512; i++ {
		f32inputs = append(f32inputs, float32(randFloat64(r32)))
	}
	for _, x := range f32inputs {
		for _, y := range f32inputs[:12] {
			got, want := Nextafter32(x, y), math.Nextafter32(x, y)
			if math.Float32bits(got) != math.Float32bits(want) {
				t.Errorf("Nextafter32(%v,%v) bits %x, want %x",
					x, y, math.Float32bits(got), math.Float32bits(want))
			}
		}
	}
	// Bit-conversion intrinsics (remapped to stdlib in the test module;
	// this exercises the call graph, not the Java bit ops).
	for _, x := range inputs {
		if got, want := Float64bits(x), math.Float64bits(x); got != want {
			t.Errorf("Float64bits(%v) = %x, want %x", x, got, want)
		}
	}
	for _, b := range []uint64{0, 1, 0x3FF0000000000000, 0x7FF8000000000001, 0xFFF0000000000000} {
		if got, want := Float64frombits(b), math.Float64frombits(b); !bitsEq(got, want) {
			t.Errorf("Float64frombits(%x) bits %x, want %x", b,
				math.Float64bits(got), math.Float64bits(want))
		}
	}
	r2 := rand.New(rand.NewSource(1))
	for i := 0; i < 256; i++ {
		x32 := r2.Float32()
		if got, want := Float32bits(x32), math.Float32bits(x32); got != want {
			t.Errorf("Float32bits(%v) = %x, want %x", x32, got, want)
		}
		b32 := r2.Uint32()
		if got, want := Float32frombits(b32), math.Float32frombits(b32); math.Float32bits(got) != math.Float32bits(want) {
			t.Errorf("Float32frombits(%x) bits %x, want %x", b32,
				math.Float32bits(got), math.Float32bits(want))
		}
	}
}

func TestFailFastPanics(t *testing.T) {
	// The multi-result entry points are fail-fast by design (WS D);
	// pin that they panic rather than silently returning wrong values.
	for name, f := range map[string]func(){
		"Frexp":  func() { Frexp(1.5) },
		"Modf":   func() { Modf(1.5) },
		"Lgamma": func() { Lgamma(1.5) },
		"Sincos": func() { Sincos(1.5) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected panic, got none", name)
				}
			}()
			f()
		}()
	}
}
