// Regression test for the internal/abi.TypeOf intrinsic (fix-redmaster-buildok r5).
//
// Root cause: internal/abi.TypeOf uses unsafe interface reinterpretation
// (*(*EmptyInterface)(unsafe.Pointer(&a))), which relies on gc's memory layout.
// graalgo represents interfaces as GoInterface objects, so the reinterpretation
// yields null data and NPEs. The fix lowers abi.TypeOf to the "abitypeof" wire
// node (a proper intrinsic), implemented by AbiTypeOfNode via
// ReflectTypes.materialize().
//
// This test exercises the intrinsic via reflect.TypeOf (which calls abi.TypeOf
// internally), covering:
//   - nil-interface → nil (AbiTypeOfNode nil handling)
//   - Elem() on ptr (materialize POINTER with Elem)
//   - Derived descriptors for Chan/Map/Slice/Array/Func (P1-A: materialize
//     must build proper derived structs, not base Type-only)
//   - toRType wrap (reflect.toRType wraps *abi.Type in *rtype; exercised
//     implicitly by TypeOf returning a usable reflect.Type)
//
// Pre-fix (master without the intrinsic): these tests fail with NPE in
// abi.TypeOf's unsafe reinterpretation, or with missing derived fields.
//
// Note: abi.NoEscape identity is covered by the frontend unit test
// (internal/frontend/abinoescape_test.go), since it's a frontend lowering.
package abitypeof

import (
	"reflect"
	"testing"
)

// TestTypeOfNilInterface: reflect.TypeOf(nil) must return nil, not crash.
// Exercises AbiTypeOfNode's nil-interface → nil path.
func TestTypeOfNilInterface(t *testing.T) {
	var x any
	if got := reflect.TypeOf(x); got != nil {
		t.Fatalf("TypeOf(nil) = %v, want nil", got)
	}
}

// TestTypeOfPtrElem: TypeOf on a pointer, then Elem(), must work.
// Exercises materialize() for POINTER kind with Elem populated.
func TestTypeOfPtrElem(t *testing.T) {
	typ := reflect.TypeOf((*int)(nil))
	if typ == nil {
		t.Fatal("TypeOf((*int)(nil)) = nil, want non-nil")
	}
	if typ.Kind() != reflect.Ptr {
		t.Fatalf("Kind() = %v, want Ptr", typ.Kind())
	}
	elem := typ.Elem()
	if elem == nil {
		t.Fatal("Elem() = nil, want non-nil")
	}
	if elem.Kind() != reflect.Int {
		t.Fatalf("Elem().Kind() = %v, want Int", elem.Kind())
	}
}

// TestTypeOfChan: ChanType.Elem and ChanDir must be readable.
// Exercises materialize() for CHAN kind (P1-A: derived descriptor).
func TestTypeOfChan(t *testing.T) {
	typ := reflect.TypeOf((chan int)(nil))
	if typ == nil {
		t.Fatal("TypeOf((chan int)(nil)) = nil, want non-nil")
	}
	if typ.Kind() != reflect.Chan {
		t.Fatalf("Kind() = %v, want Chan", typ.Kind())
	}
	elem := typ.Elem()
	if elem == nil || elem.Kind() != reflect.Int {
		t.Fatalf("Elem().Kind() = %v, want Int", elem)
	}
	if dir := typ.ChanDir(); dir != reflect.BothDir {
		t.Fatalf("ChanDir() = %v, want BothDir", dir)
	}
}

// TestTypeOfMap: MapType.Key and MapType.Elem must be readable.
// Exercises materialize() for MAP kind (P1-A: derived descriptor).
func TestTypeOfMap(t *testing.T) {
	typ := reflect.TypeOf(map[string]int(nil))
	if typ == nil {
		t.Fatal("TypeOf(map[string]int(nil)) = nil, want non-nil")
	}
	if typ.Kind() != reflect.Map {
		t.Fatalf("Kind() = %v, want Map", typ.Kind())
	}
	key := typ.Key()
	if key == nil || key.Kind() != reflect.String {
		t.Fatalf("Key().Kind() = %v, want String", key)
	}
	elem := typ.Elem()
	if elem == nil || elem.Kind() != reflect.Int {
		t.Fatalf("Elem().Kind() = %v, want Int", elem)
	}
}

// TestTypeOfSlice: SliceType.Elem must be readable.
// Exercises materialize() for SLICE kind (P1-A: derived descriptor).
func TestTypeOfSlice(t *testing.T) {
	typ := reflect.TypeOf([]int(nil))
	if typ == nil {
		t.Fatal("TypeOf([]int(nil)) = nil, want non-nil")
	}
	if typ.Kind() != reflect.Slice {
		t.Fatalf("Kind() = %v, want Slice", typ.Kind())
	}
	elem := typ.Elem()
	if elem == nil || elem.Kind() != reflect.Int {
		t.Fatalf("Elem().Kind() = %v, want Int", elem)
	}
}

// TestTypeOfArray: ArrayType.Elem and Len must be readable.
// Exercises materialize() for ARRAY kind (P1-A: derived descriptor).
func TestTypeOfArray(t *testing.T) {
	typ := reflect.TypeOf([5]int{})
	if typ == nil {
		t.Fatal("TypeOf([5]int{}) = nil, want non-nil")
	}
	if typ.Kind() != reflect.Array {
		t.Fatalf("Kind() = %v, want Array", typ.Kind())
	}
	elem := typ.Elem()
	if elem == nil || elem.Kind() != reflect.Int {
		t.Fatalf("Elem().Kind() = %v, want Int", elem)
	}
	if l := typ.Len(); l != 5 {
		t.Fatalf("Len() = %d, want 5", l)
	}
}

// TestTypeOfFunc: FuncType.NumIn and NumOut must be readable.
// Exercises materialize() for FUNC kind (P1-A: derived descriptor).
func TestTypeOfFunc(t *testing.T) {
	typ := reflect.TypeOf(func(int, string) bool { return true })
	if typ == nil {
		t.Fatal("TypeOf(func) = nil, want non-nil")
	}
	if typ.Kind() != reflect.Func {
		t.Fatalf("Kind() = %v, want Func", typ.Kind())
	}
	if n := typ.NumIn(); n != 2 {
		t.Fatalf("NumIn() = %d, want 2", n)
	}
	if n := typ.NumOut(); n != 1 {
		t.Fatalf("NumOut() = %d, want 1", n)
	}
}

// TestTypeOfStruct: StructType must materialize (not base Type-only).
// Exercises materialize() for STRUCT kind (P1-A: derived descriptor).
func TestTypeOfStruct(t *testing.T) {
	type S struct {
		X int
		Y string
	}
	typ := reflect.TypeOf(S{})
	if typ == nil {
		t.Fatal("TypeOf(S{}) = nil, want non-nil")
	}
	if typ.Kind() != reflect.Struct {
		t.Fatalf("Kind() = %v, want Struct", typ.Kind())
	}
	// NumField exercises the derived StructType descriptor.
	if n := typ.NumField(); n != 2 {
		t.Fatalf("NumField() = %d, want 2", n)
	}
}

// TestElemOnNonPtrPanics: Elem() on a non-composite type must panic per spec.
// Negative coverage: the intrinsic must not silently return wrong results.
func TestElemOnNonPtrPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Elem() on int did not panic, want panic per reflect spec")
		}
	}()
	typ := reflect.TypeOf(42)
	_ = typ.Elem() // must panic: reflect: Elem of invalid type
}
