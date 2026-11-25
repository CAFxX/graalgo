// Differential test: layout core vs gc ground truth (go1.27.1).
//
// For every type in the battery, compare SizeOf/AlignOf/field offsets
// computed from first principles (layout.go) against reflect's oracle
// values. Any mismatch fails the test. The battery is adversarial: padding,
// nested/embedded structs, zero-sized fields in every position (incl. the
// issue-9401 trailing case), zero-length arrays, and multi-word headers.
package layout

import (
	"reflect"
	"sync/atomic"
	"testing"
	"unsafe"
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
type tEmbedPtr struct {
	*tPad1
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
type tMidZeroArr struct {
	a int32
	z [0]int64
	b int32
}
type tTrailZero2 struct {
	a int64
	z [0]int64
}
type tLeadZero struct {
	z struct{}
	a int64
}
type tMidZero struct {
	a int8
	z struct{}
	b int64
}
type tAllZero struct {
	a struct{}
	b [0]int
	c struct{}
}
type tZeroArr struct {
	a int32
	z [0]func()
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
type tComplex struct {
	c64 complex64
	c128 complex128
	f32 float32
}
type tAtomic struct {
	i atomic.Int64
	x int8
}
type tRec struct {
	next *tRec
	v    int64
}
type tDeepNest struct {
	a struct {
		b struct {
			c int8
			d int64
		}
		e int16
	}
	f string
}
type tBlank struct {
	_ int32
	a int64
	_ [2]byte
}
type tUnexported struct {
	a int8
	b int64
}
type tArrInner struct {
	a [3][2]int16
	b [5]struct{}
	c [2][0]int
}

func battery() []reflect.Type {
	return []reflect.Type{
		reflect.TypeOf(false),
		reflect.TypeOf(int8(0)), reflect.TypeOf(int16(0)),
		reflect.TypeOf(int32(0)), reflect.TypeOf(int64(0)),
		reflect.TypeOf(uint8(0)), reflect.TypeOf(uint16(0)),
		reflect.TypeOf(uint32(0)), reflect.TypeOf(uint64(0)),
		reflect.TypeOf(int(0)), reflect.TypeOf(uint(0)),
		reflect.TypeOf(uintptr(0)),
		reflect.TypeOf(float32(0)), reflect.TypeOf(float64(0)),
		reflect.TypeOf(complex64(0)), reflect.TypeOf(complex128(0)),
		reflect.TypeOf(""),
		reflect.TypeOf([]byte(nil)),
		reflect.TypeOf(map[string]int(nil)),
		reflect.TypeOf(chan int(nil)),
		reflect.TypeOf(func() {}),
		reflect.TypeOf((*int)(nil)),
		reflect.TypeOf((*any)(nil)).Elem(),
		reflect.TypeOf((*error)(nil)).Elem(),
		reflect.TypeOf([0]int{}),
		reflect.TypeOf([1]byte{}),
		reflect.TypeOf([4]int32{}),
		reflect.TypeOf([3][2]int16{}),
		reflect.TypeOf([5]struct{}{}),
		reflect.TypeOf([2][0]int{}),
		reflect.TypeOf([1000]byte{}),
		reflect.TypeOf(tEmpty{}),
		reflect.TypeOf(tPad1{}),
		reflect.TypeOf(tPad2{}),
		reflect.TypeOf(tNested{}),
		reflect.TypeOf(tEmbed{}),
		reflect.TypeOf(tEmbedPtr{}),
		reflect.TypeOf(tTrailZero{}),
		reflect.TypeOf(tTrailZeroAlign8{}),
		reflect.TypeOf(tTrailZero2{}),
		reflect.TypeOf(tMidZeroArr{}),
		reflect.TypeOf(tLeadZero{}),
		reflect.TypeOf(tMidZero{}),
		reflect.TypeOf(tAllZero{}),
		reflect.TypeOf(tZeroArr{}),
		reflect.TypeOf(tComplex{}),
		reflect.TypeOf(tAtomic{}),
		reflect.TypeOf(tRec{}),
		reflect.TypeOf(tDeepNest{}),
		reflect.TypeOf(tBlank{}),
		reflect.TypeOf(tUnexported{}),
		reflect.TypeOf(tArrInner{}),
		reflect.TypeOf(struct {
			a int8
			b string
			c []int
			d map[int]int
			e any
			f func(int) int
			g *tPad1
			h [7]uint16
		}{}),
	}
}

func TestDifferential(t *testing.T) {
	for _, rt := range battery() {
		name := rt.String()
		if gs, ms := rt.Size(), SizeOf(rt); gs != ms {
			t.Errorf("%s: Size mismatch gc=%d mine=%d", name, gs, ms)
		}
		if ga, ma := uintptr(rt.Align()), AlignOf(rt); ga != ma {
			t.Errorf("%s: Align mismatch gc=%d mine=%d", name, ga, ma)
		}
		if rt.Kind() == reflect.Struct {
			mo := FieldOffsets(rt)
			for i := 0; i < rt.NumField(); i++ {
				if goff, moff := rt.Field(i).Offset, mo[i]; goff != moff {
					t.Errorf("%s.%s: Offset mismatch gc=%d mine=%d",
						name, rt.Field(i).Name, goff, moff)
				}
			}
		}
	}
}
