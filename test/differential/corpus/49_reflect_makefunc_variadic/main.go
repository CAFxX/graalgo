// Variadic reflect.MakeFunc: the trampoline must hand the callback exactly
// NumIn() Values with the trailing args packed into the final []T slice
// (gc's makeFuncImpl contract) — for 0, 1 and N variadic args — plus a
// CallSlice spread round-trip and the too-few-args panic.
//
// Regression test: the trampoline used to index the formal parameter list
// by argument position, throwing a raw Java IndexOutOfBoundsException on
// any variadic call with trailing args.
package main

import (
	"fmt"
	"reflect"
	"strings"
)

func tryCall(name string, fn reflect.Value, args []reflect.Value) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%s: panic: %v\n", name, r)
		}
	}()
	out := fn.Call(args)
	fmt.Printf("%s: %v\n", name, out[0].Interface())
}

func main() {
	// func(prefix string, nums ...int) string — fixed param + variadic tail.
	ft := reflect.TypeOf(func(string, ...int) string { return "" })
	if !ft.IsVariadic() || ft.NumIn() != 2 {
		panic("bad func type")
	}
	fn := reflect.MakeFunc(ft, func(args []reflect.Value) []reflect.Value {
		if len(args) != ft.NumIn() {
			panic(fmt.Sprintf("callback: got %d args, want %d", len(args), ft.NumIn()))
		}
		prefix := args[0].Interface().(string)
		nums := args[1].Interface().([]int) // the packed variadic slice
		parts := make([]string, len(nums))
		for i, n := range nums {
			parts[i] = fmt.Sprint(n)
		}
		return []reflect.Value{reflect.ValueOf(prefix + "[" + strings.Join(parts, ",") + "]")}
	})
	v := reflect.ValueOf
	tryCall("zero", fn, []reflect.Value{v("z")})
	tryCall("one", fn, []reflect.Value{v("o"), v(7)})
	tryCall("many", fn, []reflect.Value{v("m"), v(1), v(2), v(3), v(4)})

	// Pure-variadic target: func(...string) int — no fixed params at all.
	ft2 := reflect.TypeOf(func(...string) int { return 0 })
	fn2 := reflect.MakeFunc(ft2, func(args []reflect.Value) []reflect.Value {
		if len(args) != 1 {
			panic(fmt.Sprintf("callback: got %d args, want 1", len(args)))
		}
		return []reflect.Value{reflect.ValueOf(len(args[0].Interface().([]string)))}
	})
	tryCall("p0", fn2, nil)
	tryCall("p1", fn2, []reflect.Value{v("a")})
	tryCall("p3", fn2, []reflect.Value{v("a"), v("b"), v("c")})

	// CallSlice: the final []string is spread by the call path and must be
	// re-packed into the variadic slice by the trampoline.
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("slice: panic: %v\n", r)
			}
		}()
		ft3 := reflect.TypeOf(func(string, ...string) string { return "" })
		fn3 := reflect.MakeFunc(ft3, func(args []reflect.Value) []reflect.Value {
			if len(args) != 2 {
				panic(fmt.Sprintf("callback: got %d args, want 2", len(args)))
			}
			return []reflect.Value{reflect.ValueOf(
				args[0].Interface().(string) + "=" +
					strings.Join(args[1].Interface().([]string), "+"))}
		})
		out := fn3.CallSlice([]reflect.Value{v("s"), v([]string{"x", "y"})})
		fmt.Printf("slice: %v\n", out[0].Interface())
	}()

	// Too few args: the fixed param is missing -> a Go panic with gc's
	// message, never a raw Java exception.
	tryCall("few", fn, []reflect.Value{})
}
