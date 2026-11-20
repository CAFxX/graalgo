// Fatal: unrecovered panic of a named float32 type holding negative zero
// computed at run time; gc prints "panic: main.temp(-0)": the type name
// and the float in the runtime printer's shortest format. Warm-ups recover
// named float/bool/int/complex values and check their dynamic types.
package main

import (
	"math"
	"strconv"
)

type temp float32
type ok bool
type level int8
type wave complex128

//go:noinline
func chill(t temp) temp { return t * -1 }

func catch(f func()) (v any) {
	defer func() { v = recover() }()
	f()
	return nil
}

func kind(v any) string {
	switch x := v.(type) {
	case temp:
		return "temp " + strconv.FormatFloat(float64(x), 'g', -1, 32)
	case float32:
		return "float32"
	case ok:
		return "ok " + strconv.FormatBool(bool(x))
	case level:
		return "level " + strconv.Itoa(int(x))
	case wave:
		return "wave " + strconv.FormatComplex(complex128(x), 'g', -1, 128)
	}
	return "other"
}

func main() {
	var ts []temp
	for _, v := range []float32{1.5, -2.25, 0} {
		ts = append(ts, chill(temp(v)))
	}
	for _, t := range ts {
		println("t", float32(t), math.Signbit(float64(t)))
	}
	println(kind(catch(func() { panic(ts[0]) })))
	println(kind(catch(func() { panic(float32(ts[1])) })))
	println(kind(catch(func() { panic(ok(ts[2] == 0)) })))
	println(kind(catch(func() { panic(level(-128)) })))
	println(kind(catch(func() { panic(wave(complex(math.Inf(-1), float64(ts[2])))) })))
	got := catch(func() { panic(ts[2]) })
	println("signbit kept", math.Signbit(float64(got.(temp))), got == any(temp(0)))
	panic(ts[2])
}
