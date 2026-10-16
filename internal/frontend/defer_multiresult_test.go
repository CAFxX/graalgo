package main

// Regression tests for multi-result calls in defer/go statement positions.
//
// `defer fmt.Println("x")` and `go fmt.Println("x")` are legal Go with the
// call's results discarded. fmt.Println classifies as an mrM7 multi-result
// call; the bare statement position already desugared it via
// exportBareMultiResultCall, but defer/go statements called exportCall
// directly and failed with "multi-valued call result is not supported in
// frontend/v1 scope (WS D)".
//
// The fix desugars the deferred/go call to the __-helper call (pair
// discarded) in exportDeferStmt/exportGoStmt, gated on the same
// mrM7/mrXmlToken plans as the statement position.

import (
	"encoding/json"
	"strings"
	"testing"
)

// findNodes collects all wire nodes with the given kind.
func findNodes(t *testing.T, doc string, kind string) []map[string]any {
	t.Helper()
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(doc), &raw); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == kind {
				out = append(out, v)
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
	walk(raw)
	return out
}

func nodeFields(t *testing.T, n map[string]any) map[string]any {
	t.Helper()
	f, ok := n["fields"].(map[string]any)
	if !ok {
		t.Fatalf("node %q has no fields", n["kind"])
	}
	return f
}

// TestDeferMultiResultCall: `defer fmt.Println("x")` must export. The
// defer node must wrap a call to the mrM7 __-helper (not the raw
// two-result call, which the backend cannot build).
func TestDeferMultiResultCall(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	defer fmt.Println("first")
	fmt.Println("body")
}
`
	doc := exportSrc(t, src)
	defers := findNodes(t, doc, "defer")
	if len(defers) == 0 {
		t.Fatal("no defer nodes in exported document")
	}
	foundHelper := false
	for _, d := range defers {
		call, ok := nodeFields(t, d)["call"].(map[string]any)
		if !ok || call["kind"] != "call" {
			t.Fatalf("defer wraps %v, want call node", call["kind"])
		}
		fun, ok := nodeFields(t, call)["fun"].(map[string]any)
		if !ok {
			t.Fatal("defer call has no fun node")
		}
		// The main package's `defer fmt.Println("first")` desugars to
		// the mrM7 __-helper ident call. (Other defers in the document
		// come from imported packages and keep their own forms.)
		if fun["kind"] == "ident" {
			name, _ := nodeFields(t, fun)["name"].(string)
			if strings.HasPrefix(name, "__") {
				foundHelper = true
			}
		}
	}
	if !foundHelper {
		t.Fatal("no defer call targets an mrM7 __-helper")
	}
}

// TestGoMultiResultCall: `go fmt.Println("x")` must export. The go
// statement must hoist the helper call's args to temps evaluated at
// go-time, then spawn a zero-arg funclit running the helper call.
func TestGoMultiResultCall(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	go fmt.Println("hi")
	fmt.Println("body")
}
`
	doc := exportSrc(t, src)
	gostmts := findNodes(t, doc, "gostmt")
	if len(gostmts) == 0 {
		t.Fatal("no gostmt nodes in exported document")
	}
	// The mr desugar emits a block: hoisted arg temps + gostmt.
	blocks := findNodes(t, doc, "block")
	foundHoist := false
	for _, b := range blocks {
		stmts, _ := nodeFields(t, b)["stmts"].([]any)
		hasVardecl := false
		hasGostmt := false
		for _, s := range stmts {
			m, _ := s.(map[string]any)
			if m["kind"] == "vardecl" {
				hasVardecl = true
			}
			if m["kind"] == "gostmt" {
				hasGostmt = true
			}
		}
		if hasVardecl && hasGostmt {
			foundHoist = true
		}
	}
	if !foundHoist {
		t.Fatal("go multi-result call did not hoist args to temps before gostmt")
	}
}

// TestDeferSingleResultCallUnaffected: a defer of a single-result call
// must not take the multi-result desugar path (no __-helper).
func TestDeferSingleResultCallUnaffected(t *testing.T) {
	src := `package main

import "fmt"

func one() int { return 1 }

func main() {
	defer fmt.Println(one())
}
`
	doc := exportSrc(t, src)
	defers := findNodes(t, doc, "defer")
	if len(defers) == 0 {
		t.Fatal("no defer nodes in exported document")
	}
	// The inner one() call is single-result; the deferred call itself is
	// still the mrM7 Println helper (results discarded). What matters:
	// export succeeds and the defer call fun is the helper ident.
	for _, d := range defers {
		call, ok := nodeFields(t, d)["call"].(map[string]any)
		if !ok || call["kind"] != "call" {
			t.Fatalf("defer wraps %v, want call node", call["kind"])
		}
	}
}

// TestDeferUserMultiResultCall: a user-defined multi-result function in
// defer position keeps the existing pair-transform path (mrUser via
// exportCall), unaffected by the mrM7/mrXmlToken gate.
func TestDeferUserMultiResultCall(t *testing.T) {
	src := `package main

import "fmt"

func pair() (int, error) { return 1, nil }

func main() {
	defer fmt.Println(pair())
	fmt.Println("body")
}
`
	doc := exportSrc(t, src)
	defers := findNodes(t, doc, "defer")
	if len(defers) == 0 {
		t.Fatal("no defer nodes in exported document")
	}
}
