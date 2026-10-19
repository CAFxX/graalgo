// Regression tests for the reflect type-token key-scheme divergence
// (independent review 2026-10-19-07, P1): ReflectOpNode.sliceTokFor keyed
// the global type table with a kind-only element key ("slice:basic:ARRAY")
// while ReflectTypes.sliceOf uses a fully structural key. Consequences:
//  1. The Value.Slice-on-array path minted a different token for []E than
//     reflect.SliceOf did, so Go-level reflect.Type == (token identity on
//     the single-field rtype struct) returned false where gc returns true.
//  2. Two distinct unnamed composite element types of the same GoKind
//     collided under one key (first-interned elem won), so after slicing
//     both in one process, Type().Elem() / Index(0).Type() lied about the
//     second type.
//
// The same bug family existed in boundMethodTok (bound method values):
// it built an ad-hoc kind-only func key ("func(basic:ARRAY)(basic:INT)")
// that collided distinct composite parameter types and diverged from the
// canonical ReflectTypes.funcOf structural key, so a bound method value's
// Type() != reflect.FuncOf of the same signature.
//
// The fix delegates sliceTokFor to ReflectTypes.sliceOf and boundMethodTok
// to ReflectTypes.funcOf, unifying both minting paths on the structural
// key. Every test below fails on the pre-fix code in both halves (token
// inequality; elem mismatch) and passes after.
package reflectslicetok

import (
	"reflect"
	"testing"
)

// The array-slicing path must mint the same token as reflect.SliceOf for
// the element type. Pre-fix: Value.Slice minted []E under
// "slice:basic:ARRAY" while SliceOf minted it under "slice:array:2:basic:INT",
// so the Type() == comparison below returned false.
func TestSlicedArrayTypeEqualsSliceOf(t *testing.T) {
	arr := [2][2]int{{1, 2}, {3, 4}}
	s := reflect.ValueOf(&arr).Elem().Slice(0, 2)

	got := s.Type()
	want := reflect.SliceOf(reflect.TypeOf([2]int{}))
	if got != want {
		t.Fatalf("sliced [2][2]int: Type() = %v, want %v (token identity)", got, want)
	}
	if got.Elem() != reflect.TypeOf([2]int{}) {
		t.Fatalf("sliced [2][2]int: Elem() = %v, want [2]int", got.Elem())
	}
	if s.Index(0).Type() != reflect.TypeOf([2]int{}) {
		t.Fatalf("sliced [2][2]int: Index(0).Type() = %v, want [2]int", s.Index(0).Type())
	}
	// The sliced value's element values must be intact.
	if s.Index(1).Index(0).Int() != 3 {
		t.Fatalf("sliced [2][2]int: element value = %d, want 3", s.Index(1).Index(0).Int())
	}
}

// Slicing two arrays whose element types are distinct unnamed composites
// of the same GoKind must not collide: each sliced type keeps its own
// element type. Pre-fix both minted "slice:basic:ARRAY" (first-interned
// [2]int won), so s2.Type() == s1.Type() and s2.Type().Elem() lied.
func TestSlicedArrayTypesDoNotCollide(t *testing.T) {
	arr1 := [2][2]int{{1, 2}, {3, 4}}
	arr2 := [2][3]byte{{5, 6, 7}, {8, 9, 10}}
	s1 := reflect.ValueOf(&arr1).Elem().Slice(0, 2)
	s2 := reflect.ValueOf(&arr2).Elem().Slice(0, 2)

	want1 := reflect.SliceOf(reflect.TypeOf([2]int{}))
	want2 := reflect.SliceOf(reflect.TypeOf([3]byte{}))
	if s1.Type() != want1 {
		t.Fatalf("s1: Type() = %v, want %v", s1.Type(), want1)
	}
	if s2.Type() != want2 {
		t.Fatalf("s2: Type() = %v, want %v", s2.Type(), want2)
	}
	if s1.Type() == s2.Type() {
		t.Fatalf("s1.Type() == s2.Type() = %v: distinct slice types collided", s1.Type())
	}
	if s2.Type().Elem() != reflect.TypeOf([3]byte{}) {
		t.Fatalf("s2: Elem() = %v, want [3]byte", s2.Type().Elem())
	}
	if s2.Index(0).Type() != reflect.TypeOf([3]byte{}) {
		t.Fatalf("s2: Index(0).Type() = %v, want [3]byte", s2.Index(0).Type())
	}
}

// The Slice3 path (second sliceTokFor caller) shares the fix: a [2][]string
// array sliced with 3 indices must equal SliceOf([]string).
func TestSlicedArraySlice3TypeEqualsSliceOf(t *testing.T) {
	arr := [2][]string{{"a"}, {"b"}}
	s := reflect.ValueOf(&arr).Elem().Slice3(0, 2, 2)

	want := reflect.SliceOf(reflect.SliceOf(reflect.TypeOf("")))
	if s.Type() != want {
		t.Fatalf("Slice3 [2][]string: Type() = %v, want %v", s.Type(), want)
	}
	if s.Type().Elem() != reflect.SliceOf(reflect.TypeOf("")) {
		t.Fatalf("Slice3 [2][]string: Elem() = %v, want []string", s.Type().Elem())
	}
}

// A bound method value's func type (receiver stripped) must be
// token-identical to reflect.FuncOf of the same signature. Pre-fix the
// bound-method path minted "func(basic:ARRAY)(basic:INT)" while FuncOf
// minted "func:array:2:basic:INT,->basic:INT,", so the comparison below
// returned false.
type slicetokT struct{}

func (slicetokT) F(a [2]int) int { return a[0] }

func TestMethodValueTypeEqualsFuncOf(t *testing.T) {
	m := reflect.ValueOf(slicetokT{}).MethodByName("F")

	got := m.Type()
	want := reflect.FuncOf(
		[]reflect.Type{reflect.TypeOf([2]int{})},
		[]reflect.Type{reflect.TypeOf(0)},
		false,
	)
	if got != want {
		t.Fatalf("method value: Type() = %v, want %v (token identity)", got, want)
	}
	if got.NumIn() != 1 || got.In(0) != reflect.TypeOf([2]int{}) {
		t.Fatalf("method value: In(0) = %v, want [2]int", got.In(0))
	}
	if got.NumOut() != 1 || got.Out(0) != reflect.TypeOf(0) {
		t.Fatalf("method value: Out(0) = %v, want int", got.Out(0))
	}
}

// Two bound methods whose composite parameter types share a GoKind but
// differ structurally must not collide under one func token. Pre-fix both
// minted "func(basic:ARRAY)(basic:INT)" (first-interned [2]int won), so
// h.Type() == g.Type() and h.Type().In(0) lied.
func (slicetokT) G(a [2]int) int  { return a[0] }
func (slicetokT) H(a [3]byte) int { return int(a[0]) }

func TestMethodValueTypesDoNotCollide(t *testing.T) {
	v := reflect.ValueOf(slicetokT{})
	g := v.MethodByName("G")
	h := v.MethodByName("H")

	if g.Type() == h.Type() {
		t.Fatalf("G and H method values share Type() = %v: distinct func types collided", g.Type())
	}
	if h.Type().In(0) != reflect.TypeOf([3]byte{}) {
		t.Fatalf("H method value: In(0) = %v, want [3]byte", h.Type().In(0))
	}
	if g.Type().In(0) != reflect.TypeOf([2]int{}) {
		t.Fatalf("G method value: In(0) = %v, want [2]int", g.Type().In(0))
	}
}
