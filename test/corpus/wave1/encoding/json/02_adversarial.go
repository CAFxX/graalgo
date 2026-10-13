package main

import (
	"encoding/json"
	"fmt"
)

func main() {
	// Deep nesting, key sorting, float shortest-repr, string escapes.
	deep := map[string]any{"l1": map[string]any{"l2": map[string]any{"l3": []any{1, 2, 3}}}}
	b, _ := json.Marshal(deep)
	fmt.Printf("deep = %s\n", string(b))

	// Map keys must sort (byte-identical to gc).
	m := map[string]any{"z": 1, "a": 2, "m": 3, "A": 4, "0": 5, "_": 6}
	b, _ = json.Marshal(m)
	fmt.Printf("sorted keys = %s\n", string(b))

	// Float shortest repr edge cases.
	for _, f := range []float64{0.1, 0.3, 1e21, 1e-7, 123456789.123456789, 2.5e-324, 1.7976931348623157e308} {
		b, _ := json.Marshal(f)
		fmt.Printf("float %v -> %s\n", f, string(b))
	}

	// String escapes: all control chars, DEL, unicode.
	s := "\x00\x01\x1f\x7f\u0080\u00e9\U0001F600"
	b, _ = json.Marshal(s)
	fmt.Printf("escapes = %s\n", string(b))

	// MarshalIndent with nested + empty containers.
	b, _ = json.MarshalIndent(map[string]any{"empty_arr": []any{}, "empty_obj": map[string]any{}, "nil": nil}, "", "  ")
	fmt.Printf("indent empties:\n%s\n", string(b))

	// Number literals preserved exactly.
	for _, ns := range []string{"1E10", "1e+10", "0.30000000000000004", "123456789012345678901234567890"} {
		b, _ := json.Marshal(json.Number(ns))
		fmt.Printf("Number(%s) -> %s\n", ns, string(b))
	}
}
