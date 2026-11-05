// Regression test for the StructField.Index backing-store defect:
// ReflectOpNode.intSlice packed FieldInfo.index as an int[] GoSlice,
// but Go int is 64-bit in graalgo (GoKind: "int" -> INT64), so reading
// an Index element through the normal slice-index path
// (IndexNode -> Boxing.arrayGet with kind INT64 -> ((long[]) array)[i])
// threw ClassCastException. Pre-fix this test crashed on the first
// Index[0] read; post-fix the backing is long[] and the reads succeed.
//
// Also covers the second consumer of the same backing: printing the
// Index slice (%v) goes through the same kind-driven arrayGet.
package structfieldindex

import (
	"fmt"
	"reflect"
	"testing"
)

type Inner struct {
	X int
}

type Outer struct {
	Inner // embedded: promoted X has Index [0 0]
	Name  string
	Age   int
}

// TestFieldIndexElements is the direct reproducer: element reads of
// reflect.Type.Field(i).Index through the normal Go index path.
func TestFieldIndexElements(t *testing.T) {
	typ := reflect.TypeOf(Outer{})
	want := [][]int{{0}, {1}, {2}}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if len(f.Index) != len(want[i]) {
			t.Fatalf("Field(%d).Index len = %d, want %d", i, len(f.Index), len(want[i]))
		}
		for j, w := range want[i] {
			if got := f.Index[j]; got != w {
				t.Fatalf("Field(%d).Index[%d] = %d, want %d", i, j, got, w)
			}
		}
	}
}

// TestEmbeddedFieldIndex covers a multi-element Index slice: the
// promoted field X lives at index path [0 0].
func TestEmbeddedFieldIndex(t *testing.T) {
	typ := reflect.TypeOf(Outer{})
	f, ok := typ.FieldByName("X")
	if !ok {
		t.Fatal("FieldByName(\"X\"): not found")
	}
	if len(f.Index) != 2 {
		t.Fatalf("promoted X Index len = %d, want 2", len(f.Index))
	}
	if f.Index[0] != 0 || f.Index[1] != 0 {
		t.Fatalf("promoted X Index = [%d %d], want [0 0]", f.Index[0], f.Index[1])
	}
	// Control: the Java-side index path (FieldByIndex) never went through
	// the slice backing, so it passed even pre-fix.
	if got := typ.FieldByIndex([]int{0, 0}); got.Name != "X" {
		t.Fatalf("FieldByIndex([0 0]).Name = %q, want %q", got.Name, "X")
	}
}

// TestFieldIndexPrint covers printing the Index slice, which reads the
// same backing through the kind-driven arrayGet.
func TestFieldIndexPrint(t *testing.T) {
	f := reflect.TypeOf(Outer{}).Field(1)
	if got := fmt.Sprintf("%v", f.Index); got != "[1]" {
		t.Fatalf("Sprintf(%q, Field(1).Index) = %q, want %q", "%v", got, "[1]")
	}
}
