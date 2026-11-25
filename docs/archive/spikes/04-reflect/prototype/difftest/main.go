// difftest dumps the layout-core differential battery as a human-readable
// table: type, gc size/align/offsets vs layout-core computed values.
// Exit non-zero on any mismatch. Output is archived to logs/difftest.log.
package main

import (
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"unsafe"

	"graalgo-spike04/layout"
)

type tEmpty struct{}
type tPad1 struct {
	a int8
	b int64
	c int8
}
type tPad2 struct {
	a int64
	b int8
}
type tNested struct {
	a tPad1
	b int16
	c string
}
type tEmbed struct {
	tPad1
	x float64
}
type tTrailZero struct {
	a [4]byte
	z struct{}
}
type tTrailZeroAlign8 struct {
	a int64
	z struct{}
}
type tLeadZero struct {
	z struct{}
	a int64
}
type tAllZero struct {
	a struct{}
	b [0]int
	c struct{}
}
type tHeaders struct {
	s  string
	sl []byte
	m  map[string]int
	ch chan int
	f  func()
	p  *int
	u  unsafe.Pointer
	i  any
	e  error
}
type tAtomic struct {
	i atomic.Int64
	x int8
}
type tRec struct {
	next *tRec
	v    int64
}

func battery() []reflect.Type {
	return []reflect.Type{
		reflect.TypeOf(false),
		reflect.TypeOf(int8(0)), reflect.TypeOf(int64(0)),
		reflect.TypeOf(uint32(0)), reflect.TypeOf(uintptr(0)),
		reflect.TypeOf(float32(0)), reflect.TypeOf(complex128(0)),
		reflect.TypeOf(""), reflect.TypeOf([]byte(nil)),
		reflect.TypeOf(map[string]int(nil)), reflect.TypeOf(chan int(nil)),
		reflect.TypeOf(func() {}), reflect.TypeOf((*int)(nil)),
		reflect.TypeOf((*any)(nil)).Elem(),
		reflect.TypeOf([0]int{}), reflect.TypeOf([4]int32{}),
		reflect.TypeOf([3][2]int16{}), reflect.TypeOf([5]struct{}{}),
		reflect.TypeOf([2][0]int{}), reflect.TypeOf([1000]byte{}),
		reflect.TypeOf(tEmpty{}), reflect.TypeOf(tPad1{}),
		reflect.TypeOf(tPad2{}), reflect.TypeOf(tNested{}),
		reflect.TypeOf(tEmbed{}), reflect.TypeOf(tTrailZero{}),
		reflect.TypeOf(tTrailZeroAlign8{}), reflect.TypeOf(tLeadZero{}),
		reflect.TypeOf(tAllZero{}), reflect.TypeOf(tHeaders{}),
		reflect.TypeOf(tAtomic{}), reflect.TypeOf(tRec{}),
	}
}

func main() {
	bad := 0
	for _, rt := range battery() {
		gs, ms := rt.Size(), layout.SizeOf(rt)
		ga, ma := uintptr(rt.Align()), layout.AlignOf(rt)
		status := "OK"
		if gs != ms || ga != ma {
			status = "MISMATCH"
			bad++
		}
		fmt.Printf("%-40s gc(size=%3d align=%d) mine(size=%3d align=%d) %s\n",
			rt.String(), gs, ga, ms, ma, status)
		if rt.Kind() == reflect.Struct {
			mo := layout.FieldOffsets(rt)
			for i := 0; i < rt.NumField(); i++ {
				f := rt.Field(i)
				fs := "OK"
				if f.Offset != mo[i] {
					fs = "MISMATCH"
					bad++
				}
				fmt.Printf("    .%-24s gc(off=%3d) mine(off=%3d) %s\n",
					f.Name, f.Offset, mo[i], fs)
			}
		}
	}
	fmt.Printf("types=%d mismatches=%d\n", len(battery()), bad)
	if bad != 0 {
		os.Exit(1)
	}
}
