package main

// Fix-hoist-funcfieldcall (Option A): a multi-result function-field call
// in return position (return h.f(x)) must desugar through hoistMRCallArg
// to a regular "call" node with the pair type — the same shape
// desugarFuncFieldCall already emits for the assignment path. The
// backend never implemented a dedicated field-call node kind, so any
// such node in the wire is a hard AST-build failure downstream
// (diagnosed via crypto/tls ExportKeyingMaterial: return cs.ekm(...)).
// This test pins the general mechanism, not any one package: it fails
// on the pre-fix tree (a dedicated field-call node in the wire) and
// passes after.

import (
	"strings"
	"testing"
)

const funcFieldHoistSrc = `package main

type holder struct {
	f func(int) (int, error)
}

func (h holder) get(x int) (int, error) {
	return h.f(x)
}

func main() {
	h := holder{f: func(x int) (int, error) { return x, nil }}
	a, err := h.get(1)
	println(a, err)
}
`

// TestFuncFieldHoistEmitsRegularCall: the return-position func-field
// call hoists to a pair temp initialized by a regular "call" node whose
// fun is the field-access selector, and no dedicated field-call node
// kind appears anywhere in the wire.
func TestFuncFieldHoistEmitsRegularCall(t *testing.T) {
	out, err := ExportFile(writeIfaceProg(t, funcFieldHoistSrc))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	if bad := findAllKind(doc, "funcfieldcall"); len(bad) != 0 {
		t.Fatalf("wire contains %d dedicated field-call node(s), want 0", len(bad))
	}
	// Find the get method's funcdecl and its hoisted pair temp.
	var getFn map[string]any
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if name, _ := fl["name"].(string); strings.Contains(name, "get") {
			getFn = fm
			break
		}
	}
	if getFn == nil {
		names := []string{}
		for _, f := range doc["funcs"].([]any) {
			fm, _ := f.(map[string]any)
			fl, _ := fm["fields"].(map[string]any)
			if name, _ := fl["name"].(string); name != "" {
				names = append(names, name)
			}
		}
		t.Fatalf("no get funcdecl in wire; funcs: %v", names)
	}
	found := false
	for _, n := range findAllKind(getFn, "vardecl") {
		fl, _ := n["fields"].(map[string]any)
		vals, _ := fl["values"].([]any)
		if len(vals) != 1 {
			continue
		}
		call, _ := vals[0].(map[string]any)
		if call == nil || call["kind"] != "call" {
			continue
		}
		cf, _ := call["fields"].(map[string]any)
		fun, _ := cf["fun"].(map[string]any)
		if fun != nil && fun["kind"] == "selector" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("get body has no pair temp initialized by a call-with-selector-fun node")
	}
}
