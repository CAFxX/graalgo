// Targets the dynamic type and value of recovered panic values: panic(nil)
// yielding *runtime.PanicNilError and its message, errors.Is/As through
// wrapped errors, custom error and Stringer types, named basic types,
// pointers to errors, and panicking with an interface holding nil.
package main

import (
	"errors"
	"runtime"
	"strconv"
)

type MyErr struct{ code int }

func (e *MyErr) Error() string { return "myerr " + strconv.Itoa(e.code) }

type ValErr string

func (v ValErr) Error() string { return "valerr:" + string(v) }

type Str struct{ s string }

func (s Str) String() string { return "stringer:" + s.s }

type MyInt int
type MyStr string

var sentinel = errors.New("sentinel")

type wrapped struct{ inner error }

func (w wrapped) Error() string { return "wrapped(" + w.inner.Error() + ")" }
func (w wrapped) Unwrap() error { return w.inner }

func catch(f func()) (r any) {
	defer func() { r = recover() }()
	f()
	return "no-panic"
}

func describe(r any) string {
	switch v := r.(type) {
	case nil:
		return "nil"
	case *runtime.PanicNilError:
		return "PanicNilError:" + v.Error()
	case runtime.Error:
		return "runtime.Error:" + v.Error()
	case error:
		return "error:" + v.Error()
	case Str:
		return "Str{" + v.s + "}"
	case MyInt:
		return "MyInt:" + strconv.Itoa(int(v))
	case MyStr:
		return "MyStr:" + string(v)
	case string:
		return "string:" + v
	case int:
		return "int:" + strconv.Itoa(v)
	case int8:
		return "int8:" + strconv.Itoa(int(v))
	case uint64:
		return "uint64:" + strconv.FormatUint(v, 10)
	case float64:
		return "float64:" + strconv.FormatFloat(v, 'g', -1, 64)
	case float32:
		return "float32:" + strconv.FormatFloat(float64(v), 'g', -1, 32)
	case complex128:
		return "complex128:" + strconv.FormatFloat(real(v), 'g', -1, 64) + "," + strconv.FormatFloat(imag(v), 'g', -1, 64)
	case bool:
		return "bool:" + strconv.FormatBool(v)
	case []int:
		return "slice len " + strconv.Itoa(len(v))
	}
	return "unknown"
}

func main() {
	println(describe(catch(func() { panic(nil) })))
	var e error
	println(describe(catch(func() { panic(e) })))
	var me *MyErr
	r := catch(func() { panic(me) })
	_, isPtr := r.(*MyErr)
	println("typed nil ptr error:", isPtr, r != nil)
	println(describe(catch(func() { panic(&MyErr{7}) })))
	println(describe(catch(func() { panic(ValErr("v")) })))
	println(describe(catch(func() { panic(Str{"s"}) })))
	println(describe(catch(func() { panic(MyInt(-3)) })))
	println(describe(catch(func() { panic(MyStr("ms")) })))
	println(describe(catch(func() { panic("plain") })))
	println(describe(catch(func() { panic(12) })))
	println(describe(catch(func() { panic(int8(-128)) })))
	println(describe(catch(func() { panic(uint64(1<<63 + 5)) })))
	println(describe(catch(func() { panic(1.25) })))
	println(describe(catch(func() { panic(float32(0.1)) })))
	println(describe(catch(func() { panic(complex(1, -2)) })))
	println(describe(catch(func() { panic(true) })))
	println(describe(catch(func() { panic([]int{1, 2}) })))
	println(describe(catch(func() {})))

	r = catch(func() { panic(wrapped{sentinel}) })
	err := r.(error)
	println("errors.Is sentinel:", errors.Is(err, sentinel))
	r = catch(func() { panic(wrapped{&MyErr{9}}) })
	var target *MyErr
	println("errors.As:", errors.As(r.(error), &target), target.code)

	r = catch(func() { panic(nil) })
	pn := r.(*runtime.PanicNilError)
	var asRT runtime.Error = pn
	println("PanicNilError is runtime.Error:", asRT != nil)

	r = catch(func() {
		var m map[int]int
		m[1] = 1
	})
	var rte runtime.Error
	println("errors.As runtime.Error:", errors.As(r.(error), &rte), rte.Error())
	println(describe(catch(func() { panic(errors.Join(sentinel, ValErr("j"))) })))
}
