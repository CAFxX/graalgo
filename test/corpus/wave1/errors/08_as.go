package main

import (
	"errors"
	"fmt"
	"reflect"
)

// er08: errors.As with reflect-driven target setup — exercises
// Value.Elem() on a pointer to a nil pointer (PKG-022).
//
// The stdlib errors_test.go TestAs does:
//   rtarget := reflect.ValueOf(tc.target)          // e.g. **errorT, target nil
//   rtarget.Elem().Set(reflect.Zero(...))          // must not panic
// Before the PKG-022 fix, Elem() on **T pointing to nil *T returned the
// zero Value, so Set panicked with "reflect: call of reflect.Value.Set
// on zero Value".

type errorT struct{ msg string }

func (e errorT) Error() string { return e.msg }

// wrapT is a simple error wrapper (avoids fmt.Errorf's internal types).
type wrapT struct {
	msg string
	err error
}

func (e wrapT) Error() string { return e.msg }
func (e wrapT) Unwrap() error { return e.err }

func main() {
	// Case 1: the exact TestAs setup pattern — **errorT to nil *errorT.
	var errP *errorT
	target := &errP
	rtarget := reflect.ValueOf(target)
	// This panicked pre-fix (Elem returned zero Value).
	rtarget.Elem().Set(reflect.Zero(reflect.TypeOf(target).Elem()))
	if errP != nil {
		panic("clear: errP should be nil")
	}
	fmt.Println("clear ok")

	// Case 2: errors.As finds the error and sets through Elem.
	inner := errorT{"inner"}
	wrapped := wrapT{"outer", inner}
	var target2 errorT
	if !errors.As(wrapped, &target2) {
		panic("As should match errorT")
	}
	if target2.msg != "inner" {
		panic("As should set target2")
	}
	fmt.Println("as-direct ok")

	// Case 3: errors.As with **errorT target (pointer chain through nil).
	innerP := &errorT{"innerP"}
	wrappedP := wrapT{"outerP", innerP}
	var errP2 *errorT
	if !errors.As(wrappedP, &errP2) {
		panic("As should match *errorT")
	}
	if errP2 == nil || errP2.msg != "innerP" {
		panic("As should set errP2")
	}
	fmt.Println("as-ptr ok")

	// Case 4: triple pointer chain — Elem at each level.
	var ppp **errorT
	pp2 := &ppp
	v := reflect.ValueOf(&pp2)
	e1 := v.Elem()
	if !e1.IsValid() {
		panic("***errorT Elem should be valid")
	}
	e2 := e1.Elem()
	if !e2.IsValid() {
		panic("**errorT Elem (nil pointee) should be valid")
	}
	if !e2.IsNil() {
		panic("**errorT Elem should be nil")
	}
	// e2 is nil *errorT: Elem on it must be zero Value (pointer itself nil).
	e3 := e2.Elem()
	if e3.IsValid() {
		panic("Elem on nil *errorT should be zero Value")
	}
	fmt.Println("ptr-chain ok")

	// Case 5: reflect.New on pointer type, then Elem and Set.
	ne := reflect.New(reflect.TypeOf(errP2))
	e := ne.Elem()
	if !e.IsValid() || !e.IsNil() || !e.CanSet() {
		panic("New(*errorT).Elem should be valid, nil, settable")
	}
	e.Set(reflect.ValueOf(&errorT{"set"}))
	if ne.Elem().IsNil() {
		panic("Set through New-Elem should stick")
	}
	fmt.Println("new-elem-set ok")

	// Case 6: double-Elem on a reflect.New pointer (PKG-022 VETO-1).
	// New(*errorT) is **errorT to a nil *errorT; the first Elem reuses
	// the New cell handle (denoting the cell content, the nil *errorT);
	// the second Elem must see the nil pointer and yield the zero Value
	// (gc) — not a bogus valid Value.
	ne2 := reflect.New(reflect.TypeOf(errP2))
	d1 := ne2.Elem()
	if !d1.IsValid() || !d1.IsNil() || !d1.CanSet() {
		panic("New(*errorT).Elem should be valid, nil, settable")
	}
	d2 := d1.Elem()
	if d2.IsValid() {
		panic("New(*errorT).Elem().Elem() should be the zero Value")
	}
	fmt.Println("new-double-elem ok")

	fmt.Println("PASS")
}
