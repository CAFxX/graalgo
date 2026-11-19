package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSyntheticOsExpand verifies the REVIEW-2026-11-18-14 O-P1-1 fix:
// os.Expand / os.ExpandEnv are real (gc-verbatim) implementations in the
// synthetic os package, driven by the single-result __getShellName helper
// — not the fail-fast panics they replaced at base a0df481b:
//
//	panic("os.Expand: not in M8 scope (needs multi-result getShellName; re-test: WS D)")
//	panic("os.getShellName: not in M8 scope (multi-result; re-test: WS D)")
//
// Behavioral reference: gc 1.27.1 os/env_test.go expandTests (18 cases,
// verbatim — "", "$*", "$$", "${*}", "$1", "${1}", "now is the time",
// "$HOME", "$home_1", "${HOME}", "${H}OME", "A$$$#$1$H$home_1*B",
// "start$+middle$^end$", "mixed$|bag$$$", "$", "$}", "${", "${}"),
// plus TestExpandEnv. The independent review of tip 4123acc8 ran a
// 51-case differential against gc 1.27.1 (those 18 gc cases + 28 extra
// edge cases + 5 ExpandEnv cases, 0 failures); the assertions below are
// the committed regression surface for that behavior at the export layer
// (frontend tests cannot execute synthetic functions — execution needs
// the backend — so the wire doc is the right surface here, as in
// TestSyntheticBytesBufferReadDesugar).
//
// Failing-capable: restoring the base panic bodies fails this test three
// ways — no __getShellName funcdecl is emitted, the Expand body no longer
// calls __getShellName, and the panic string "not in M8 scope" reappears
// in the Expand body.
func TestSyntheticOsExpand(t *testing.T) {
	src := `package main

import "os"

func main() {
	println(os.Expand("x${HOME}y", func(s string) string { return s }))
	println(os.ExpandEnv("a$HOME"))
}
`
	doc := decodeExport(t, mustExport(t, src))

	// Collect the os-pkgpath funcdecls by name.
	decls := map[string]map[string]any{}
	for _, f := range findAllKind(doc, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		name, _ := fl["name"].(string)
		pp, _ := fl["pkgpath"].(string)
		if pp == "os" {
			decls[name] = fl
		}
	}

	// 1. The __getShellName helper funcdecl is emitted (single-result).
	helper, ok := decls["__getShellName"]
	if !ok {
		t.Fatal("wire doc missing funcdecl __getShellName in pkgpath \"os\"")
	}
	if params, _ := helper["params"].([]any); len(params) != 1 {
		t.Fatalf("__getShellName params = %d, want 1 (string)", len(params))
	}
	if results, _ := helper["results"].([]any); len(results) != 1 {
		t.Fatalf("__getShellName results = %d, want 1 (shellNamePair)", len(results))
	}
	if _, hasBody := helper["body"]; !hasBody {
		t.Fatal("__getShellName funcdecl has no body")
	}

	// 2. Expand and ExpandEnv are present in pkgpath "os" with
	// single-result signatures: Expand(string, func(string) string) string
	// and ExpandEnv(string) string.
	expand, ok := decls["Expand"]
	if !ok {
		t.Fatal("wire doc missing funcdecl Expand in pkgpath \"os\"")
	}
	expandEnv, ok := decls["ExpandEnv"]
	if !ok {
		t.Fatal("wire doc missing funcdecl ExpandEnv in pkgpath \"os\"")
	}
	assertStringParam := func(f map[string]any, i int, what string) float64 {
		t.Helper()
		params, _ := f["params"].([]any)
		if len(params) <= i {
			t.Fatalf("%s has %d params, want param %d", what, len(params), i)
		}
		pm, _ := params[i].(map[string]any)
		pf, _ := pm["fields"].(map[string]any)
		tid, _ := pf["type"].(float64)
		if !osExpandTypeIsString(t, doc, int(tid)) {
			t.Fatalf("%s param %d is not string (type id %d)", what, i, int(tid))
		}
		return tid
	}
	assertStringResult := func(f map[string]any, what string) {
		t.Helper()
		results, _ := f["results"].([]any)
		if len(results) != 1 {
			t.Fatalf("%s results = %d, want 1 (string)", what, len(results))
		}
		rm, _ := results[0].(map[string]any)
		rf, _ := rm["fields"].(map[string]any)
		tid, _ := rf["type"].(float64)
		if !osExpandTypeIsString(t, doc, int(tid)) {
			t.Fatalf("%s result is not string (type id %d)", what, int(tid))
		}
	}
	expandParams, _ := expand["params"].([]any)
	if len(expandParams) != 2 {
		t.Fatalf("Expand params = %d, want 2 (string, func(string) string)", len(expandParams))
	}
	assertStringParam(expand, 0, "Expand")
	mapping, _ := expandParams[1].(map[string]any)
	mappingFields, _ := mapping["fields"].(map[string]any)
	mappingTid, _ := mappingFields["type"].(float64)
	if !osExpandTypeIsStringFunc(t, doc, int(mappingTid)) {
		t.Fatalf("Expand param 1 is not func(string) string (type id %d)", int(mappingTid))
	}
	assertStringResult(expand, "Expand")
	if params, _ := expandEnv["params"].([]any); len(params) != 1 {
		t.Fatalf("ExpandEnv params = %d, want 1 (string)", len(params))
	}
	assertStringParam(expandEnv, 0, "ExpandEnv")
	assertStringResult(expandEnv, "ExpandEnv")

	// Expand calls the helper (the implementation is wired up, not just
	// emitted), and ExpandEnv calls Expand.
	if !osExpandBodyContains(t, expand, "__getShellName") {
		t.Fatal("Expand body does not call __getShellName")
	}
	if !osExpandBodyContains(t, expandEnv, `"name":"Expand"`) {
		t.Fatal("ExpandEnv body does not call Expand")
	}

	// 3. No "not in M8 scope" panic string remains in the Expand /
	// getShellName / ExpandEnv bodies — the base fail-fast bodies are
	// gone. Scoped to these three funcdecls: other os members (e.g.
	// Chown, OpenRoot) still legitimately carry M8 fail-fast panics.
	for _, name := range []string{"Expand", "ExpandEnv", "__getShellName"} {
		bodyJSON, _ := json.Marshal(decls[name]["body"])
		if strings.Contains(string(bodyJSON), "not in M8 scope") {
			t.Errorf("funcdecl %q body still contains %q (base fail-fast panic)", name, "not in M8 scope")
		}
	}
}

// osExpandTypeIsString reports whether the types-table entry with the
// given id is the basic string type.
func osExpandTypeIsString(t *testing.T, doc map[string]any, id int) bool {
	t.Helper()
	entries, _ := doc["types"].([]any)
	if id < 0 || id >= len(entries) {
		t.Fatalf("type id %d out of range (types table has %d entries)", id, len(entries))
	}
	em, _ := entries[id].(map[string]any)
	if em["kind"] != "basic" {
		return false
	}
	det, _ := em["detail"].(map[string]any)
	return det["name"] == "string"
}

// osExpandTypeIsStringFunc reports whether the types-table entry with the
// given id is the signature func(string) string: one string param, one
// string result, non-variadic.
func osExpandTypeIsStringFunc(t *testing.T, doc map[string]any, id int) bool {
	t.Helper()
	entries, _ := doc["types"].([]any)
	if id < 0 || id >= len(entries) {
		t.Fatalf("type id %d out of range (types table has %d entries)", id, len(entries))
	}
	em, _ := entries[id].(map[string]any)
	if em["kind"] != "signature" {
		return false
	}
	det, _ := em["detail"].(map[string]any)
	params, _ := det["params"].([]any)
	results, _ := det["results"].([]any)
	if len(params) != 1 || len(results) != 1 || det["variadic"] != false {
		return false
	}
	pm, _ := params[0].(map[string]any)
	rm, _ := results[0].(map[string]any)
	ptid, _ := pm["type"].(float64)
	rtid, _ := rm["type"].(float64)
	return osExpandTypeIsString(t, doc, int(ptid)) && osExpandTypeIsString(t, doc, int(rtid))
}

// osExpandBodyContains reports whether the marshalled body of a funcdecl
// contains the given substring.
func osExpandBodyContains(t *testing.T, f map[string]any, substr string) bool {
	t.Helper()
	bodyJSON, err := json.Marshal(f["body"])
	if err != nil {
		t.Fatalf("cannot marshal funcdecl body: %v", err)
	}
	return strings.Contains(string(bodyJSON), substr)
}
