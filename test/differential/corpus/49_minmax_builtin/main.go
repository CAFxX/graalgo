package main

// min/max builtins (Go 1.21+): signed-zero and NaN edge cases plus the
// surrounding ordered-type surface.
//
// P1-1 (2026-10-16): the live interop MinMaxNode folded floats with raw
// </>, so min(+0.0,-0.0) kept +0.0 and max(-0.0,+0.0) kept -0.0, and NaN
// collapsed to a canonical NaN. gc: min(+0,-0) = -0, max(-0,+0) = +0, and
// the first NaN argument's value propagates. The fold now uses the
// Float.compare/Double.compare total order with first-NaN propagation.
//
// -0.0 is produced by unary negation of a variable: the -0.0 literal is
// +0.0 in gc (go/types folds it); see bi22_float_edge.
//
// NOTE: uses builtin println (stderr), not fmt: at this commit the
// frontend cannot export fmt-importing programs (pre-existing
// badlinkname_rtype_FieldByName WS D failure, also on master).
func main() {
	pz := 0.0
	nz := -pz
	println("f64 min(+0,-0):", min(pz, nz), "max(-0,+0):", max(nz, pz))

	pz32 := float32(0)
	nz32 := -pz32
	println("f32 min(+0,-0):", min(pz32, nz32), "max(-0,+0):", max(nz32, pz32))

	// NaN propagates (and never equals itself, IEEE).
	nan := pz / pz
	r1 := min(1.0, nan, 2.0)
	r2 := max(nan, 2.0)
	println("nan min:", r1, "max:", r2, "selfNe:", r1 != r1 && r2 != r2)

	// Multi-arg ties keep working; zeros of mixed sign in longer folds.
	println("fold min:", min(3.0, nz, pz, 1.0), "max:", max(3.0, nz, pz, 1.0))

	// Integers: signed, unsigned, single argument.
	println("int min:", min(3, -7, 2), "max:", max(3, -7, 2))
	println("uint max:", max(uint(3), uint(9), uint(1)), "one:", min(42))

	// Strings: byte-wise lexicographic.
	println("str min:", min("b", "a", "c"), "max:", max("b", "a", "c"))
}
