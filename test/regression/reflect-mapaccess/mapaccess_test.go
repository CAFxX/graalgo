// Regression tests for the reflect mapaccess/mapassign/mapdelete/
// typehash P0 (fix-rv1107-10-mapaccess): core reflect MapIndex /
// MapKeys / SetMapIndex / MapOf were broken at runtime because the
// mapaccess family (reflect.mapaccess, mapaccess_faststr, mapassign0,
// mapassign_faststr0, mapdelete, mapdelete_faststr, typehash) had no
// backend implementation — calling MapIndex died with
// "linkname: no intrinsic registered and no Go function bound for
// symbol reflect.mapaccess_faststr". The fix registers real backend
// implementations (ReflectLinknames) plus the maps.Iter view
// (MapsView) MapKeys/MapRange need.
//
// Every test below is differential by construction: it asserts the
// exact values gc 1.27.1 produces for the same program (verified
// against the gc baseline before the fix landed). Keys are sorted
// before comparison — reflect iteration order is randomized.
package reflectmapaccess

import (
	"reflect"
	"sort"
	"testing"
)

type structKey struct {
	A int
	B string
}

func TestMapIndexStringKeys(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	v := reflect.ValueOf(m)
	got := v.MapIndex(reflect.ValueOf("a"))
	if !got.IsValid() || got.Int() != 1 {
		t.Fatalf("MapIndex(a) = (%v, valid=%v), want (1, true)", got, got.IsValid())
	}
	if got := v.MapIndex(reflect.ValueOf("z")); got.IsValid() {
		t.Fatalf("MapIndex(z) valid, want invalid (zero Value)")
	}
}

func TestMapIndexIntKeys(t *testing.T) {
	m := map[int]string{10: "x", 20: "y"}
	v := reflect.ValueOf(m)
	got := v.MapIndex(reflect.ValueOf(10))
	if !got.IsValid() || got.String() != "x" {
		t.Fatalf("MapIndex(10) = %q, want %q", got.String(), "x")
	}
}

func TestMapIndexStructKeys(t *testing.T) {
	m := map[structKey]bool{{A: 1, B: "s"}: true}
	v := reflect.ValueOf(m)
	got := v.MapIndex(reflect.ValueOf(structKey{A: 1, B: "s"}))
	if !got.IsValid() || !got.Bool() {
		t.Fatalf("MapIndex(struct key) = (%v, valid=%v), want (true, true)", got, got.IsValid())
	}
}

func TestMapIndexNilMap(t *testing.T) {
	var m map[string]int
	v := reflect.ValueOf(m)
	if got := v.MapIndex(reflect.ValueOf("a")); got.IsValid() {
		t.Fatalf("MapIndex on nil map valid, want invalid")
	}
}

func TestMapIndexPointerElem(t *testing.T) {
	n := 42
	m := map[string]*int{"p": &n}
	v := reflect.ValueOf(m)
	got := v.MapIndex(reflect.ValueOf("p"))
	if !got.IsValid() {
		t.Fatalf("MapIndex(p) invalid, want valid")
	}
	elem := got.Elem()
	if !elem.IsValid() || elem.Int() != 42 {
		t.Fatalf("MapIndex(p).Elem() = %v, want 42", elem)
	}
}

func TestMapIndexPointerKeys(t *testing.T) {
	a, b := 1, 2
	m := map[*int]string{&a: "one", &b: "two"}
	v := reflect.ValueOf(m)
	got := v.MapIndex(reflect.ValueOf(&a))
	if !got.IsValid() || got.String() != "one" {
		t.Fatalf("MapIndex(&a) = (%q, valid=%v), want (one, true)", got.String(), got.IsValid())
	}
	c := 3
	if got := v.MapIndex(reflect.ValueOf(&c)); got.IsValid() {
		t.Fatalf("MapIndex(&c) valid, want invalid")
	}
}

func TestMapKeys(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	v := reflect.ValueOf(m)
	keys := v.MapKeys()
	var got []string
	for _, k := range keys {
		got = append(got, k.String())
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("MapKeys() = %v, want [a b]", got)
	}
}

func TestMapKeysNilMap(t *testing.T) {
	var m map[string]int
	v := reflect.ValueOf(m)
	if keys := v.MapKeys(); len(keys) != 0 {
		t.Fatalf("MapKeys() on nil map = %v, want []", keys)
	}
}

func TestMapKeysLarge(t *testing.T) {
	m := make(map[string]int)
	for i := 0; i < 20; i++ {
		m[string(rune('a'+i))] = i
	}
	v := reflect.ValueOf(m)
	keys := v.MapKeys()
	if len(keys) != 20 {
		t.Fatalf("MapKeys() len = %d, want 20", len(keys))
	}
	sum := 0
	for _, k := range keys {
		sum += int(v.MapIndex(k).Int())
	}
	if sum != 190 {
		t.Fatalf("MapKeys() values sum = %d, want 190", sum)
	}
}

func TestSetMapIndex(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	v := reflect.ValueOf(m)
	v.SetMapIndex(reflect.ValueOf("c"), reflect.ValueOf(3))
	if len(m) != 3 || m["c"] != 3 {
		t.Fatalf("SetMapIndex(c, 3): m = %v, want len 3 with c=3", m)
	}
	v.SetMapIndex(reflect.ValueOf("a"), reflect.ValueOf(10))
	if m["a"] != 10 {
		t.Fatalf("SetMapIndex(a, 10): m[a] = %d, want 10", m["a"])
	}
	v.SetMapIndex(reflect.ValueOf("b"), reflect.Value{})
	if _, ok := m["b"]; ok {
		t.Fatalf("SetMapIndex(b, zero) did not delete b: m = %v", m)
	}
	if len(m) != 2 {
		t.Fatalf("after delete len = %d, want 2", len(m))
	}
}

func TestSetMapIndexIntKeys(t *testing.T) {
	m := map[int]string{1: "a"}
	v := reflect.ValueOf(m)
	v.SetMapIndex(reflect.ValueOf(2), reflect.ValueOf("z"))
	if m[2] != "z" {
		t.Fatalf("SetMapIndex(2, z): m = %v", m)
	}
	v.SetMapIndex(reflect.ValueOf(1), reflect.Value{})
	if _, ok := m[1]; ok {
		t.Fatalf("SetMapIndex(1, zero) did not delete 1: m = %v", m)
	}
}

func TestSetMapIndexNilMapPanics(t *testing.T) {
	var m map[string]int
	v := reflect.ValueOf(m)
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("SetMapIndex on nil map did not panic")
		}
	}()
	v.SetMapIndex(reflect.ValueOf("a"), reflect.ValueOf(1))
}

func TestMapOfMakeMap(t *testing.T) {
	mt := reflect.MapOf(reflect.TypeOf(0), reflect.TypeOf(""))
	if mt.String() != "map[int]string" {
		t.Fatalf("MapOf type = %s, want map[int]string", mt.String())
	}
	mv := reflect.MakeMap(mt)
	mv.SetMapIndex(reflect.ValueOf(7), reflect.ValueOf("seven"))
	got := mv.MapIndex(reflect.ValueOf(7))
	if !got.IsValid() || got.String() != "seven" {
		t.Fatalf("MakeMap get = (%q, valid=%v), want (seven, true)", got.String(), got.IsValid())
	}
	if keys := mv.MapKeys(); len(keys) != 1 {
		t.Fatalf("MakeMap MapKeys len = %d, want 1", len(keys))
	}
	if mv.Len() != 1 {
		t.Fatalf("MakeMap Len() = %d, want 1", mv.Len())
	}
}

func TestMapRange(t *testing.T) {
	m := map[string]int{"a": 10, "c": 3}
	v := reflect.ValueOf(m)
	it := v.MapRange()
	got := map[string]int{}
	for it.Next() {
		got[it.Key().String()] = int(it.Value().Int())
	}
	if len(got) != 2 || got["a"] != 10 || got["c"] != 3 {
		t.Fatalf("MapRange() = %v, want map[a:10 c:3]", got)
	}
}
