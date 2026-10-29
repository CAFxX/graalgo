package main

// Regression tests for multi-result methods of disk packages being
// dropped from method tables when a static Appendix-M entry exists
// (fix-redmaster-r40, failure B).
//
// reflect's (*rtype).FieldByName has a real body in the disk source
// and a static m7 entry naming a hand-written helper in the synthetic
// reflect twin (__reflectTFieldByName, which takes the synthetic
// rtype). transformDiskMultiResult skipped the real method outright,
// so it was emitted nowhere: no funcdecl, no disk MR spec, and — via
// multiResultHelperFunc finding no usable helper — no entry in
// *rtype's wire method table. Any interface dispatch on a *rtype
// (even t.Kind()) then failed when the backend built the itab:
// "method \"FieldByName\" missing for dynamic type \"*reflect.rtype\"".
// That broke fmt.Print/Printf (which dispatch Type methods through
// the interface) in any program whose import graph did not mask it.
//
// Fixes: (1) the disk transform only applies the static-entry skip to
// bodyless declarations — a real body is transformed and registers a
// disk spec; (2) multiResultHelperFor prefers a disk spec over the
// static tables, since the disk helper is the one whose receiver
// representation matches the concrete type's values.

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestDiskMRStaticEntryRealBodyStillTransformed(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "type.go", `package reflect

type rtype struct{ x int }

type StructField struct{ Name string }

func (t *rtype) FieldByName(name string) (StructField, bool) {
	return StructField{}, false
}
`, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("reflect", []*ast.File{f})
	var found *diskMRSpec
	for i := range specs {
		if specs[i].name == "FieldByName" && specs[i].recvType == "rtype" {
			found = &specs[i]
		}
	}
	if found == nil {
		t.Fatal("no disk MR spec for (*rtype).FieldByName: the static-entry " +
			"skip dropped the real-bodied method")
	}
	if found.helper != "__rtype_FieldByName" {
		t.Fatalf("spec helper = %q, want __rtype_FieldByName", found.helper)
	}
	// The helper FuncDecl must exist in the rewritten AST.
	hasHelper := false
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "__rtype_FieldByName" {
			hasHelper = true
		}
	}
	if !hasHelper {
		t.Fatal("rewritten AST has no __rtype_FieldByName helper decl")
	}
}

func TestDiskMRM5EntryRealBodyStillSkipped(t *testing.T) {
	// Negative, the other tier: M5-owned entries (context.Deadline)
	// skip even with a real body — dedicated exporter machinery
	// (mrDeadline) desugars their call sites, and the mechanical
	// transform must not rewrite them (guards
	// TestDeadlineCallableP4's contract at the transform level).
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "context.go", `package context

type cancelCtx struct{ x int }

type Time struct{}

func (c *cancelCtx) Deadline() (Time, bool) { return Time{}, false }
`, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("context", []*ast.File{f})
	for _, s := range specs {
		if s.name == "Deadline" {
			t.Fatal("M5-owned context.Deadline was transformed; want skip")
		}
	}
}

func TestDiskMRStaticEntryBodylessStillSkipped(t *testing.T) {
	// Negative: a bodyless declaration with a static entry (a linkname
	// shim shape) keeps the skip — there is no real body to transform
	// and the static helper serves it. The fixture uses a SURVIVING
	// static entry (syscall.Accept): the reflect triple previously
	// used here (rtype.FieldByName) lost its static entry when
	// fix-redmaster-r42 deleted the stale reflect_m7 rows (reflect
	// now compiles from disk, so those calls take the normal disk MR
	// path), and a triple with no static entry no longer satisfies
	// this test's precondition — all seven deleted positions have
	// bodies in real disk reflect, so the deletion changes no real
	// bodyless declaration's treatment.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "syscall.go", `package syscall

type Sockaddr struct{ x int }

func Accept(fd int, sa Sockaddr) (int, Sockaddr, error)
`, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("syscall", []*ast.File{f})
	for _, s := range specs {
		if s.name == "Accept" {
			t.Fatal("bodyless static-entry function was transformed; want skip")
		}
	}
}

// TestDiskMRMethodEntriesMapToHelpers exports a program using fmt
// (which loads internal/abi through reflect) and asserts the wire
// method entries for disk multi-result methods of internal/abi types
// resolve to the pair-returning __-helpers, not the panic stubs. On
// the unfixed code the entries carry the stub object ids: the
// backend's reflect method installation skips them with an
// "unknown function object id" warning on stderr (part of the
// warning class that makes snapshot-corpus programs drift — the
// snapshot expects empty stderr) and the methods are unavailable
// via reflect.
func TestDiskMRMethodEntriesMapToHelpers(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	fmt.Println("x")
}
`
	doc := exportSrc(t, src)
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(doc), &raw); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	types, _ := raw["types"].([]any)
	funcs, _ := raw["funcs"].([]any)
	funcName := map[float64]string{}
	for _, fv := range funcs {
		fm, _ := fv.(map[string]any)
		fields := nodeFields(t, fm)
		if obj, ok := fields["obj"].(float64); ok {
			name, _ := fields["name"].(string)
			funcName[obj] = name
		}
	}
	want := map[string]string{
		"internal/abi.Name.ReadVarint":               "__Name_ReadVarint",
		"internal/abi.StructType.descriptorSizes":    "__StructType_descriptorSizes",
		"internal/abi.SliceType.descriptorSizes":     "__SliceType_descriptorSizes",
		"internal/abi.MapType.descriptorSizes":       "__MapType_descriptorSizes",
		"internal/abi.PtrType.descriptorSizes":       "__PtrType_descriptorSizes",
		"internal/abi.ArrayType.descriptorSizes":     "__ArrayType_descriptorSizes",
		"internal/abi.ChanType.descriptorSizes":      "__ChanType_descriptorSizes",
		"internal/abi.FuncType.descriptorSizes":      "__FuncType_descriptorSizes",
		"internal/abi.InterfaceType.descriptorSizes": "__InterfaceType_descriptorSizes",
	}
	// Display names per type id: named types by their detail name,
	// pointer types as "*<elem name>" (pointer-receiver method sets —
	// e.g. descriptorSizes — live on the pointer entry).
	displayName := map[float64]string{}
	for i, tv := range types {
		tm, _ := tv.(map[string]any)
		detail, _ := tm["detail"].(map[string]any)
		if detail == nil {
			continue
		}
		if name, _ := detail["name"].(string); name != "" {
			displayName[float64(i)] = name
		}
	}
	for i, tv := range types {
		tm, _ := tv.(map[string]any)
		detail, _ := tm["detail"].(map[string]any)
		if detail == nil {
			continue
		}
		if tm["kind"] == "pointer" {
			if elem, ok := detail["elem"].(float64); ok {
				if en, ok := displayName[elem]; ok {
					displayName[float64(i)] = "*" + en
				}
			}
		}
	}
	found := map[string]string{}
	for i, tv := range types {
		tm, _ := tv.(map[string]any)
		detail, _ := tm["detail"].(map[string]any)
		if detail == nil {
			continue
		}
		tname := displayName[float64(i)]
		// The want keys name the value type; pointer entries carry
		// the same methods for pointer receivers.
		base := tname
		if len(base) > 0 && base[0] == '*' {
			base = base[1:]
		}
		methods, _ := detail["methods"].([]any)
		for _, mv := range methods {
			mm, _ := mv.(map[string]any)
			mname, _ := mm["name"].(string)
			key := base + "." + mname
			if _, wanted := want[key]; wanted {
				obj, _ := mm["obj"].(float64)
				found[key] = funcName[obj]
			}
		}
	}
	for key, helper := range want {
		got, ok := found[key]
		if !ok {
			t.Fatalf("no method entry for %s in the exported types", key)
		}
		if got != helper {
			t.Fatalf("method %s maps to %q, want disk helper %q (stub ids warn+skip in the backend)", key, got, helper)
		}
	}
}

// TestRTypeMethodTableHasMultiResultMethods exports a program that
// dispatches reflect.Type methods and asserts *rtype's wire method
// table carries the multi-result methods, mapped to the disk helper.
func TestRTypeMethodTableHasMultiResultMethods(t *testing.T) {
	src := `package main

import (
	"fmt"
	"reflect"
)

type S struct{ Age int }

func main() {
	t := reflect.TypeOf(S{})
	f, ok := t.FieldByName("Age")
	fmt.Println(f.Name, ok)
}
`
	doc := exportSrc(t, src)
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(doc), &raw); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	types, _ := raw["types"].([]any)
	funcs, _ := raw["funcs"].([]any)
	// Map func obj -> name for resolving method entries.
	funcName := map[float64]string{}
	for _, fv := range funcs {
		fm, _ := fv.(map[string]any)
		fields := nodeFields(t, fm)
		if obj, ok := fields["obj"].(float64); ok {
			name, _ := fields["name"].(string)
			funcName[obj] = name
		}
	}
	found := map[string]string{}
	for _, tv := range types {
		tm, _ := tv.(map[string]any)
		detail, _ := tm["detail"].(map[string]any)
		if detail == nil {
			continue
		}
		// The *rtype entry is a pointer whose methods list the rtype
		// method set; identify it by a distinctive method pair.
		methods, _ := detail["methods"].([]any)
		if len(methods) == 0 {
			continue
		}
		names := map[string]float64{}
		for _, mv := range methods {
			mm, _ := mv.(map[string]any)
			if nm, ok := mm["name"].(string); ok {
				obj, _ := mm["obj"].(float64)
				names[nm] = obj
			}
		}
		if _, hasKind := names["Kind"]; hasKind {
			if _, hasCommon := names["common"]; hasCommon {
				for _, want := range []string{"FieldByName", "FieldByNameFunc", "MethodByName"} {
					if obj, ok := names[want]; ok {
						found[want] = funcName[obj]
					}
				}
			}
		}
	}
	for _, want := range []string{"FieldByName", "FieldByNameFunc", "MethodByName"} {
		helper, ok := found[want]
		if !ok {
			t.Fatalf("*rtype method table is missing %s (itab build will fail at dispatch)", want)
		}
		if helper != "__rtype_"+want {
			t.Fatalf("*rtype.%s maps to %q, want disk helper __rtype_%s", want, helper, want)
		}
	}
}
