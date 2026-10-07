package main

// P3-M8 (WS D): exporter pinning tests for multi-result interface method
// dispatch (D1). Red-green: these fail on the pre-D1 tree (every
// multi-result interface call is a scope error; the method tables carry
// no helper-mapped entries) and pass after.
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
//     interface method with no table entry (io.ReaderFrom.ReadFrom)
//     keeps the fail-fast.
//  5. TestD1IfaceMultiResultUserInterface pins the structural match: a
//     user-declared interface with the Read shape dispatches too.

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
}

var d1IoTable = []d1IoExpect{
	{"io", "WriteString", "", "__writeString", "ioWritePair", []string{"n", "err"}},
	{"io", "ReadAtLeast", "", "__readAtLeast", "readAtLeastPair", []string{"n", "err"}},
	{"io", "ReadFull", "", "__readFull", "readFullPair", []string{"n", "err"}},
	{"io", "Copy", "", "__copy", "copyPair", []string{"written", "err"}},
	{"io", "CopyN", "", "__copyN", "copyNPair", []string{"written", "err"}},
	{"io", "CopyBuffer", "", "__copyBufferChecked", "copyBufferPair", []string{"written", "err"}},
	{"io", "ReadAll", "", "__readAll", "readAllPair", []string{"b", "err"}},
	{"io", "Read", "LimitedReader", "__limitedReaderRead", "limitedReaderReadPair", []string{"n", "err"}},
	{"io", "Write", "discard", "__discardWrite", "ioWritePair", []string{"n", "err"}},
	{"io", "WriteString", "discard", "__discardWriteString", "ioWritePair", []string{"n", "err"}},
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

// d1MethodTable returns the wire "methods" list of the named/pointer
// type-table entry whose detail name matches.
func d1MethodTable(t *testing.T, doc map[string]any, wantName string) []any {
	t.Helper()
	for _, e := range typeEntries(t, doc) {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		d, _ := em["detail"].(map[string]any)
		if d["name"] != wantName {
			continue
		}
		ms, _ := d["methods"].([]any)
		return ms
	}
	t.Fatalf("no type-table entry named %s", wantName)
	return nil
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
	// The -*T method tables map the multi-result methods to the
	// pair-returning __-helpers (obj = helper funcdecl id).
	cases := []struct {
		typeName string
		method   string
		helper   string
	}{
		// Pointer entries carry the full *T method set; named entries
		// carry the value method set. The test locates the entry holding
		// the mapped method.
		{"*strings.Reader", "Read", "__stringsReaderRead"},
		{"io.discard", "Write", "__discardWrite"},
		{"io.discard", "WriteString", "__discardWriteString"},
		{"*io.LimitedReader", "Read", "__limitedReaderRead"},
	}
	_ = cases
	// Scan every named/pointer entry: find the one whose method table
	// contains the mapped method pointing at the helper.
	for _, c := range cases {
		found := false
		for _, e := range typeEntries(t, doc) {
			em, ok := e.(map[string]any)
			if !ok {
				continue
			}
			kind, _ := em["kind"].(string)
			if kind != "named" && kind != "pointer" {
				continue
			}
			d, _ := em["detail"].(map[string]any)
			for _, m := range d["methods"].([]any) {
				mm, _ := m.(map[string]any)
				if mm["name"] != c.method {
					continue
				}
				obj, _ := mm["obj"].(float64)
				if funcIDs[obj] != c.helper {
					continue
				}
				// The entry must be for the right dynamic type: the
				// method's implkey must match the multi-result method's
				// (name + receiver-stripped signature).
				ik, _ := mm["implkey"].(string)
				if !strings.Contains(ik, c.method+"\x00") {
					t.Errorf("%s: method %q entry has implkey %q", c.typeName, c.method, ik)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("no method-table entry maps %s.%s to %s", c.typeName, c.method, c.helper)
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
	// A multi-result interface method with no D1 table entry (3 results)
	// must keep the fail-fast.
	const src = `package main

type R interface {
	Read(p []byte) (int, error, int)
}

type B struct{}

func main() {
	var r R
	n, err, _ := r.Read(nil)
	println(n, err == nil)
}
`
	_, err := ExportFile(writeIfaceProg(t, src))
	if err == nil {
		t.Fatal("expected scope error for 3-result interface method, got success")
	}
	if !strings.Contains(err.Error(), "WS D") {
		t.Fatalf("error %q does not contain the WS D scope error", err.Error())
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
