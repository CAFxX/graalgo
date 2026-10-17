package main

// Tests for body-less multi-result function handling in the disk
// multi-result transform (transformDiskMultiResult in mr_disk.go) and the
// body-less registration path (registerUserMRFunc in mrdesugar_m5.go).
//
// RV-P1-17 (option a′): body-less declarations (e.g. //go:noescape
// assembly stubs like reflect's chanrecv) are left untouched by the disk
// transform — no spec, pair type, helper, or stub is generated for them —
// and flow through the established exportLinknameDecl pipeline, which
// emits "unimplemented": true. Their call sites compile because
// registerUserMRFunc pre-registers the pair type for body-less decls
// (fixing the file-ordering gap that motivated 47840bf8).
//
// Pinned behaviors:
//  1. TestDiskMRBodylessNoSpec: a body-less multi-result FuncDecl
//     produces no diskMRSpec.
//  2. TestDiskMRBodylessDeclPreserved: the original declaration is
//     preserved in the output decls (not replaced by pair/helper/stub).
//  3. TestDiskMRBodylessTypeChecks: the rewritten AST type-checks cleanly.
//  4. TestDiskMRBodylessLookupNil: lookupDiskMR returns nil for the
//     body-less declaration (negative).
//  5. TestDiskMRBodylessSingleResultNegative: single-result body-less
//     declarations are untouched.

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

const diskMRBodylessSrc = `package p

import "unsafe"

//go:noescape
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`

func parseDiskMRBodyless(t *testing.T, src string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return f
}

// TestDiskMRBodylessNoSpec: a body-less multi-result FuncDecl produces no
// diskMRSpec — the disk transform leaves it alone.
func TestDiskMRBodylessNoSpec(t *testing.T) {
	f := parseDiskMRBodyless(t, diskMRBodylessSrc)
	specs := transformDiskMultiResult("example.com/p-bodyless", []*ast.File{f})
	if len(specs) != 0 {
		t.Fatalf("got %d specs for body-less multi-result func, want 0", len(specs))
	}
}

// TestDiskMRBodylessDeclPreserved: the original body-less declaration is
// preserved verbatim in the output decls — no pair type, __-helper, or
// stub is generated for it.
func TestDiskMRBodylessDeclPreserved(t *testing.T) {
	f := parseDiskMRBodyless(t, diskMRBodylessSrc)
	transformDiskMultiResult("example.com/p-bodyless", []*ast.File{f})
	// 2 decls: the import GenDecl and the original FuncDecl.
	if len(f.Decls) != 2 {
		t.Fatalf("got %d decls after transform, want 2 (import + original)", len(f.Decls))
	}
	fd, ok := f.Decls[1].(*ast.FuncDecl)
	if !ok {
		t.Fatalf("decl[1] is %T, want *ast.FuncDecl", f.Decls[1])
	}
	if fd.Name.Name != "chanrecv" {
		t.Errorf("decl name = %q, want %q", fd.Name.Name, "chanrecv")
	}
	if fd.Body != nil {
		t.Error("original body-less declaration gained a body")
	}
	if numResults(fd) != 2 {
		t.Errorf("numResults = %d, want 2", numResults(fd))
	}
}

// TestDiskMRBodylessTypeChecks: the rewritten AST (original declaration
// only) type-checks cleanly.
func TestDiskMRBodylessTypeChecks(t *testing.T) {
	checkDiskMRClean(t, diskMRBodylessSrc)
}

// TestDiskMRBodylessLookupNil: lookupDiskMR returns nil for a body-less
// multi-result declaration (negative — no desugar entry exists), while a
// bodied multi-result function in the same package does get one. This
// pins that the transform skips body-less decls without disturbing the
// normal path.
func TestDiskMRBodylessLookupNil(t *testing.T) {
	src := `package p

import "unsafe"

//go:noescape
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)

func bodied(a int) (int, bool) { return a, true }
`
	f := parseDiskMRBodyless(t, src)
	specs := transformDiskMultiResult("example.com/p-bodyless-lookup", []*ast.File{f})
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1 (bodied only)", len(specs))
	}
	// Save/restore the global: other tests must not see this package's
	// registration, and this test must not see theirs.
	diskMRMu.Lock()
	oldSpecs := diskMRSpecs
	diskMRSpecs = nil
	diskMRMu.Unlock()
	defer func() {
		diskMRMu.Lock()
		diskMRSpecs = oldSpecs
		diskMRMu.Unlock()
	}()
	registerDiskMRSpecs(specs)
	if spec := lookupDiskMR("example.com/p-bodyless-lookup", "chanrecv", ""); spec != nil {
		t.Errorf("lookupDiskMR(..., chanrecv, \"\") = %+v, want nil", spec)
	}
	if spec := lookupDiskMR("example.com/p-bodyless-lookup", "bodied", ""); spec == nil {
		t.Error("lookupDiskMR(..., bodied, \"\") = nil, want spec (sanity: lookup works)")
	}
}

// TestDiskMRBodylessSingleResultNegative: single-result body-less
// functions are NOT transformed (no spec, declaration preserved).
func TestDiskMRBodylessSingleResultNegative(t *testing.T) {
	src := `package p

import "unsafe"

//go:noescape
func chansend0(ch unsafe.Pointer, val unsafe.Pointer, nb bool) bool
`
	f := parseDiskMRBodyless(t, src)
	specs := transformDiskMultiResult("example.com/p-bodyless1", []*ast.File{f})
	if len(specs) != 0 {
		t.Fatalf("got %d specs for single-result body-less func, want 0", len(specs))
	}
	// 2 decls: the import GenDecl and the original FuncDecl.
	if len(f.Decls) != 2 {
		t.Fatalf("got %d decls after transform, want 2", len(f.Decls))
	}
	if _, ok := f.Decls[1].(*ast.FuncDecl); !ok {
		t.Fatalf("decl[1] is %T, want *ast.FuncDecl", f.Decls[1])
	}
}

// TestDiskMRBodylessLinknameExport is the (a′) proof gate (RV-P1-17 §4
// test 3): a disk package (real reflect from GOROOT) with a body-less
// multi-result stub (chanrecv) whose call site sits in multi-value
// position (Value.recv calls chanrecv(v.pointer(), nb, p); reached here
// via Value.TryRecv) must export cleanly — no "multi-valued call result
// is not supported in frontend/v1 scope (WS D)" error — and the emitted
// funcdecl must carry "unimplemented": true (the established
// exportLinknameDecl pipeline owns the failure, not a fabricated
// panic-body helper).
//
// Red-green: on the 47840bf8 code this fails (no "chanrecv" funcdecl is
// emitted — only __chanrecv with a panic body); on the pure pre-47840bf8
// revert without the registerUserMRFunc fix it fails with the WS D scope
// error.
func TestDiskMRBodylessLinknameExport(t *testing.T) {
	const src = `package main

import "reflect"

func main() {
	ch := make(chan int, 1)
	ch <- 42
	_, ok := reflect.ValueOf(ch).TryRecv()
	println(ok)
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		if strings.Contains(err.Error(), "multi-valued call result is not supported") {
			t.Fatalf("export failed with the WS D scope error (a′ ordering fix broken): %v", err)
		}
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	found := false
	for _, n := range findAllKind(doc, "funcdecl") {
		fields, _ := n["fields"].(map[string]any)
		name, _ := fields["name"].(string)
		pkgpath, _ := fields["pkgpath"].(string)
		if name != "chanrecv" || pkgpath != "reflect" {
			continue
		}
		found = true
		if fields["unimplemented"] != true {
			t.Errorf("reflect.chanrecv funcdecl missing \"unimplemented\": true")
		}
		if _, hasBody := fields["body"]; hasBody {
			t.Error("reflect.chanrecv funcdecl unexpectedly has a body")
		}
	}
	if !found {
		t.Error("no funcdecl for reflect.chanrecv in export (want the linkname/unimplemented decl)")
	}
}

func bodylessFuncDeclInfo(t *testing.T, pkgPath, src, name string) (*types.Package, *funcDeclInfo) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	info := &types.Info{
		Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object),
	}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check(pkgPath, fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}
	obj, _ := pkg.Scope().Lookup(name).(*types.Func)
	var sig *types.Signature
	if obj != nil {
		var ok bool
		sig, ok = obj.Type().(*types.Signature)
		if !ok {
			t.Fatalf("%s type is not a signature", name)
		}
	}
	var fd *ast.FuncDecl
	for _, decl := range f.Decls {
		if d, ok := decl.(*ast.FuncDecl); ok && d.Name.Name == name {
			fd = d
		}
	}
	if fd == nil {
		t.Fatalf("no FuncDecl for %s", name)
	}
	if fd.Body != nil {
		t.Fatalf("test fixture %s unexpectedly has a body", name)
	}
	// Note: methods are not in package scope; callers set fi.obj/fi.sig
	// from the type's method set (see TestRegisterUserMRFuncBodylessMethod).
	return pkg, &funcDeclInfo{fd: fd, obj: obj, sig: sig}
}

// TestRegisterUserMRFuncBodyless: a body-less *funcDeclInfo with 2 results
// gets a userMRFuncs entry with a synthesized pair type — the ordering
// fix (RV-P1-17 a′). This must hold regardless of declaration order: the
// pre-pass runs before the declaration is emitted, and exportFuncDecl's
// inline registration is idempotent with it.
func TestRegisterUserMRFuncBodyless(t *testing.T) {
	const src = `package p

//go:noescape
func stub2(a int, b string) (int, bool)
`
	pkg, fi := bodylessFuncDeclInfo(t, "example.com/p-reg", src, "stub2")
	e := &exporter{pkg: pkg, userMRFuncs: make(map[*types.Func]*userMRInfo)}
	mrInfo, err := e.registerUserMRFunc(fi)
	if err != nil {
		t.Fatalf("registerUserMRFunc: %v", err)
	}
	if mrInfo == nil {
		t.Fatal("registerUserMRFunc returned nil for body-less 2-result func, want entry")
	}
	if len(mrInfo.pairFields) != 2 || mrInfo.pairFields[0] != "f0" || mrInfo.pairFields[1] != "f1" {
		t.Errorf("pairFields = %v, want [f0 f1]", mrInfo.pairFields)
	}
	// The pair type's field types match the result types positionally.
	st, ok := mrInfo.pairType.Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("pair type underlying is %T, want *types.Struct", mrInfo.pairType.Underlying())
	}
	if st.NumFields() != 2 {
		t.Fatalf("pair struct has %d fields, want 2", st.NumFields())
	}
	if st.Field(0).Type() != types.Typ[types.Int] || st.Field(1).Type() != types.Typ[types.Bool] {
		t.Errorf("pair field types = %v, %v, want int, bool", st.Field(0).Type(), st.Field(1).Type())
	}
	// Idempotency: a second registration (the exportFuncDecl inline path)
	// returns the same entry instead of synthesizing a duplicate.
	mrInfo2, err := e.registerUserMRFunc(fi)
	if err != nil {
		t.Fatalf("second registerUserMRFunc: %v", err)
	}
	if mrInfo2 != mrInfo {
		t.Error("second registration returned a different entry (not idempotent)")
	}
}

// TestRegisterUserMRFuncBodylessMethod: a body-less multi-result method
// also gets a pair entry (receiver-qualified pair name).
func TestRegisterUserMRFuncBodylessMethod(t *testing.T) {
	const src = `package p

type T struct{}

//go:noescape
func (t T) multi() (int, string)
`
	pkg, fi := bodylessFuncDeclInfo(t, "example.com/p-regm", src, "multi")
	// Methods live in the type's method set, not package scope: resolve
	// the *types.Func from T's method set.
	tname, ok := pkg.Scope().Lookup("T").(*types.TypeName)
	if !ok {
		t.Fatal("p.T is not a *types.TypeName")
	}
	mobj := types.NewMethodSet(tname.Type()).Lookup(pkg, "multi")
	if mobj == nil {
		t.Fatal("method T.multi not found in method set")
	}
	mfn, ok := mobj.Obj().(*types.Func)
	if !ok {
		t.Fatal("T.multi is not a *types.Func")
	}
	fi.obj = mfn
	fi.sig = mfn.Type().(*types.Signature)
	// Wire the receiver like the importer does (sig.Recv()).
	fi.recv = fi.sig.Recv()
	e := &exporter{pkg: pkg, userMRFuncs: make(map[*types.Func]*userMRInfo)}
	mrInfo, err := e.registerUserMRFunc(fi)
	if err != nil {
		t.Fatalf("registerUserMRFunc: %v", err)
	}
	if mrInfo == nil {
		t.Fatal("registerUserMRFunc returned nil for body-less 2-result method, want entry")
	}
	if len(mrInfo.pairFields) != 2 {
		t.Errorf("pairFields = %v, want 2 fields", mrInfo.pairFields)
	}
}
