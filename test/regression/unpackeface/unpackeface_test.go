// Regression test for reflect.unpackEface intrinsic (RC-4a).
//
// reflect.unpackEface(i any) Value uses unsafe interface reinterpretation
// (*(*abi.EmptyInterface)(unsafe.Pointer(&i))) which relies on gc's memory
// layout. graalgo represents interfaces as high-level GoInterface objects,
// so the reinterpretation produces a broken value and the subsequent field
// selection (e.Type) fails with ClassCastException.
//
// The fix lowers reflect.unpackEface to the "reflectunpack" wire node,
// which builds the reflect.Value directly from the GoInterface's type and
// data, bypassing the unsafe reinterpretation.
package unpackeface

import (
	"reflect"
	"testing"
)

// TestUnpackEfaceString is the RC-4a reproducer: reflect.ValueOf on a string
// followed by Kind(). Pre-fix this failed with ClassCastException
// (GoInterface cannot be cast to GoStruct) in SelectorNode.readFieldPath
// when unpackEface tried to reinterpret the interface memory layout.
func TestUnpackEfaceString(t *testing.T) {
	v := reflect.ValueOf("hello")
	if k := v.Kind(); k != reflect.String {
		t.Fatalf("ValueOf(\"hello\").Kind() = %v, want String", k)
	}
}

// TestUnpackEfaceInt verifies unpackEface works for integer types.
func TestUnpackEfaceInt(t *testing.T) {
	v := reflect.ValueOf(42)
	if k := v.Kind(); k != reflect.Int {
		t.Fatalf("ValueOf(42).Kind() = %v, want Int", k)
	}
}

// TestUnpackEfaceNil verifies unpackEface handles nil interfaces.
// Pre-fix: nil would cause issues in the reinterpretation.
// Post-fix: returns zero Value.
func TestUnpackEfaceNil(t *testing.T) {
	var i any
	v := reflect.ValueOf(i)
	if v.IsValid() {
		t.Fatalf("ValueOf(nil).IsValid() = true, want false")
	}
}
