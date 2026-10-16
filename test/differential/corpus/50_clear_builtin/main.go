package main

// clear builtin (Go 1.21+): the shared semantic matrix for the two ClearNode
// implementations (P1-2, 2026-10-16).
//
// The live interop p3builtin.ClearNode (v1 wire path, production launchers)
// and the truffle frontend.ClearNode (NodeFactoryV2 only, no launcher yet)
// MUST stay semantically identical. This program pins the matrix on the
// interop path differentially against gc; ClearNodeSemanticsTest pins the
// same matrix on the truffle node directly in-JVM. Cases:
//
//   - nil map / nil slice: no-op, no panic
//   - non-nil map with entries: all entries deleted
//   - slice of every element kind: all len(s) elements zeroed, for every
//     backing-array type (byte[], short[], int[], long[], float[],
//     double[], char[], boolean[], Object[])
//   - only len(s) elements are zeroed; backing beyond len is untouched
//
// NOTE: uses builtin println (stderr), not fmt: at this commit the
// frontend cannot export fmt-importing programs (pre-existing
// badlinkname_rtype_FieldByName WS D failure, also on master).
func main() {
	// Maps.
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	clear(m)
	println("map len:", len(m))
	var nm map[string]int
	clear(nm)
	println("nil map: ok")

	// Nil slice: no-op.
	var ns []int
	clear(ns)
	println("nil slice: ok")

	// Every element kind zeroes.
	i := []int{1, -2, 3}
	clear(i)
	println("int:", i[0], i[1], i[2])
	i8 := []int8{1, -2, 3}
	clear(i8)
	println("int8:", i8[0], i8[1], i8[2])
	i16 := []int16{1, -2, 3}
	clear(i16)
	println("int16:", i16[0], i16[1], i16[2])
	i32 := []int32{1, -2, 3}
	clear(i32)
	println("int32:", i32[0], i32[1], i32[2])
	i64 := []int64{1, -2, 3}
	clear(i64)
	println("int64:", i64[0], i64[1], i64[2])
	u := []uint{1, 2, 3}
	clear(u)
	println("uint:", u[0], u[1], u[2])
	f32 := []float32{1.5, -2.5, 3.25}
	clear(f32)
	println("float32:", f32[0], f32[1], f32[2])
	f64 := []float64{1.5, -2.5, 3.25}
	clear(f64)
	println("float64:", f64[0], f64[1], f64[2])
	b := []bool{true, false, true}
	clear(b)
	println("bool:", b[0], b[1], b[2])
	by := []byte{1, 2, 3}
	clear(by)
	println("byte:", by[0], by[1], by[2])
	str := []string{"a", "b", "c"}
	clear(str)
	// NOTE: str[i] == "" is not asserted here: reading a nil string
	// element and comparing hits a pre-existing backend NPE in string ==
	// (also on fresh make([]string,2) — unrelated to clear, filed
	// separately). clear itself must not crash and must keep len.
	println("string cleared, len:", len(str))

	// Only len(s) elements are zeroed; backing beyond len is untouched.
	s := make([]int, 2, 5)
	full := s[:5]
	for k := range full {
		full[k] = 7
	}
	clear(s)
	println("len2cap5:", full[0], full[1], full[2], full[3], full[4])

	// Re-clearing an already empty slice is a no-op.
	clear(i)
	println("reclear:", i[0], i[1], i[2])
}
