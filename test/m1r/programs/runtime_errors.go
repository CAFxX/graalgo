package main

// BUG-0046: the facade runtime declares upstream's *runtime.TypeAssertionError
// and *runtime.PanicNilError and the backend panics with them: recover()
// returns the Go types (type switch, errors.As through a wrapper, %T/%v),
// with gc's messages from upstream's Error methods over the descriptors of
// the types involved; panic(nil) follows GODEBUG panicnil, including changes
// of the environment variable at run time (gc's reparsedebugvars).

import (
	"errors"
	"fmt"
	"os"
	"runtime"
)

type T struct{}

func (T) M() {}

type U struct{}

func (U) M() {}

type Named interface{ M() }

func try(name string, f func()) {
	defer func() {
		r := recover()
		switch e := r.(type) {
		case *runtime.TypeAssertionError:
			var re runtime.Error
			var tae *runtime.TypeAssertionError
			wrapped := fmt.Errorf("wrapped: %w", e)
			println(name, "TypeAssertionError:", e.Error(), errors.As(e, &re), errors.As(wrapped, &tae), tae == e)
		case *runtime.PanicNilError:
			var re runtime.Error = e
			re.RuntimeError()
			println(name, "PanicNilError:", e.Error())
		case nil:
			println(name, "recovered nil")
		default:
			println(name, "other")
		}
		fmt.Printf("  %T | %v | %s\n", r, r, r)
	}()
	f()
}

func local() any {
	type L int
	return L(1)
}

func main() {
	var x interface{ M() } = T{}
	try("iface-to-concrete", func() { _ = x.(U) })
	var nx Named = T{}
	try("named-iface", func() { _ = nx.(U) })
	var y any = 3
	try("eface-to-concrete", func() { _ = y.(string) })
	try("missing-method", func() { _ = y.(fmt.Stringer) })
	var n any
	try("nil-to-concrete", func() { _ = n.(int) })
	try("nil-to-iface", func() { _ = n.(fmt.Stringer) })
	var z fmt.Stringer
	try("nil-iface-to-iface", func() { _ = z.(interface{ M() }) })
	type L int
	try("scopes", func() { _ = local().(L) })
	try("ptr", func() { _ = y.(*int) })
	if _, ok := y.(string); !ok {
		println("comma-ok does not panic")
	}

	try("panic-nil", func() { panic(nil) })
	var nilErr error
	try("panic-nil-error", func() { panic(nilErr) })
	os.Setenv("GODEBUG", "panicnil=1")
	try("env-panicnil=1", func() { panic(nil) })
	// The rightmost setting decides; an unparsable one keeps the current value.
	os.Setenv("GODEBUG", "panicnil=0,panicnil=bad")
	try("env-bad", func() { panic(nil) })
	os.Setenv("GODEBUG", "panicnil=1,panicnil=0")
	try("env-panicnil=0", func() { panic(nil) })
	os.Setenv("GODEBUG", "panicnil=1")
	os.Unsetenv("GODEBUG")
	try("env-unset", func() { panic(nil) })

	// Printed by the crash report through Error().
	_ = y.(fmt.Stringer)
}
