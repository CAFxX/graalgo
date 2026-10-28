package main

// Regression tests for variadic packing on the multi-result helper
// path (fix-redmaster-r40).
//
// A call to a variadic multi-result function (fmt.Println,
// fmt.Fprintln, ...) desugars to a call to its pair-returning
// __-helper, with arguments built by multiResultHelperArgs. That path
// exported the arguments bare, skipping the packVariadicArgs step the
// plain call path applies (exportCallArgsPacked). The backend binds
// call arguments positionally and never packs, so the helper's
// variadic slice parameter received the first element itself:
//
//	fmt.Fprintln(os.Stdout, 42)  bound a=42 (a Long), and
//	doPrintln's `for _, arg := range a` cast it to GoSlice.
//
// The fix applies packVariadicArgs in multiResultHelperArgs (tail-only
// for prependRecv plans). These tests assert on the exported wire:
// the helper call's trailing argument must be a complit packing the
// elements for a non-spread call, and the bare slice for a spread.

import (
	"encoding/json"
	"testing"
)

// mainFuncBody returns the body node of package main's main function
// in an exported wire document.
func mainFuncBody(t *testing.T, doc string) map[string]any {
	t.Helper()
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(doc), &raw); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	funcs, _ := raw["funcs"].([]any)
	for _, fv := range funcs {
		fm, _ := fv.(map[string]any)
		fields := nodeFields(t, fm)
		if fields["name"] == "main" && fields["pkgpath"] == "main" {
			body, _ := fields["body"].(map[string]any)
			if body == nil {
				t.Fatal("main has no body")
			}
			return body
		}
	}
	t.Fatal("no main.main in exported document")
	return nil
}

// helperCallIn returns the first call node under n whose fun is an
// ident with the given name, or nil.
func helperCallIn(t *testing.T, n map[string]any, name string) map[string]any {
	t.Helper()
	var found map[string]any
	var walk func(v any)
	walk = func(v any) {
		if found != nil {
			return
		}
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "call" {
				fun, _ := nodeFields(t, v)["fun"].(map[string]any)
				if fun != nil && fun["kind"] == "ident" {
					if nm, _ := nodeFields(t, fun)["name"].(string); nm == name {
						found = v
						return
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(n)
	return found
}

func callArgs(t *testing.T, call map[string]any) []any {
	t.Helper()
	args, _ := nodeFields(t, call)["args"].([]any)
	return args
}

func TestMRVariadicHelperCallPacksArgs(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	fmt.Println(42)
}
`
	doc := exportSrc(t, src)
	call := helperCallIn(t, mainFuncBody(t, doc), "__Println")
	if call == nil {
		t.Fatal("no __Println helper call in main")
	}
	args := callArgs(t, call)
	if len(args) != 1 {
		t.Fatalf("helper call has %d args, want 1 (the packed slice)", len(args))
	}
	arg, _ := args[0].(map[string]any)
	if arg["kind"] != "complit" {
		t.Fatalf("variadic arg kind = %v, want complit (packed); "+
			"an unpacked element binds into the slice parameter itself", arg["kind"])
	}
	vals, _ := nodeFields(t, arg)["values"].([]any)
	if len(vals) != 1 {
		t.Fatalf("packed complit has %d values, want 1", len(vals))
	}
}

func TestMRVariadicHelperCallPacksAfterFixedArgs(t *testing.T) {
	src := `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stdout, 1, 2)
}
`
	doc := exportSrc(t, src)
	call := helperCallIn(t, mainFuncBody(t, doc), "__Fprintln")
	if call == nil {
		t.Fatal("no __Fprintln helper call in main")
	}
	args := callArgs(t, call)
	if len(args) != 2 {
		t.Fatalf("helper call has %d args, want 2 (w, packed slice)", len(args))
	}
	arg, _ := args[1].(map[string]any)
	if arg["kind"] != "complit" {
		t.Fatalf("variadic arg kind = %v, want complit (packed)", arg["kind"])
	}
	vals, _ := nodeFields(t, arg)["values"].([]any)
	if len(vals) != 2 {
		t.Fatalf("packed complit has %d values, want 2", len(vals))
	}
}

func TestMRVariadicHelperCallSpreadNotPacked(t *testing.T) {
	// Negative: a spread call must pass its slice bare — packing it
	// would wrap the slice as a single element.
	src := `package main

import "fmt"

func main() {
	xs := []any{1, 2}
	fmt.Println(xs...)
}
`
	doc := exportSrc(t, src)
	call := helperCallIn(t, mainFuncBody(t, doc), "__Println")
	if call == nil {
		t.Fatal("no __Println helper call in main")
	}
	args := callArgs(t, call)
	if len(args) != 1 {
		t.Fatalf("helper call has %d args, want 1 (the spread slice)", len(args))
	}
	arg, _ := args[0].(map[string]any)
	if arg["kind"] == "complit" {
		t.Fatal("spread call was packed into a complit (slice wrapped as one element)")
	}
}
