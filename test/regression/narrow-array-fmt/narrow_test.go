// Regression test (debt-paydown, 2026-11-18): fmt printing of arrays and
// slices with narrow element kinds (int8/uint8/int16/uint16) crashed the
// product launcher with "ClassCastException: [I cannot be cast to [B".
// fmt's reflect walk reads elements through a pointer to the boxed
// scalar, whose carrier (UnsafeTable.makeCarrier) is a one-element int[];
// ReflectValues.arrayGet assumed the D5 narrow backing (byte[]/short[]).
// Unsigned elements with the high bit set (uint8 250 in an ARRAY) still
// print sign-extended through uint32 under graalgo — a separate bug,
// BUG-0003 in docs/dev/bugs.md — so these cases stay below the high bit.
// Run under gc with `go test`, under graalgo with `graalgo test ./dir`.
package narrowarrayfmt

import (
	"fmt"
	"testing"
)

func TestNarrowArraysPrint(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{fmt.Sprint([2]uint8{5, 127}), "[5 127]"},
		{fmt.Sprint([2]int8{-3, 7}), "[-3 7]"},
		{fmt.Sprint([2]uint16{32767, 1}), "[32767 1]"},
		{fmt.Sprint([]uint8{250, 1}), "[250 1]"},
		{fmt.Sprint([2]int16{-32768, 2}), "[-32768 2]"},
		{fmt.Sprint([][]uint8{{1, 2}}), "[[1 2]]"},
		{fmt.Sprint([2][2]uint8{{1, 2}, {3, 4}}), "[[1 2] [3 4]]"},
		{fmt.Sprintf("%v", [2][2][2][2]uint8{}), "[[[[0 0] [0 0]] [[0 0] [0 0]]] [[[0 0] [0 0]] [[0 0] [0 0]]]]"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
}
