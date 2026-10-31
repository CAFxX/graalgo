package jsonrangelast

import (
	"encoding/json"
	"testing"
)

// Regression test for review N-01 (2026-10-31 codebase review):
// synth_json.go carried a P3-M7 WORKAROUND claiming that "range-over-slice
// drops the last element in synthetic packages", and manually indexed the
// sorted keys of map[string]any instead of ranging over them. A
// reproduce-first check (guest program marshaling []any slices through
// the synthetic encoding/json, whose []any case already ranged over the
// slice) showed the claim does not reproduce: graalgo output was
// byte-identical to gc, last element included. The workaround was removed
// and this test pins the corrected behavior: range-over-slice inside the
// synthetic encoding/json package must visit the last element.
//
// The test asserts exact marshal output, so a last-element drop fails it:
// dropping the last element of [1,2,3,4,5] would yield "[1,2,3,4]",
// and dropping the last sorted map key would omit "e" from the object.
func TestSyntheticJsonRangeVisitsLastElement(t *testing.T) {
	// Slice case: exercises range-over-slice in the synthetic package's
	// []any marshal path directly.
	b, err := json.Marshal([]any{1, 2, 3, 4, 5})
	if err != nil {
		t.Fatalf("json.Marshal slice: unexpected err: %v", err)
	}
	if got := string(b); got != "[1,2,3,4,5]" {
		t.Fatalf("json.Marshal([]any{1,2,3,4,5}) = %s, want [1,2,3,4,5] (last element missing?)", got)
	}

	// Single-element slice: the only element IS the last element.
	b, err = json.Marshal([]any{42})
	if err != nil {
		t.Fatalf("json.Marshal single: unexpected err: %v", err)
	}
	if got := string(b); got != "[42]" {
		t.Fatalf("json.Marshal([]any{42}) = %s, want [42]", got)
	}

	// Map case: the former workaround site, now a plain range loop over
	// the sorted keys. Keys sort to a,b,c,d,e; the last key "e" must
	// be present.
	b, err = json.Marshal(map[string]any{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5})
	if err != nil {
		t.Fatalf("json.Marshal map: unexpected err: %v", err)
	}
	if got := string(b); got != `{"a":1,"b":2,"c":3,"d":4,"e":5}` {
		t.Fatalf("json.Marshal(map) = %s, want {\"a\":1,\"b\":2,\"c\":3,\"d\":4,\"e\":5}", got)
	}

	// Nested case: a slice inside a map value, range inside range.
	b, err = json.Marshal(map[string]any{"nums": []any{7, 8, 9}})
	if err != nil {
		t.Fatalf("json.Marshal nested: unexpected err: %v", err)
	}
	if got := string(b); got != `{"nums":[7,8,9]}` {
		t.Fatalf("json.Marshal(nested) = %s, want {\"nums\":[7,8,9]}", got)
	}

	// String slice: last element is a string, not an int, so the
	// check is not specific to one element type.
	b, err = json.Marshal([]any{"a", "b", "c"})
	if err != nil {
		t.Fatalf("json.Marshal strings: unexpected err: %v", err)
	}
	if got := string(b); got != `["a","b","c"]` {
		t.Fatalf("json.Marshal(strings) = %s, want [\"a\",\"b\",\"c\"]", got)
	}
}

// TestSyntheticJsonRangeDoesNotDependOnElementCount guards against a
// length-dependent variant of the alleged bug (e.g. dropping the last
// element only above some length) by sweeping lengths 1..8 and requiring
// the final element of each to appear in the output.
func TestSyntheticJsonRangeAllLengths(t *testing.T) {
	for n := 1; n <= 8; n++ {
		vals := make([]any, n)
		for i := range vals {
			vals[i] = i + 1
		}
		b, err := json.Marshal(vals)
		if err != nil {
			t.Fatalf("len %d: unexpected err: %v", n, err)
		}
		// The full expected rendering, last element included: a
		// dropped last element changes both the suffix and the length.
		want := "["
		for i := 1; i <= n; i++ {
			if i > 1 {
				want += ","
			}
			want += jsonRangeLastItoa(i)
		}
		want += "]"
		if got := string(b); got != want {
			t.Fatalf("len %d: Marshal = %s, want %s", n, got, want)
		}
	}
}

func jsonRangeLastItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
