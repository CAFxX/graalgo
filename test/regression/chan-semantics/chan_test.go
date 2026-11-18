// Regression test (ci-green lane, 2026-11-18): GOROOT/test chancap.go,
// convT2X.go, named.go and cmp.go/typeswitch.go failed to build under the
// product launcher:
//   - make(C) with a defined channel type (type C chan int): "makechan
//     type must be a chan, got named" — the factory checked the type's
//     own entry instead of its underlying type;
//   - channel ==/!=: "illegal binary op/kind combination: EQ result=BOOL
//     operand=CHAN" — channels are comparable (equal iff made by the same
//     make, or both nil);
//   - a concrete value sent on a chan of interface type was not boxed
//     (struct0.go, convT2X.go: ClassCastException GoStruct/Long ->
//     GoInterface at the receiver's type assertion).
//
// Run under gc with `go test`, under graalgo with `graalgo test ./dir`.
package chansemantics

import "testing"

type C chan int

func TestMakeNamedChan(t *testing.T) {
	c := make(C, 3)
	c <- 7
	if len(c) != 1 || cap(c) != 3 {
		t.Fatalf("len/cap = %d/%d, want 1/3", len(c), cap(c))
	}
	if v := <-c; v != 7 {
		t.Fatalf("recv %d, want 7", v)
	}
}

func TestChanEquality(t *testing.T) {
	a := make(chan int)
	b := make(chan int)
	a2 := a
	var n1, n2 chan int
	if !(a == a2) || a != a2 {
		t.Error("a channel must equal itself")
	}
	if a == b || !(a != b) {
		t.Error("distinct makes must differ")
	}
	if !(n1 == n2) || n1 != nil || a == nil {
		t.Error("nil channel equality")
	}
}

type pt struct{ x, y int }

func TestSendBoxesToElemInterface(t *testing.T) {
	c := make(chan any, 4)
	c <- struct{}{}
	c <- pt{1, 2}
	c <- 42
	c <- int8(-3)
	if _, ok := (<-c).(struct{}); !ok {
		t.Error("struct{} lost its dynamic type")
	}
	if v, ok := (<-c).(pt); !ok || v.y != 2 {
		t.Errorf("pt: %v %v", v, ok)
	}
	if v, ok := (<-c).(int); !ok || v != 42 {
		t.Errorf("untyped const sent on chan any must be int: %v %v", v, ok)
	}
	if v, ok := (<-c).(int8); !ok || v != -3 {
		t.Errorf("int8: %v %v", v, ok)
	}
}

func TestSelectSendBoxes(t *testing.T) {
	c := make(chan any, 1)
	select {
	case c <- pt{3, 4}:
	}
	if v, ok := (<-c).(pt); !ok || v.x != 3 {
		t.Errorf("select send: %v %v", v, ok)
	}
}

func TestSendConvertsToNarrowElem(t *testing.T) {
	c := make(chan int8, 2)
	c <- 100
	c <- -128
	if a, b := <-c, <-c; a != 100 || b != -128 {
		t.Errorf("int8 chan: %d %d", a, b)
	}
	var e error = nil
	ce := make(chan error, 1)
	ce <- e
	if <-ce != nil {
		t.Error("nil error")
	}
}
