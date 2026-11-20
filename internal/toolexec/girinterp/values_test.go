package girinterp

import (
	"io"
	"math"
	"testing"
)

func newTestInterp(t *testing.T) *Interp {
	t.Helper()
	in, err := New(nil, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// panicText runs f and returns the text of the Go panic it raises
// through panicValue ("" when it does not panic).
func panicText(in *Interp, f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = in.panicText(r.(*goPanic).val)
		}
	}()
	f()
	return ""
}

// TestMapAssignReplacesKey: m[+0] = a; m[-0] = b leaves the key -0, for a
// float key and for an interface key holding a float (BUG-0042).
func TestMapAssignReplacesKey(t *testing.T) {
	in := newTestInterp(t)
	f64 := in.basic(KFloat64)
	iface := &RType{Kind: KInterface, Key: "interface{}", Str: "interface {}"}
	negZero := math.Copysign(0, -1)
	for _, c := range []struct {
		name string
		kt   *RType
		pos  Value
		neg  Value
	}{
		{"float64", f64, 0.0, negZero},
		{"interface", iface, Iface{T: f64, V: 0.0}, Iface{T: f64, V: negZero}},
	} {
		m := newMap()
		m.set(in, c.pos, c.kt, "pos")
		m.set(in, c.neg, c.kt, "neg")
		if m.n != 1 {
			t.Fatalf("%s: %d entries, want 1", c.name, m.n)
		}
		k := m.keys[0]
		if x, ok := k.(Iface); ok {
			k = x.V
		}
		if !math.Signbit(k.(float64)) || m.vals[0] != "neg" {
			t.Errorf("%s: entry %v=%v, want -0=neg", c.name, k, m.vals[0])
		}
	}
}

// TestMapKeyErrorOnEmptyMap: a lookup or delete of an unhashable
// interface key on a nil or empty map panics with gc's
// unhashableTypeError text; a non-empty map hashes it and panics with
// the runtime errorString; a hashable key does not panic (BUG-0041).
func TestMapKeyErrorOnEmptyMap(t *testing.T) {
	in := newTestInterp(t)
	iface := &RType{Kind: KInterface, Key: "interface{}", Str: "interface {}"}
	ints := &RType{Kind: KSlice, Key: "[]int", Str: "[]int", Elem: in.basic(KInt)}
	bad := Iface{T: ints, V: Slice{}}
	full := newMap()
	full.set(in, Iface{T: in.basic(KInt), V: 1}, iface, 1)
	const plain = "hash of unhashable type: []int"
	for _, c := range []struct {
		name string
		f    func()
		want string
	}{
		{"nil lookup", func() { (*Map)(nil).lookup(in, bad, iface) }, plain},
		{"empty lookup", func() { newMap().lookup(in, bad, iface) }, plain},
		{"nil delete", func() { (*Map)(nil).del(in, bad, iface) }, plain},
		{"empty delete", func() { newMap().del(in, bad, iface) }, plain},
		{"full lookup", func() { full.lookup(in, bad, iface) }, "runtime error: hash of unhashable type []int"},
		{"hashable", func() { newMap().lookup(in, Iface{T: in.basic(KInt), V: 1}, iface) }, ""},
	} {
		if got := panicText(in, c.f); got != c.want {
			t.Errorf("%s: panic %q, want %q", c.name, got, c.want)
		}
	}
}

// TestSliceToArrayPointerAliases: (*[n]T)(s) shares s's backing store in
// both directions, repeated conversions give the same pointer, and
// [n]T(s) is a copy (BUG-0039).
func TestSliceToArrayPointerAliases(t *testing.T) {
	in := newTestInterp(t)
	it := in.basic(KInt)
	sl := &RType{Kind: KSlice, Key: "[]int", Str: "[]int", Elem: it}
	at := &RType{Kind: KArray, Key: "[2]int", Str: "[2]int", Elem: it, Len: 2}
	pt := &RType{Kind: KPointer, Key: "*[2]int", Str: "*[2]int", Elem: at}
	s := Slice{A: []Value{1, 2, 3, 4}, Off: 1, Len: 3, Cap: 3}

	p := in.convert(s, sl, pt).(*Value)
	a := (*p).(*Array)
	a.E[0] = 20
	if s.A[1] != 20 {
		t.Errorf("write through *[2]int not seen by the slice: %v", s.A)
	}
	s.A[2] = 30
	if a.E[1] != 30 {
		t.Errorf("write through the slice not seen by *[2]int: %v", a.E)
	}
	if &a.E[1] != &s.A[2] {
		t.Error("&p[1] != &s[1]")
	}
	if q := in.convert(s, sl, pt).(*Value); q != p {
		t.Error("converting the same slice twice gave different pointers")
	}
	store(p, &Array{E: []Value{7, 8}})
	if s.A[1] != 7 || s.A[2] != 8 {
		t.Errorf("*p = [2]int{7, 8} not seen by the slice: %v", s.A)
	}

	c := in.convert(s, sl, at).(*Array)
	c.E[0] = 99
	if s.A[1] == 99 {
		t.Error("[2]int(s) aliases the slice")
	}
	if got := panicText(in, func() { in.convert(Slice{A: []Value{1}, Len: 1, Cap: 1}, sl, pt) }); got !=
		"runtime error: cannot convert slice with length 1 to array or pointer to array with length 2" {
		t.Errorf("short slice: panic %q", got)
	}
	if got := in.convert(Slice{}, sl, &RType{Kind: KPointer, Key: "*[0]int", Str: "*[0]int",
		Elem: &RType{Kind: KArray, Key: "[0]int", Str: "[0]int", Elem: it}}).(*Value); got != nil {
		t.Error("nil slice to *[0]int is not nil")
	}
}
