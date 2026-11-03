package reflectsetint

import (
	"reflect"
	"testing"
)

// Regression tests for the graalgo independent codebase review P1-1
// follow-up lane rev1103-reflect-set-int: reflect.Value.Set on an int
// target misread the target's type as int64.
//
// Root cause (verified differentially against gc 1.27.1): the
// structural type-identity keys keyed basic types by GoKind, and GoKind
// has no INT kind — int and int64 both map to INT64 (likewise uint /
// uint64 / uintptr to UINT64, rune to INT32, byte to UINT8). So *int
// and *int64 shared one dynamic-type descriptor, and the descriptor's
// reflect token resolved to whichever wire id was recorded last
// (*int64 here). Consequences, both pinned below:
//   - Set(int) onto an int target falsely panicked with
//     "value of type int is not assignable to type int64";
//   - Set(int64) onto an int target falsely SUCCEEDED (gc panics).
// The fix keys structural identity by canonical basic name in all
// three copies of the key function (NodeFactory.structuralKey,
// DescriptorNames.structuralKey, ReflectTypes.structuralKey).
//
// Scope note: struct-field and slice-element Set paths are NOT pinned
// here — at the lane base they fail independently (struct-field Set
// throws a host ClassCastException, slice-element Set a typedmemmove
// host error); those belong to the slice-header writeback lane.

// recoverCase runs f, requires it to panic with the exact string value
// want, and requires execution to continue afterwards.
func recoverCase(t *testing.T, name string, f func(), want string) {
	t.Helper()
	continued := false
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatalf("%s: expected panic %q, got none", name, want)
			}
			s, ok := r.(string)
			if !ok {
				t.Fatalf("%s: panic value type %T, want string", name, r)
			}
			if s != want {
				t.Fatalf("%s: panic = %q, want %q", name, s, want)
			}
			continued = true
		}()
		f()
	}()
	if !continued {
		t.Fatalf("%s: execution did not continue after recover", name)
	}
}

func TestSetIntToIntLocal(t *testing.T) {
	x := 1
	reflect.ValueOf(&x).Elem().Set(reflect.ValueOf(2))
	if x != 2 {
		t.Fatalf("x = %d after Set(int), want 2", x)
	}
	var y int = 3
	reflect.ValueOf(&x).Elem().Set(reflect.ValueOf(y))
	if x != 3 {
		t.Fatalf("x = %d after Set(int var), want 3", x)
	}
}

func TestSetInt64ToInt64Local(t *testing.T) {
	x := int64(1)
	reflect.ValueOf(&x).Elem().Set(reflect.ValueOf(int64(2)))
	if x != 2 {
		t.Fatalf("x = %d after Set(int64), want 2", x)
	}
}

func TestSetInt64ToIntPanics(t *testing.T) {
	x := 1
	recoverCase(t, "Set(int64) onto int", func() {
		reflect.ValueOf(&x).Elem().Set(reflect.ValueOf(int64(2)))
	}, "reflect.Set: value of type int64 is not assignable to type int")
	if x != 1 {
		t.Fatalf("x = %d after rejected Set, want 1", x)
	}
}

func TestSetIntToInt64Panics(t *testing.T) {
	x := int64(1)
	recoverCase(t, "Set(int) onto int64", func() {
		reflect.ValueOf(&x).Elem().Set(reflect.ValueOf(2))
	}, "reflect.Set: value of type int is not assignable to type int64")
	if x != 1 {
		t.Fatalf("x = %d after rejected Set, want 1", x)
	}
}

func TestPointerElemTypeIdentity(t *testing.T) {
	x := 1
	y := int64(1)
	intType := reflect.TypeOf(1)
	int64Type := reflect.TypeOf(int64(1))
	if got := reflect.ValueOf(&x).Elem().Type(); got != intType {
		t.Fatalf("Elem(*int).Type() = %v, want %v", got, intType)
	}
	if got := reflect.ValueOf(&y).Elem().Type(); got != int64Type {
		t.Fatalf("Elem(*int64).Type() = %v, want %v", got, int64Type)
	}
	if reflect.ValueOf(&x).Elem().Type() == reflect.ValueOf(&y).Elem().Type() {
		t.Fatalf("Elem(*int).Type() == Elem(*int64).Type(), want distinct")
	}
	if k := reflect.TypeOf(&x).Elem().Kind(); k != reflect.Int {
		t.Fatalf("TypeOf(&x).Elem().Kind() = %v, want Int", k)
	}
	if k := reflect.TypeOf(&y).Elem().Kind(); k != reflect.Int64 {
		t.Fatalf("TypeOf(&y).Elem().Kind() = %v, want Int64", k)
	}
	if s := reflect.TypeOf(&x).Elem().String(); s != "int" {
		t.Fatalf("TypeOf(&x).Elem().String() = %q, want \"int\"", s)
	}
}

func TestPointerToTypeIdentity(t *testing.T) {
	if got, want := reflect.PointerTo(reflect.TypeOf(1)), reflect.TypeOf(new(int)); got != want {
		t.Fatalf("PointerTo(int) = %v, want %v", got, want)
	}
	if reflect.PointerTo(reflect.TypeOf(1)) == reflect.PointerTo(reflect.TypeOf(int64(1))) {
		t.Fatalf("PointerTo(int) == PointerTo(int64), want distinct")
	}
}

func TestCompositeTypeIdentity(t *testing.T) {
	// Slices: []int vs []int64 share GoKind structure but are
	// distinct Go types; their element kinds must differ.
	if reflect.TypeOf([]int{}) == reflect.TypeOf([]int64{}) {
		t.Fatalf("TypeOf([]int{}) == TypeOf([]int64{}), want distinct")
	}
	if k := reflect.TypeOf([]int{}).Elem().Kind(); k != reflect.Int {
		t.Fatalf("Elem([]int).Kind() = %v, want Int", k)
	}
	if k := reflect.TypeOf([]int64{}).Elem().Kind(); k != reflect.Int64 {
		t.Fatalf("Elem([]int64).Kind() = %v, want Int64", k)
	}
	// uint / uint64 / uintptr are three distinct types.
	if reflect.TypeOf(uint(1)) == reflect.TypeOf(uint64(1)) {
		t.Fatalf("TypeOf(uint) == TypeOf(uint64), want distinct")
	}
	if reflect.TypeOf(uint(1)) == reflect.TypeOf(uintptr(1)) {
		t.Fatalf("TypeOf(uint) == TypeOf(uintptr), want distinct")
	}
	u := uint(1)
	if got := reflect.ValueOf(&u).Elem().Type(); got != reflect.TypeOf(uint(1)) {
		t.Fatalf("Elem(*uint).Type() = %v, want %v", got, reflect.TypeOf(uint(1)))
	}
	// Func types: func(int) vs func(int64) are distinct.
	var f1 func(int)
	var f2 func(int64)
	if reflect.TypeOf(f1) == reflect.TypeOf(f2) {
		t.Fatalf("TypeOf(func(int)) == TypeOf(func(int64)), want distinct")
	}
	// Chans: chan int vs chan int64 are distinct.
	var c1 chan int
	var c2 chan int64
	if reflect.TypeOf(c1) == reflect.TypeOf(c2) {
		t.Fatalf("TypeOf(chan int) == TypeOf(chan int64), want distinct")
	}
}

func TestNewIntElemSet(t *testing.T) {
	// Control: the reflect.New path was already correct at the lane
	// base; pin it so the fix cannot regress it.
	v := reflect.New(reflect.TypeOf(1)).Elem()
	v.Set(reflect.ValueOf(3))
	if v.Int() != 3 {
		t.Fatalf("New(int).Elem() = %d after Set, want 3", v.Int())
	}
}
