package main

// P3-M8 (WS D): exporter pinning tests for multi-result interface method
// dispatch (D1). Red-green (observed by reviewer probe, not inferred —
// REVIEW-2 P2-4): on the pre-D1 tree (7e53780) these fail, but NOT all
// via scope errors. The exporter panics on synthetic import (nil deref
// in describeType, pre-existing): tests 3/4/5 die on that panic; test 2
// fails on the assertion (no helper-mapped entries); test 1 fails to
// compile (ioMultiResultFuncs undefined). With only the import panic
// fixed (baseline+pkgPathOf-only probe), every multi-result interface
// call fails with the WS-D scope error. Green: pass with D1.
//
//  1. TestD1IoDesugarWiring pins the ioMultiResultFuncs table entries
//     against synth_io.go (helper/pair/fields, following the
//     TestM7StrconvJsonDesugarWiring pattern).
//  2. TestD1IfaceMethodTableHelperMapping pins the itab wiring: the
//     *strings.Reader / io.discard / *io.LimitedReader method tables map
//     Read/Write to the pair-returning __-helpers' funcdecl ids.
//  3. TestD1IfaceMultiResultCallNode pins the wire shape of
//     `n, err := r.Read(buf)` through an interface: a desugared block
//     whose temp holds an ifacemethod node typed as the interface pair.
//  4. TestD1IfaceMultiResultStillFailFast pins that a multi-result
//     interface method with no table entry (3-result user method;
//     tabled-name/untabled-types 2-result method exercising the
//     positional types.Identical guard; io.ReaderFrom.ReadFrom) keeps
//     the fail-fast.
//  5. TestD1IfaceMultiResultUserInterface pins the structural match: a
//     user-declared interface with the Read shape dispatches too.
//  6. TestD1VarDeclMultiResultFailFast pins the named M8 limitation for
//     `var n, err = r.Read(buf)` (ValueSpec position): fail fast, never
//     a 2-name/1-pair-value vardecl.

import (
	"go/types"
	"strings"
	"testing"
)

type d1IoExpect struct {
	pkgPath    string
	name       string
	recvType   string
	helper     string
	pairType   string
	pairFields []string
	// pairFieldTypes parallels pairFields: the predeclared type of each
	// pair field (P2-3 — the test pins types as well as names against
	// synth_io.go). Keys are d1BasicType keys.
	pairFieldTypes []string
}

var d1IoTable = []d1IoExpect{
	{"io", "WriteString", "", "__writeString", "ioWritePair", []string{"n", "err"}, []string{"int", "error"}},
	{"io", "ReadAtLeast", "", "__readAtLeast", "readAtLeastPair", []string{"n", "err"}, []string{"int", "error"}},
	{"io", "ReadFull", "", "__readFull", "readFullPair", []string{"n", "err"}, []string{"int", "error"}},
	{"io", "Copy", "", "__copy", "copyPair", []string{"written", "err"}, []string{"int64", "error"}},
	{"io", "CopyN", "", "__copyN", "copyNPair", []string{"written", "err"}, []string{"int64", "error"}},
	{"io", "CopyBuffer", "", "__copyBufferChecked", "copyBufferPair", []string{"written", "err"}, []string{"int64", "error"}},
	{"io", "ReadAll", "", "__readAll", "readAllPair", []string{"b", "err"}, []string{"[]byte", "error"}},
	{"io", "Read", "LimitedReader", "__limitedReaderRead", "limitedReaderReadPair", []string{"n", "err"}, []string{"int", "error"}},
	{"io", "Write", "discard", "__discardWrite", "ioWritePair", []string{"n", "err"}, []string{"int", "error"}},
	{"io", "WriteString", "discard", "__discardWriteString", "ioWritePair", []string{"n", "err"}, []string{"int", "error"}},
}

// d1BasicType maps a d1IoExpect pairFieldTypes key to the universe type
// for the types.Identical pin. Every pair field is a predeclared type.
func d1BasicType(key string) (types.Type, bool) {
	switch key {
	case "int":
		return types.Typ[types.Int], true
	case "int64":
		return types.Typ[types.Int64], true
	case "error":
		return types.Universe.Lookup("error").Type(), true
	case "[]byte":
		return types.NewSlice(types.Typ[types.Byte]), true
	}
	return nil, false
}

func TestD1IoDesugarWiring(t *testing.T) {
	if len(d1IoTable) != len(ioMultiResultFuncs) {
		t.Fatalf("d1IoTable has %d entries, ioMultiResultFuncs has %d",
			len(d1IoTable), len(ioMultiResultFuncs))
	}
	for _, want := range d1IoTable {
		name := want.pkgPath + "." + want.name
		if want.recvType != "" {
			name = want.pkgPath + "." + want.recvType + "." + want.name
		}
		t.Run(name, func(t *testing.T) {
			var got *m7MultiResultFunc
			for i := range ioMultiResultFuncs {
				mf := &ioMultiResultFuncs[i]
				if mf.pkgPath == want.pkgPath && mf.name == want.name && mf.recvType == want.recvType {
					if got != nil {
						t.Fatalf("duplicate ioMultiResultFuncs entry for %s", name)
					}
					got = mf
				}
			}
			if got == nil {
				t.Fatalf("no ioMultiResultFuncs entry for %s", name)
			}
			if got.helper != want.helper || got.pairType != want.pairType {
				t.Fatalf("entry = helper %q pair %q, want helper %q pair %q",
					got.helper, got.pairType, want.helper, want.pairType)
			}
			if len(got.pairFields) != len(want.pairFields) {
				t.Fatalf("pairFields = %v, want %v", got.pairFields, want.pairFields)
			}
			for i := range want.pairFields {
				if got.pairFields[i] != want.pairFields[i] {
					t.Fatalf("pairFields = %v, want %v", got.pairFields, want.pairFields)
				}
			}
			// The __-helper exists and the pair struct's fields match
			// the multi-result signature's results positionally.
			sp := getSyntheticPackage(want.pkgPath)
			hobj := sp.pkg.Scope().Lookup(want.helper)
			if _, ok := hobj.(*types.Func); !ok {
				t.Fatalf("helper %s.%s missing from package scope", want.pkgPath, want.helper)
			}
			pobj := sp.pkg.Scope().Lookup(want.pairType)
			nt, _ := pobj.Type().(*types.Named)
			if nt == nil {
				t.Fatalf("pair type %s.%s missing", want.pkgPath, want.pairType)
			}
			st, ok := nt.Underlying().(*types.Struct)
			if !ok || st.NumFields() != len(want.pairFields) {
				t.Fatalf("pair type %s is not a %d-field struct", want.pairType, len(want.pairFields))
			}
			for i, fn := range want.pairFields {
				if st.Field(i).Name() != fn {
					t.Fatalf("pair field %d = %q, want %q", i, st.Field(i).Name(), fn)
				}
			}
			// P2-3: pin the pair field types too — types are half of the
			// table-against-synth_io.go contract.
			if len(want.pairFieldTypes) != len(want.pairFields) {
				t.Fatalf("d1IoTable row %s: pairFieldTypes has %d entries, pairFields has %d",
					name, len(want.pairFieldTypes), len(want.pairFields))
			}
			for i, tn := range want.pairFieldTypes {
				wantT, ok := d1BasicType(tn)
				if !ok {
					t.Fatalf("d1IoTable row %s: unknown pairFieldTypes key %q", name, tn)
				}
				if !types.Identical(st.Field(i).Type(), wantT) {
					t.Fatalf("pair field %d type = %s, want %s", i, st.Field(i).Type(), tn)
				}
			}
		})
	}
}

// d1ExportIfaceProg exports src and returns the decoded wire document.
func d1ExportIfaceProg(t *testing.T, src string) map[string]any {
	t.Helper()
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	return decodeExport(t, out)
}

// d1EntryTypeName resolves a named/pointer type-table entry to its
// wire type name: detail.name for named entries; for pointer entries
// (whose detail carries no name, only elem) "*"+ the element type's
// detail.name. Returns "" when the name cannot be resolved.
func d1EntryTypeName(t *testing.T, doc map[string]any, em map[string]any) string {
	t.Helper()
	d, _ := em["detail"].(map[string]any)
	if name, _ := d["name"].(string); name != "" {
		return name
	}
	kind, _ := em["kind"].(string)
	if kind == "pointer" {
		if elem, ok := d["elem"].(float64); ok {
			tes := typeEntries(t, doc)
			if int(elem) < len(tes) {
				if ee, ok := tes[int(elem)].(map[string]any); ok {
					ed, _ := ee["detail"].(map[string]any)
					if en, _ := ed["name"].(string); en != "" {
						return "*" + en
					}
				}
			}
		}
	}
	return ""
}

// d1MethodTable returns the wire "methods" lists of EVERY named/pointer
// type-table entry whose resolved type name matches wantName
// (e.g. "*strings.Reader", "io.discard"). A type may own several table
// entries in the wire (one per pointer type id), so callers must check
// all of them — a mis-wired itab could hide behind the first.
func d1MethodTable(t *testing.T, doc map[string]any, wantName string) [][]any {
	t.Helper()
	var out [][]any
	for _, e := range typeEntries(t, doc) {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := em["kind"].(string)
		if kind != "named" && kind != "pointer" {
			continue
		}
		if d1EntryTypeName(t, doc, em) != wantName {
			continue
		}
		d, _ := em["detail"].(map[string]any)
		ms, _ := d["methods"].([]any)
		out = append(out, ms)
	}
	if len(out) == 0 {
		t.Fatalf("no type-table entry named %s", wantName)
	}
	return out
}

// d1FuncIDByName maps wire funcdecl object ids to names.
func d1FuncIDByName(t *testing.T, doc map[string]any) map[float64]string {
	t.Helper()
	out := map[float64]string{}
	fns, _ := doc["funcs"].([]any)
	for _, f := range fns {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		fl, _ := fm["fields"].(map[string]any)
		id, _ := fl["obj"].(float64)
		name, _ := fl["name"].(string)
		out[id] = name
	}
	return out
}

func TestD1IfaceMethodTableHelperMapping(t *testing.T) {
	const src = `package main

import (
	"io"
	"strings"
)

func main() {
	var r io.Reader = strings.NewReader("hi")
	var w io.Writer = io.Discard
	var lr *io.LimitedReader
	println(r == nil, w == nil, lr == nil)
}
`
	doc := d1ExportIfaceProg(t, src)
	funcIDs := d1FuncIDByName(t, doc)
	// The per-type method tables map the multi-result methods to the
	// pair-returning __-helpers (obj = helper funcdecl id). Each case
	// uses d1MethodTable on the type's own table entry, so a mis-wired
	// itab — the right method attached to the wrong type, or pointing
	// at the wrong type's helper — fails that type's assertion (P1-2).
	// The old any-entry scan could not catch a mis-wired itab.
	cases := []struct {
		typeName string
		method   string
		helper   string
	}{
		{"*strings.Reader", "Read", "__stringsReaderRead"},
		{"io.discard", "Write", "__discardWrite"},
		{"io.discard", "WriteString", "__discardWriteString"},
		{"*io.LimitedReader", "Read", "__limitedReaderRead"},
	}
	for _, c := range cases {
		t.Run(c.typeName+"."+c.method, func(t *testing.T) {
			// Every table entry owned by this type must map the method
			// to the expected helper — checking only the first would
			// let a mis-wired duplicate entry hide.
			tables := d1MethodTable(t, doc, c.typeName)
			for ti, ms := range tables {
				found := false
				for _, m := range ms {
					mm, ok := m.(map[string]any)
					if !ok || mm["name"] != c.method {
						continue
					}
					if found {
						t.Fatalf("table %d for %s: duplicate %s entry", ti, c.typeName, c.method)
					}
					found = true
					obj, _ := mm["obj"].(float64)
					if got := funcIDs[obj]; got != c.helper {
						t.Errorf("table %d for %s: obj = %v (%q), want helper %q",
							ti, c.typeName, obj, got, c.helper)
					}
					// The entry must be for the right method: the
					// method's implkey must match the multi-result
					// method's (name + receiver-stripped signature).
					ik, _ := mm["implkey"].(string)
					if !strings.Contains(ik, c.method+"\x00") {
						t.Errorf("table %d for %s: method %q entry has implkey %q",
							ti, c.typeName, c.method, ik)
					}
				}
				if !found {
					t.Errorf("table %d for %s: no %s entry", ti, c.typeName, c.method)
				}
			}
		})
	}
	// Negative scan: no other type's table maps these methods to
	// helpers they shouldn't have. A mis-wiring that attaches a helper
	// under the wrong type (e.g. __stringsReaderRead under
	// *io.LimitedReader's table) fails here even when each type's own
	// table looks right.
	allowed := map[string]map[string]bool{}
	for _, c := range cases {
		m := allowed[c.typeName]
		if m == nil {
			m = map[string]bool{}
			allowed[c.typeName] = m
		}
		m[c.method] = true
	}
	helpers := map[string]bool{
		"__stringsReaderRead": true, "__discardWrite": true,
		"__discardWriteString": true, "__limitedReaderRead": true,
	}
	for _, e := range typeEntries(t, doc) {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := em["kind"].(string)
		if kind != "named" && kind != "pointer" {
			continue
		}
		name := d1EntryTypeName(t, doc, em)
		d, _ := em["detail"].(map[string]any)
		ms, _ := d["methods"].([]any)
		for _, m := range ms {
			mm, _ := m.(map[string]any)
			mn, _ := mm["name"].(string)
			obj, _ := mm["obj"].(float64)
			if !helpers[funcIDs[obj]] {
				continue
			}
			if !allowed[name][mn] {
				t.Errorf("unexpected helper mapping: type %q method %q -> %q (obj %v)",
					name, mn, funcIDs[obj], obj)
			}
		}
	}
}

func TestD1IfaceMultiResultCallNode(t *testing.T) {
	const src = `package main

import (
	"io"
	"strings"
)

func main() {
	var r io.Reader = strings.NewReader("hello")
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	println(n, err == nil)
}
`
	doc := d1ExportIfaceProg(t, src)
	nodes := findAllKind(doc, "ifacemethod")
	if len(nodes) == 0 {
		t.Fatal("no ifacemethod node in wire doc")
	}
	var readNode map[string]any
	for _, n := range nodes {
		if n["fields"].(map[string]any)["method"] == "Read" {
			readNode = n
		}
	}
	if readNode == nil {
		t.Fatal("no ifacemethod node for Read")
	}
	f := readNode["fields"].(map[string]any)
	// The node's type must be the ioReadPair type-table id.
	typeID, _ := f["type"].(float64)
	types := typeEntries(t, doc)
	te, ok := types[int(typeID)].(map[string]any)
	if !ok {
		t.Fatalf("ifacemethod type id %v out of range", typeID)
	}
	detail, _ := te["detail"].(map[string]any)
	if detail["name"] != "io.ioReadPair" {
		t.Fatalf("ifacemethod type = %v, want io.ioReadPair", detail["name"])
	}
	// The desugared block: __p5mr_N := <ifacemethod>, then two
	// selector extractions assigned to n and err.
	blocks := findAllKind(doc, "block")
	found := false
	for _, b := range blocks {
		stmts, _ := b["fields"].(map[string]any)["stmts"].([]any)
		if len(stmts) != 3 {
			continue
		}
		s0, _ := stmts[0].(map[string]any)
		if s0["kind"] != "vardecl" {
			continue
		}
		vals, _ := s0["fields"].(map[string]any)["values"].([]any)
		if len(vals) != 1 {
			continue
		}
		v0, _ := vals[0].(map[string]any)
		if v0["kind"] != "ifacemethod" {
			continue
		}
		s1, _ := stmts[1].(map[string]any)
		s2, _ := stmts[2].(map[string]any)
		names1, _ := s1["fields"].(map[string]any)["names"].([]any)
		names2, _ := s2["fields"].(map[string]any)["names"].([]any)
		if len(names1) == 1 && names1[0] == "n" && len(names2) == 1 && names2[0] == "err" {
			found = true
		}
	}
	if !found {
		t.Fatal("no desugared block __p5mr_N := r.Read(buf); n := ...; err := ... found")
	}
}

func TestD1IfaceMultiResultStillFailFast(t *testing.T) {
	// P4 (WS D, general interface MR): user-defined multi-result
	// interface methods now work via classifyUserIfaceMultiResultCall
	// (synthesized pair types), not just the m8IfaceMultiResultMethods
	// whitelist. These cases previously failed with the WS-D scope
	// error; they now compile.
	//
	// NOTE (P3, Gate-1 fix-iface-mr): io.WriterTo.WriteTo and
	// io.ReaderFrom.ReadFrom were added to m8IfaceMultiResultMethods
	// (bufio needs both for image/png). They no longer fail fast at
	// export; concrete types without pair helpers fail loudly at
	// runtime ("missing method" or the stub's scope panic), per the
	// established M8 design.
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "3-result user method",
			src: `package main

type R interface {
	Read(p []byte) (int, error, int)
}

type B struct{}

func (B) Read(p []byte) (int, error, int) { return 0, nil, 0 }

func main() {
	var r R = B{}
	n, err, _ := r.Read(nil)
	println(n, err == nil)
}
`,
		},
		{
			name: "2-result user method, non-tabled result types",
			src: `package main

type R interface {
	Read(p []byte) (int, string)
}

type B struct{}

func (B) Read(p []byte) (int, string) { return 0, "" }

func main() {
	var r R = B{}
	n, s := r.Read(nil)
	println(n, s)
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ExportFile(writeIfaceProg(t, tc.src))
			if err != nil {
				t.Fatalf("expected success for %s, got error: %v", tc.name, err)
			}
		})
	}
}

// TestD1VarDeclMultiResultFailFast: `var n, err = r.Read(buf)` (ValueSpec
// position) keeps the named M8 limitation fail-fast instead of emitting
// a 2-name/1-pair-value vardecl (REVIEW-1 P2-5).
func TestD1VarDeclMultiResultFailFast(t *testing.T) {
	const src = `package main

import "io"

func main() {
	var r io.Reader
	buf := make([]byte, 10)
	var n, err = r.Read(buf)
	println(n, err == nil)
}
`
	_, err := ExportFile(writeIfaceProg(t, src))
	if err == nil {
		t.Fatal("expected M8 limitation error for var-decl multi-result interface call, got success")
	}
	if !strings.Contains(err.Error(), "var declaration") {
		t.Fatalf("error %q does not contain the var-declaration limitation", err.Error())
	}
}

func TestD1IfaceMultiResultUserInterface(t *testing.T) {
	// A user-declared interface with the Read shape dispatches through
	// the same machinery (structural match on method name + results).
	const src = `package main

import (
	"io"
	"strings"
)

type reader interface {
	Read(p []byte) (int, error)
}

func main() {
	var r reader = strings.NewReader("hi")
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	println(n, err == nil)
	// io.Reader still works alongside.
	var r2 io.Reader = strings.NewReader("yo")
	n2, err2 := r2.Read(buf)
	println(n2, err2 == nil)
}
`
	doc := d1ExportIfaceProg(t, src)
	// Count ifacemethod Read nodes in the user's main function only
	// (the synthetic io helpers contain their own r.Read/dst.Write
	// dispatches).
	reads := 0
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != "main" {
			continue
		}
		for _, n := range findAllKind(fm, "ifacemethod") {
			if n["fields"].(map[string]any)["method"] == "Read" {
				reads++
			}
		}
	}
	if reads != 2 {
		t.Fatalf("want 2 ifacemethod Read nodes in main (user + io interfaces), got %d", reads)
	}
}

// TestD1BareIfaceCall: bare r.Read(buf) as a statement must dispatch via
// ifacemethod, not fail fast (P1 fix for REVIEW-1).
func TestD1BareIfaceCall(t *testing.T) {
	src := `package main
import "io"
func main() {
	var r io.Reader
	buf := make([]byte, 10)
	r.Read(buf)
	r.Read(buf)
}
`
	doc := d1ExportIfaceProg(t, src)
	// Should export successfully (not fail with M8 scope error).
	// Count ifacemethod Read nodes in main.
	reads := 0
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != "main" {
			continue
		}
		for _, n := range findAllKind(fm, "ifacemethod") {
			if n["fields"].(map[string]any)["method"] == "Read" {
				reads++
			}
		}
	}
	if reads != 2 {
		t.Fatalf("want 2 ifacemethod Read nodes (bare statements), got %d", reads)
	}
}

// TestD1IfaceMultiResultAssignPosition: an unlisted multi-result
// interface method in `=` assignment position (not `:=`) desugars via
// the general classifier (classifyUserIfaceMultiResultCall), exactly
// like expression position. Regression test for the red-master
// incident where fmt/scan.go's `r, size, err = s.rs.ReadRune()`
// (io.RuneReader, 3 results, in no whitelist) fell through
// classifyMultiResultCall to a plain 3-target/1-rhs assign, which the
// v1 backend rejects (assign.fields: lhs/rhs length mismatch) —
// failing all 38 snapshot programs' builds.
func TestD1IfaceMultiResultAssignPosition(t *testing.T) {
	const src = `package main

import "io"

func readRune3(r io.RuneReader) (rune, int, error) {
	var ch rune
	var size int
	var err error
	ch, size, err = r.ReadRune()
	return ch, size, err
}

func main() {
	ch, _, _ := readRune3(nil)
	println(ch)
}
`
	doc := d1ExportIfaceProg(t, src)
	// The v1 backend invariant: every assign has len(lhs)==len(rhs)
	// or is a comma-ok form. A 3-lhs/1-rhs assign must never be
	// emitted.
	for _, n := range findAllKind(doc, "assign") {
		f := n["fields"].(map[string]any)
		lhs, _ := f["lhs"].([]any)
		rhs, _ := f["rhs"].([]any)
		if len(lhs) == 3 && len(rhs) == 1 {
			t.Fatalf("emitted 3-target/1-rhs assign (backend would reject): %v", n)
		}
	}
	// The desugared block: __p5mr_N := <ifacemethod ReadRune>, then
	// three extractions assigned to ch, size, err.
	blocks := findAllKind(doc, "block")
	found := false
	for _, b := range blocks {
		stmts, _ := b["fields"].(map[string]any)["stmts"].([]any)
		if len(stmts) != 4 {
			continue
		}
		s0, _ := stmts[0].(map[string]any)
		if s0["kind"] != "vardecl" {
			continue
		}
		vals, _ := s0["fields"].(map[string]any)["values"].([]any)
		if len(vals) != 1 {
			continue
		}
		v0, _ := vals[0].(map[string]any)
		if v0["kind"] != "ifacemethod" {
			continue
		}
		if v0["fields"].(map[string]any)["method"] != "ReadRune" {
			continue
		}
		found = true
	}
	if !found {
		t.Fatal("no desugared block __p5mr_N := r.ReadRune(); ch = ...; size = ...; err = ... found")
	}
}
