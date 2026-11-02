// Tests pinning the veto fix-up for the P1 union mislabel: union and
// type-set elements in embedded interfaces must never be described as
// "basic" types, and `complete` must report IsMethodSet semantics —
// false for constraint interfaces even after successful checking.
//
// Like the other emit tests, these assert specific type-table entries,
// not just "emission succeeded": the old fallback produced a
// plausible-looking basic entry whose only trace of the union was the
// display string, which a kind-dispatching backend would misread as a
// scalar.
package emit

import (
	"strings"
	"testing"
)

// ifaceOfGlobal resolves a globaltype node's type id through its Named
// entry to the underlying interface entry.
func ifaceOfGlobal(t *testing.T, doc *Document, global map[string]any) map[string]any {
	t.Helper()
	named := typeEntry(t, doc, nodeFields(t, global)["type"])
	if named["kind"] != "named" {
		t.Fatalf("globaltype type kind = %v, want named", named["kind"])
	}
	iface := typeEntry(t, doc, named["detail"].(map[string]any)["underlying"])
	if iface["kind"] != "interface" {
		t.Fatalf("named underlying kind = %v, want interface", iface["kind"])
	}
	return iface
}

// unionTerms resolves an interface embedded entry id to its term list,
// failing unless the entry is a union.
func unionTerms(t *testing.T, doc *Document, id any) []any {
	t.Helper()
	entry := typeEntry(t, doc, id)
	if entry["kind"] != "union" {
		t.Fatalf("embedded type kind = %v, want union", entry["kind"])
	}
	terms, ok := entry["detail"].(map[string]any)["terms"].([]any)
	if !ok {
		t.Fatalf("union has no terms list: %#v", entry)
	}
	return terms
}

// basicName resolves a type id to its basic name, failing unless the
// entry is a basic type.
func basicName(t *testing.T, doc *Document, id any) string {
	t.Helper()
	entry := typeEntry(t, doc, id)
	if entry["kind"] != "basic" {
		t.Fatalf("type kind = %v, want basic", entry["kind"])
	}
	name, _ := entry["detail"].(map[string]any)["name"].(string)
	return name
}

func TestInterfaceTypeSets(t *testing.T) {
	doc := emitDoc(t, `package demo

type C interface { ~int | ~string }
type D interface { int | string }
type Plain interface { M() }
type Mixed interface { C; M() }
type A = int
type G[T ~int] struct { V T }

func main() {}
`)
	types := globalsByKind(t, doc, "globaltype")
	if len(types) != 6 {
		t.Fatalf("found %d globaltype nodes, want 6", len(types))
	}

	// `~int | ~string`: one embedded union, both terms tilded.
	cIface := ifaceOfGlobal(t, doc, types[0])
	cDetail := cIface["detail"].(map[string]any)
	if cDetail["complete"] != false {
		t.Fatalf("C complete = %v, want false (constraint interface)", cDetail["complete"])
	}
	cEmbedded, ok := cDetail["embedded"].([]any)
	if !ok || len(cEmbedded) != 1 {
		t.Fatalf("C embedded has %d entries, want 1", len(cEmbedded))
	}
	cTerms := unionTerms(t, doc, cEmbedded[0])
	if len(cTerms) != 2 {
		t.Fatalf("C union has %d terms, want 2", len(cTerms))
	}
	wantCTerms := []struct {
		name  string
		tilde bool
	}{{"int", true}, {"string", true}}
	for i, want := range wantCTerms {
		term := cTerms[i].(map[string]any)
		if basicName(t, doc, term["type"]) != want.name || term["tilde"] != want.tilde {
			t.Fatalf("C term %d = (%v, tilde %v), want (%s, tilde %v)",
				i, term["type"], term["tilde"], want.name, want.tilde)
		}
	}

	// `int | string`: same shape, no tildes.
	dIface := ifaceOfGlobal(t, doc, types[1])
	dDetail := dIface["detail"].(map[string]any)
	if dDetail["complete"] != false {
		t.Fatalf("D complete = %v, want false (constraint interface)", dDetail["complete"])
	}
	dEmbedded := dDetail["embedded"].([]any)
	dTerms := unionTerms(t, doc, dEmbedded[0])
	if len(dTerms) != 2 {
		t.Fatalf("D union has %d terms, want 2", len(dTerms))
	}
	for i, want := range []string{"int", "string"} {
		term := dTerms[i].(map[string]any)
		if basicName(t, doc, term["type"]) != want || term["tilde"] != false {
			t.Fatalf("D term %d = (%v, tilde %v), want (%s, tilde false)",
				i, term["type"], term["tilde"], want)
		}
	}

	// Plain method-only interface: complete, methods are the contract.
	pIface := ifaceOfGlobal(t, doc, types[2])
	pDetail := pIface["detail"].(map[string]any)
	if pDetail["complete"] != true {
		t.Fatalf("Plain complete = %v, want true", pDetail["complete"])
	}
	if len(pDetail["methods"].([]any)) != 1 {
		t.Fatalf("Plain methods has %d entries, want 1", len(pDetail["methods"].([]any)))
	}
	if len(pDetail["embedded"].([]any)) != 0 {
		t.Fatalf("Plain embedded = %v, want empty", pDetail["embedded"])
	}

	// Mixed: a method AND an embedded constraint — still incomplete.
	// This is the case the old comment got wrong: complete:false here
	// does not mean "under construction", it means the method list is
	// not the whole contract (the embedded union restricts it).
	mIface := ifaceOfGlobal(t, doc, types[3])
	mDetail := mIface["detail"].(map[string]any)
	if mDetail["complete"] != false {
		t.Fatalf("Mixed complete = %v, want false (embeds constraint C)", mDetail["complete"])
	}
	if len(mDetail["methods"].([]any)) != 1 {
		t.Fatalf("Mixed methods has %d entries, want 1", len(mDetail["methods"].([]any)))
	}
	mEmbedded := mDetail["embedded"].([]any)
	if len(mEmbedded) != 1 {
		t.Fatalf("Mixed embedded has %d entries, want 1", len(mEmbedded))
	}
	if typeEntry(t, doc, mEmbedded[0])["kind"] != "named" {
		t.Fatalf("Mixed embedded kind = %v, want named (C)", typeEntry(t, doc, mEmbedded[0])["kind"])
	}

	// Alias: its own kind with an RHS, not a basic int.
	if nodeFields(t, types[4])["alias"] != true {
		t.Fatalf("A alias = %v, want true", nodeFields(t, types[4])["alias"])
	}
	alias := typeEntry(t, doc, nodeFields(t, types[4])["type"])
	if alias["kind"] != "alias" {
		t.Fatalf("A type kind = %v, want alias", alias["kind"])
	}
	aliasDetail := alias["detail"].(map[string]any)
	if aliasDetail["name"] != "A" {
		t.Fatalf("alias name = %v, want A", aliasDetail["name"])
	}
	if basicName(t, doc, aliasDetail["rhs"]) != "int" {
		t.Fatalf("alias rhs is not basic int: %#v", aliasDetail["rhs"])
	}

	// Type parameter as a struct field type: kind typeparam with an
	// interface constraint whose embedded element is a tilded union.
	gNamed := typeEntry(t, doc, nodeFields(t, types[5])["type"])
	gStruct := typeEntry(t, doc, gNamed["detail"].(map[string]any)["underlying"])
	if gStruct["kind"] != "struct" {
		t.Fatalf("G underlying kind = %v, want struct", gStruct["kind"])
	}
	fields := gStruct["detail"].(map[string]any)["fields"].([]any)
	if len(fields) != 1 {
		t.Fatalf("G has %d fields, want 1", len(fields))
	}
	tp := typeEntry(t, doc, fields[0].(map[string]any)["type"])
	if tp["kind"] != "typeparam" {
		t.Fatalf("G field V type kind = %v, want typeparam", tp["kind"])
	}
	tpDetail := tp["detail"].(map[string]any)
	if tpDetail["name"] != "T" {
		t.Fatalf("typeparam name = %v, want T", tpDetail["name"])
	}
	constraint := typeEntry(t, doc, tpDetail["constraint"])
	if constraint["kind"] != "interface" {
		t.Fatalf("T constraint kind = %v, want interface", constraint["kind"])
	}
	cEmbedded2 := constraint["detail"].(map[string]any)["embedded"].([]any)
	if len(cEmbedded2) != 1 {
		t.Fatalf("T constraint embedded has %d entries, want 1", len(cEmbedded2))
	}
	tTerms := unionTerms(t, doc, cEmbedded2[0])
	if len(tTerms) != 1 {
		t.Fatalf("T constraint union has %d terms, want 1", len(tTerms))
	}
	tTerm := tTerms[0].(map[string]any)
	if basicName(t, doc, tTerm["type"]) != "int" || tTerm["tilde"] != true {
		t.Fatalf("T constraint term = (%v, tilde %v), want (int, tilde true)",
			tTerm["type"], tTerm["tilde"])
	}

	// Sweep: no basic entry may carry a union display string. The old
	// fallback's signature was kind basic + a name like "~int | ~string";
	// any surviving instance means some type still falls through.
	for i, ty := range doc.Types {
		m, ok := ty.(map[string]any)
		if !ok || m["kind"] != "basic" {
			continue
		}
		name, _ := m["detail"].(map[string]any)["name"].(string)
		if strings.Contains(name, "|") {
			t.Fatalf("type %d is a basic entry with union name %q", i, name)
		}
	}
}
