package main

// Tests for rev20261103-funcname (REVIEW-2026-11-03-00 N-P1-1):
// (*runtime.Func).Name()/Entry() in method-value position, and the
// deletion of the resolveMethodCall panicMsg marker.
//
// Previously the marker reached methodValueNode with a nil decl and
// crashed the exporter (nil dereference); direct calls were already real
// (exportCall's runtimeStackMethod interception → runtimefuncname /
// runtimefuncentry wire nodes, implemented by graalgo.rtstack).

import (
	"strings"
	"testing"
)

// TestRuntimeFuncNameMethodValueExport: (*Func).Name/Entry in method-value
// position eta-expand into a real closure around the intrinsic wire node.
// The deferred-evaluation shape is pinned: each method-value factory call
// node must RETURN a nested zero-arg funclit (the deferred value), and the
// intrinsic wire node must live inside that nested closure's body — never
// evaluated by the factory call itself. The pre-fix eager shape
// call(fun=funclit(params=[__mv_recv], body=return(wire))) binds the
// eagerly-computed string instead of a func() string and has no nested
// funclit at all, so this pin fails on it.
func TestRuntimeFuncNameMethodValueExport(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	_ = n
	fn := runtime.FuncForPC(pcs[0])
	if fn == nil {
		return
	}
	nameOf := fn.Name
	entryOf := fn.Entry
	name, entry := nameOf(), entryOf()
	_, _ = name, entry
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with (*runtime.Func).Name/Entry method values: %v", err)
	}
	for _, want := range []string{
		`"kind": "funclit"`,
		`"kind": "runtimefuncname"`,
		`"kind": "runtimefuncentry"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing %s", want)
		}
	}
	if strings.Contains(out, "not implemented") {
		t.Errorf("exported JSON still contains the retired panicMsg stub text")
	}
	mvCalls := 0
	for _, c := range findNodes(t, out, "call") {
		fun, ok := nodeFields(t, c)["fun"].(map[string]any)
		if !ok || fun["kind"] != "funclit" {
			continue
		}
		wireKind := ""
		for _, k := range []string{"runtimefuncname", "runtimefuncentry"} {
			if subtreeHasKind(fun, k) {
				wireKind = k
				break
			}
		}
		if wireKind == "" {
			continue
		}
		mvCalls++
		nested := nestedFunclits(fun)
		if len(nested) != 1 {
			t.Errorf("method-value call: factory funclit holds %d nested funclits, want 1 (the deferred closure)", len(nested))
			continue
		}
		closureFields := nodeFields(t, nested[0])
		params, ok := closureFields["params"].([]any)
		if !ok {
			t.Errorf("method-value closure has no params array in fields")
		} else if len(params) != 0 {
			t.Errorf("method-value closure has %d params, want 0 (deferred, receiver captured)", len(params))
		}
		inFactory := countSubtreeKind(fun, wireKind)
		inClosure := countSubtreeKind(nested[0], wireKind)
		if inClosure < 1 || inFactory != inClosure {
			t.Errorf("method-value call: %q wire nodes in factory=%d closure=%d, want all %d inside the deferred closure",
				wireKind, inFactory, inClosure, inFactory)
		}
	}
	if mvCalls != 2 {
		t.Errorf("found %d method-value factory call nodes, want 2 (Name + Entry)", mvCalls)
	}
}

// TestRuntimeFuncNameMethodValueMultiResult: the multi-result stack
// members (Frames.Next, Func.FileLine) in method-value position fail fast
// with the named WS D scope error — a method value would need the pair
// machinery, and a silent "not found" or a crash is not acceptable.
func TestRuntimeFuncNameMethodValueMultiResult(t *testing.T) {
	progs := []struct{ name, body string }{
		{"Next", `frames := runtime.CallersFrames(pcs[:n])
	_ = frames
	f := frames.Next
	_ = f`},
		{"FileLine", `fn := runtime.FuncForPC(pcs[0])
	_ = fn
	f := fn.FileLine
	_ = f`},
	}
	for _, p := range progs {
		src := "package main\n\nimport \"runtime\"\n\nfunc main() {\n\tvar pcs [8]uintptr\n\tn := runtime.Callers(1, pcs[:])\n\t_, _ = pcs, n\n\t" + p.body + "\n}\n"
		_, err := ExportFile(writeSyncProg(t, src))
		if err == nil {
			t.Errorf("method value %s: expected scope error, got success", p.name)
			continue
		}
		if !strings.Contains(err.Error(), "multi-result") || !strings.Contains(err.Error(), p.name) {
			t.Errorf("method value %s: error %q does not name the multi-result scope", p.name, err)
		}
	}
}

// TestRuntimeFuncNameGenericContext: stack-inspection calls and method
// values inside a generic template body must export once the template is
// instantiated. The findNested instantiation walk skips runtime stack
// methods (like the roots loop); without the skip, enumeration fails with
// "method Name not found on Func".
func TestRuntimeFuncNameGenericContext(t *testing.T) {
	src := `package main

import "runtime"

func probe[T any](v T, fn *runtime.Func) string {
	_ = v
	direct := fn.Name()
	nameOf := fn.Name
	_ = direct
	return nameOf()
}

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	fn := runtime.FuncForPC(pcs[0])
	_, _ = n, fn
	_ = probe[int](0, fn)
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with stack-inspection shapes in a generic body: %v", err)
	}
	for _, want := range []string{
		`"kind": "runtimefuncname"`,
		`"kind": "funclit"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing %s", want)
		}
	}
}

// subtreeHasKind reports whether any node in n's subtree (inclusive) has
// the given kind.
func subtreeHasKind(n map[string]any, kind string) bool {
	return countSubtreeKind(n, kind) > 0
}

// countSubtreeKind counts nodes with the given kind in n's subtree
// (inclusive).
func countSubtreeKind(n map[string]any, kind string) int {
	count := 0
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == kind {
				count++
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
	return count
}

// nestedFunclits returns the funclit nodes directly nested in n's subtree,
// excluding n itself.
func nestedFunclits(n map[string]any) []map[string]any {
	var out []map[string]any
	var walk func(v any, top bool)
	walk = func(v any, top bool) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funclit" && !top {
				out = append(out, v)
			}
			for _, c := range v {
				walk(c, false)
			}
		case []any:
			for _, c := range v {
				walk(c, false)
			}
		}
	}
	walk(n, true)
	return out
}
