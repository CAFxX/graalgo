package main

// Exhaustive pinning for the multi-result desugar tables in
// mrdesugar_m5.go (P1-4, REVIEW-2026-11-17-07).
//
// Before this test, the tables were pinned only by hardcoded name lookup
// in three test files (~90 rows of 617 m7/m8 rows; the 36-row os table
// had zero test mentions). A typo'd, dropped, or stale row for an
// unlisted function failed silently: the call fell through to the M7
// scope panic at runtime instead of failing a test. This test iterates
// every table exhaustively, so adding, dropping, or renaming a row, a
// __-helper, or a pair struct fails here first.
//
// Two tests:
//
//   - TestDesugarTablesExhaustive (forward): every row in m7, m8, os,
//     m9, io (712 rows) and m8IfaceMultiResultMethods (39 rows) must
//     resolve: no duplicate key within a table; well-formed fields; the
//     multi-result function/method exists in the synthetic package with
//     exactly len(pairFields) results; the __-helper exists in the
//     classifier-resolved helper package (m7HelperPkgOverride for m7/io
//     rows, the row's helperPkg for m8/os/m9 rows) with exactly one
//     result and parameters accepting the call's [receiver,] arguments
//     (assignability, not identity: hash helpers take the hash.Hash
//     interface while the method's receiver is the concrete digest);
//     the pair struct exists in the helper package with exactly the
//     listed fields in order, each field's type matching the
//     corresponding result type (generic-aware: distinct *types.TypeParam
//     objects for the same type parameter compare by name+constraint).
//     Interface rows have no helper: the pair struct must exist with
//     exactly the listed fields in order.
//
//   - TestDesugarTablesReverseStubCoverage (reverse): every declared
//     multi-result function/method in a table package whose body is a
//     fail-fast stub (a single panic statement) must have a row in one
//     of the tables (or m5StaticMREntries), or be listed in
//     desugarReverseExemptions with a scope reason. Real-bodied
//     multi-result functions need no row: the exporter transforms them
//     itself (verified: a program calling bytes.Cut exports cleanly).
//     Interface methods are not enumerated here: iface dispatch matches
//     structurally per method shape, so there is no per-interface row
//     requirement; the iface rows themselves are pinned by the forward
//     test and their shape matching is enforced by
//     classifyIfaceMultiResultCall at each call site.
//
// Packages that cannot be type-checked get an AST fallback instead of a
// skip: archive/zip (undefined time.FixedZone) and mime/multipart (os
// used as a value) still parse, so the row's function, helper, and pair
// struct are verified textually (existence, result/parameter arity,
// pair field names/order). Packages with no synthetic source at all
// (net/http, net/http/httptest: retired to the disk importer) are
// covered only by the explicit desugarRetiredPkgs exemption, which the
// test verifies is still accurate (no synthetic source exists).
//
// The test never passes vacuously: every table must be non-empty, and
// every exemption entry must be consumed by a stub that still carries
// the exempting scope marker in its panic message.
//
// Known findings on the base this test landed on (master a0df481b):
// the test FAILS on (1) a duplicate m8 row (crypto/sha3 Write SHAKE,
// lines 698 and 803), (2) seven duplicate m8Iface rows (the
// elliptic/ecdh block pasted twice), (3) seventeen stub-bodied
// functions with helpers but no row (io/fs package-level
// Glob/ReadDir/ReadFile/Stat/Sub, dirInfo.Info, subFS
// Open/ReadDir/ReadFile/Glob/Sub; os.dirFS Open/ReadDir/ReadFile/Stat;
// crypto/tls Dialer.Dial/DialContext), and (4) the boring
// randReader.Read row, whose helper takes ([]byte, []byte) while the
// desugar prepends the randReader receiver, and (5) the crypto/sha3
// SHAKE.Write row (both copies): its helper __hashWrite takes hash.Hash
// but *SHAKE has no Sum/Size, so it does not implement hash.Hash
// (SHAKE.Read via __hashRead/hash.XOF is fine). Those are table defects
// for a follow-up lane; this test is the pin that caught them. The exact
// row literals for (3) are recorded in the lane report.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// desugarRow is one desugar table row, normalized across the five
// concrete tables (m7, m8, os, m9, io). helperPkg is resolved exactly as
// the classifiers resolve it: m7HelperPkgOverride for m7/io rows, the
// row's helperPkg field (or the declaring package) for m8/os/m9 rows.
type desugarRow struct {
	table      string
	pkgPath    string
	name       string
	recvType   string
	helper     string
	helperPkg  string
	pairType   string
	pairFields []string
}

func (r desugarRow) key() string {
	return r.pkgPath + "|" + r.name + "|" + r.recvType
}

func (r desugarRow) display() string {
	if r.recvType == "" {
		return r.table + " " + r.pkgPath + "." + r.name
	}
	return r.table + " " + r.pkgPath + "." + r.recvType + "." + r.name
}

// desugarIfaceRow is one m8IfaceMultiResultMethods row. There is no
// helper: interface dispatch resolves the pair struct only. pairPkg is
// resolved exactly as classifyIfaceMultiResultCall resolves it
// (m7HelperPkgOverride, else the declaring package).
type desugarIfaceRow struct {
	pkgPath    string
	method     string
	pairType   string
	pairFields []string
	pairPkg    string
}

func allDesugarConcreteRows() []desugarRow {
	var rows []desugarRow
	for _, mf := range m7MultiResultFuncs {
		hp := mf.pkgPath
		if o, ok := m7HelperPkgOverride[mf.pkgPath]; ok {
			hp = o
		}
		rows = append(rows, desugarRow{"m7", mf.pkgPath, mf.name, mf.recvType, mf.helper, hp, mf.pairType, mf.pairFields})
	}
	for _, mf := range m8MultiResultFuncs {
		hp := mf.helperPkg
		if hp == "" {
			hp = mf.pkgPath
		}
		rows = append(rows, desugarRow{"m8", mf.pkgPath, mf.name, mf.recvType, mf.helper, hp, mf.pairType, mf.pairFields})
	}
	for _, mf := range osMultiResultFuncs {
		hp := mf.helperPkg
		if hp == "" {
			hp = mf.pkgPath
		}
		rows = append(rows, desugarRow{"os", mf.pkgPath, mf.name, mf.recvType, mf.helper, hp, mf.pairType, mf.pairFields})
	}
	for _, mf := range m9MultiResultFuncs {
		hp := mf.helperPkg
		if hp == "" {
			hp = mf.pkgPath
		}
		rows = append(rows, desugarRow{"m9", mf.pkgPath, mf.name, mf.recvType, mf.helper, hp, mf.pairType, mf.pairFields})
	}
	for _, mf := range ioMultiResultFuncs {
		hp := mf.pkgPath
		if o, ok := m7HelperPkgOverride[mf.pkgPath]; ok {
			hp = o
		}
		rows = append(rows, desugarRow{"io", mf.pkgPath, mf.name, mf.recvType, mf.helper, hp, mf.pairType, mf.pairFields})
	}
	return rows
}

func allDesugarIfaceRows() []desugarIfaceRow {
	var rows []desugarIfaceRow
	for _, m := range m8IfaceMultiResultMethods {
		pp := m.pkgPath
		if o, ok := m7HelperPkgOverride[m.pkgPath]; ok {
			pp = o
		}
		rows = append(rows, desugarIfaceRow{m.pkgPath, m.method, m.pairType, m.pairFields, pp})
	}
	return rows
}

// desugarRetiredPkgs lists declaring packages whose rows cannot be
// verified against a synthetic source because the package retired to the
// disk importer. The value is the reason the exemption is sound. A row
// for a retired package passes only while the package still has no
// synthetic source; if one reappears the row is verified normally and
// the exemption lapses. Real net/http is not importable in frontend/v1
// scope (the WS G import overlay rejects x/net/http/httpguts), so no
// call site can reach these rows.
var desugarRetiredPkgs = map[string]string{
	"net/http":          "retired to the disk importer (P4 bridge-removal); synthetic source deleted with synth_net_http.go; real net/http is not importable in frontend/v1 scope",
	"net/http/httptest": "retired to the disk importer (P4 bridge-removal); synthetic source deleted with synth_net_http.go; real net/http/httptest is not importable in frontend/v1 scope",
}

// desugarTypesEqual compares two types structurally, tolerating distinct
// *types.TypeParam objects for the same type parameter (name + equivalent
// constraint). types.Identical fails there: a generic function and its
// __-helper each declare their own type parameter (e.g. Key[Hash
// hash.Hash] vs __mrKey[Hash hash.Hash]), and a pair struct's E and the
// multi-result function's E are likewise distinct objects. The
// comparison recurses into pointer/named/slice/array/map/chan/signature
// structure so a nested parameter (e.g. *Curve[P]) compares equal too.
// Constraints are compared with the same structural rule (not
// types.Identical, which fails on the nested distinct parameters); the
// seen set breaks the cycle a constraint forms with its own parameter
// (P's constraint Point[P] mentions P).
func desugarTypesEqual(a, b types.Type) bool {
	return desugarTypesEqualSeen(a, b, map[[2]types.Type]bool{})
}

func desugarTypesEqualSeen(a, b types.Type, seen map[[2]types.Type]bool) bool {
	if types.Identical(a, b) {
		return true
	}
	if pa, ok := a.(*types.TypeParam); ok {
		pb, ok := b.(*types.TypeParam)
		if !ok {
			return false
		}
		key := [2]types.Type{a, b}
		if seen[key] {
			return true
		}
		seen[key] = true
		return pa.Obj().Name() == pb.Obj().Name() && desugarTypesEqualSeen(pa.Constraint(), pb.Constraint(), seen)
	}
	switch ta := a.(type) {
	case *types.Pointer:
		tb, ok := b.(*types.Pointer)
		return ok && desugarTypesEqualSeen(ta.Elem(), tb.Elem(), seen)
	case *types.Named:
		tb, ok := b.(*types.Named)
		if !ok || ta.Obj() != tb.Obj() {
			return false
		}
		if ta.TypeArgs().Len() != tb.TypeArgs().Len() {
			return false
		}
		for i := 0; i < ta.TypeArgs().Len(); i++ {
			if !desugarTypesEqualSeen(ta.TypeArgs().At(i), tb.TypeArgs().At(i), seen) {
				return false
			}
		}
		return true
	case *types.Slice:
		tb, ok := b.(*types.Slice)
		return ok && desugarTypesEqualSeen(ta.Elem(), tb.Elem(), seen)
	case *types.Array:
		tb, ok := b.(*types.Array)
		return ok && ta.Len() == tb.Len() && desugarTypesEqualSeen(ta.Elem(), tb.Elem(), seen)
	case *types.Map:
		tb, ok := b.(*types.Map)
		return ok && desugarTypesEqualSeen(ta.Key(), tb.Key(), seen) && desugarTypesEqualSeen(ta.Elem(), tb.Elem(), seen)
	case *types.Chan:
		tb, ok := b.(*types.Chan)
		return ok && ta.Dir() == tb.Dir() && desugarTypesEqualSeen(ta.Elem(), tb.Elem(), seen)
	case *types.Signature:
		tb, ok := b.(*types.Signature)
		if !ok || ta.Variadic() != tb.Variadic() {
			return false
		}
		if ta.Params().Len() != tb.Params().Len() || ta.Results().Len() != tb.Results().Len() {
			return false
		}
		for i := 0; i < ta.Params().Len(); i++ {
			if !desugarTypesEqualSeen(ta.Params().At(i).Type(), tb.Params().At(i).Type(), seen) {
				return false
			}
		}
		for i := 0; i < ta.Results().Len(); i++ {
			if !desugarTypesEqualSeen(ta.Results().At(i).Type(), tb.Results().At(i).Type(), seen) {
				return false
			}
		}
		return true
	}
	return false
}

// desugarArgMatches reports whether the desugar may pass arg (the
// multi-result call's argument, receiver prepended for methods) to a
// helper parameter of type param:
//   - assignability (the hash helpers take the hash.Hash interface while
//     the call passes the concrete digest);
//   - generic-aware structural equality (distinct TypeParam objects);
//   - interface downcast: arg is an interface value and param is a
//     concrete type implementing it (io/fs DirEntry.Info's helper takes
//     *os.dirEntry while the call passes the DirEntry interface; the
//     runtime value is the concrete type, so the helper must implement
//     the interface — an unrelated concrete type fails here).
func desugarArgMatches(arg, param types.Type) bool {
	if types.AssignableTo(arg, param) {
		return true
	}
	if desugarTypesEqual(arg, param) {
		return true
	}
	iface := desugarInterfaceOf(arg)
	if iface == nil {
		return false
	}
	return types.Implements(param, iface)
}

// desugarInterfaceOf returns the interface type of t when t is an
// interface (directly or via a named type), or nil otherwise.
func desugarInterfaceOf(t types.Type) *types.Interface {
	if iface, ok := t.(*types.Interface); ok {
		return iface
	}
	if nt, ok := t.(*types.Named); ok {
		if iface, ok := nt.Underlying().(*types.Interface); ok {
			return iface
		}
	}
	return nil
}

// loadDesugarPackage loads a synthetic package, returning the panic as an
// error instead of crashing the test binary: buildSyntheticPackage panics
// on a missing source or a type error, and both cases are test data here
// (retired packages, archive/zip, mime/multipart), not test bugs.
func loadDesugarPackage(path string) (sp *syntheticPackage, errMsg string) {
	defer func() {
		if r := recover(); r != nil {
			sp = nil
			errMsg = fmt.Sprint(r)
		}
	}()
	sp = getSyntheticPackage(path)
	return sp, ""
}

// lookupDesugarMRFunc finds the multi-result function (package-level) or
// method (on the named receiver type) in a loaded synthetic package.
// Interface receivers are looked up in the interface's own method list:
// the pointer method set is empty for interface named types, so a plain
// method-set lookup misses e.g. crypto/cipher AEAD.Open and
// io/fs DirEntry.Info.
func lookupDesugarMRFunc(sp *syntheticPackage, name, recvType string) *types.Func {
	if recvType == "" {
		fn, _ := sp.pkg.Scope().Lookup(name).(*types.Func)
		return fn
	}
	tnobj := sp.pkg.Scope().Lookup(recvType)
	if tnobj == nil {
		return nil
	}
	nt, ok := tnobj.Type().(*types.Named)
	if !ok {
		return nil
	}
	if iface, ok := nt.Underlying().(*types.Interface); ok {
		for i := 0; i < iface.NumMethods(); i++ {
			if m := iface.Method(i); m.Name() == name {
				return m
			}
		}
		return nil
	}
	ms := types.NewMethodSet(types.NewPointer(nt))
	sel := ms.Lookup(sp.pkg, name)
	if sel == nil {
		return nil
	}
	fn, _ := sel.Obj().(*types.Func)
	return fn
}

// desugarRecvBase returns the base named type of a FuncDecl's receiver
// ("rtype" for func (t *rtype) M, "" for package-level funcs), unwrapping
// pointer and generic instantiation syntax.
func desugarRecvBase(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	e := fd.Recv.List[0].Type
	if se, ok := e.(*ast.StarExpr); ok {
		e = se.X
	}
	for {
		if ie, ok := e.(*ast.IndexExpr); ok {
			e = ie.X
			continue
		}
		if ie, ok := e.(*ast.IndexListExpr); ok {
			e = ie.X
			continue
		}
		break
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "?"
}

// parseDesugarSource parses a synthetic source with the same repairs
// buildSyntheticPackage applies (restoreRawStrings,
// hoistSyntheticImports). Parse-only: sources with type errors
// (archive/zip, mime/multipart) still parse.
func parseDesugarSource(path string) (file *ast.File, fset *token.FileSet, errMsg string) {
	src, ok := syntheticSources[path]
	if !ok {
		return nil, nil, "no synthetic source for " + path
	}
	src = restoreRawStrings(src)
	src = hoistSyntheticImports(src)
	fset = token.NewFileSet()
	file, err := parser.ParseFile(fset, path+".go", src, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return nil, nil, "parse error: " + err.Error()
	}
	return file, fset, ""
}

func astFindFunc(file *ast.File, name, recvType string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != name {
			continue
		}
		if desugarRecvBase(fd) == recvType {
			return fd
		}
	}
	return nil
}

func astCountFields(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	n := 0
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			n++
		} else {
			n += len(f.Names)
		}
	}
	return n
}

// astPairFields returns the field names of a named struct type in order,
// or ok=false when the type is missing, not a struct, or has an embedded
// (unnamed) field, which a pair struct must not have.
func astPairFields(file *ast.File, pairType string) (fields []string, ok bool) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != pairType {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return nil, false
			}
			for _, f := range st.Fields.List {
				if len(f.Names) == 0 {
					return nil, false
				}
				for _, n := range f.Names {
					fields = append(fields, n.Name)
				}
			}
			return fields, true
		}
	}
	return nil, false
}

// astIsStub reports whether a FuncDecl body is a single panic statement
// (the fail-fast stub form).
func astIsStub(fd *ast.FuncDecl) bool {
	if fd.Body == nil || len(fd.Body.List) != 1 {
		return false
	}
	es, ok := fd.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "panic"
}

// astPanicMessage returns the stub's panic message: a string literal, or
// a package-level const/var identifier resolved to its string literal in
// the same file (syscall's panic(failAccept4) form). ok=false when the
// panic argument is neither.
func astPanicMessage(fd *ast.FuncDecl, file *ast.File) (msg string, ok bool) {
	if !astIsStub(fd) {
		return "", false
	}
	es := fd.Body.List[0].(*ast.ExprStmt)
	call := es.X.(*ast.CallExpr)
	if len(call.Args) != 1 {
		return "", false
	}
	if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return "", false
		}
		return s, true
	}
	if id, ok := call.Args[0].(*ast.Ident); ok {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) != 1 {
					continue
				}
				for _, n := range vs.Names {
					if n.Name != id.Name {
						continue
					}
					if lit, ok := vs.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						s, err := strconv.Unquote(lit.Value)
						if err != nil {
							return "", false
						}
						return s, true
					}
				}
			}
		}
	}
	return "", false
}

// checkDesugarRowForward verifies one concrete table row. It mirrors the
// classifier's helper-package resolution (already applied by
// allDesugarConcreteRows) and the desugar's call shape: the helper is
// called with the multi-result call's [receiver,] arguments.
func checkDesugarRowForward(t *testing.T, row desugarRow) {
	t.Helper()
	// Well-formedness: a row with an empty field can never resolve.
	if row.pkgPath == "" || row.name == "" || row.helper == "" || row.pairType == "" {
		t.Errorf("%s: empty pkgPath/name/helper/pairType in row %+v", row.display(), row)
		return
	}
	if len(row.pairFields) == 0 {
		t.Errorf("%s: empty pairFields", row.display())
		return
	}
	seen := map[string]bool{}
	for _, f := range row.pairFields {
		if f == "" || seen[f] {
			t.Errorf("%s: bad pairFields %v", row.display(), row.pairFields)
			return
		}
		seen[f] = true
	}
	// Retired package: no synthetic source; the row is covered by the
	// desugarRetiredPkgs exemption (verified still accurate here).
	if _, hasSrc := syntheticSources[row.pkgPath]; !hasSrc {
		if reason, ok := desugarRetiredPkgs[row.pkgPath]; ok {
			t.Logf("%s: retired package, exempt (%s)", row.display(), reason)
			return
		}
		t.Errorf("%s: declaring package %q has no synthetic source and is not in desugarRetiredPkgs", row.display(), row.pkgPath)
		return
	}
	sp, errMsg := loadDesugarPackage(row.pkgPath)
	if errMsg != "" {
		checkDesugarRowForwardAST(t, row, errMsg)
		return
	}
	mrFn := lookupDesugarMRFunc(sp, row.name, row.recvType)
	if mrFn == nil {
		t.Errorf("%s: multi-result function/method missing in synthetic %s", row.display(), row.pkgPath)
		return
	}
	sig, ok := mrFn.Type().(*types.Signature)
	if !ok {
		t.Errorf("%s: synthetic %s.%s has non-signature type %v", row.display(), row.pkgPath, row.name, mrFn.Type())
		return
	}
	if sig.Results().Len() != len(row.pairFields) {
		t.Errorf("%s: synthetic %s has %d results, want %d (pairFields)", row.display(), row.name, sig.Results().Len(), len(row.pairFields))
		return
	}
	hsp, errMsg := loadDesugarPackage(row.helperPkg)
	if errMsg != "" {
		t.Errorf("%s: helper package %q does not load: %s", row.display(), row.helperPkg, errMsg)
		return
	}
	hobj := hsp.pkg.Scope().Lookup(row.helper)
	helper, ok := hobj.(*types.Func)
	if !ok {
		t.Errorf("%s: helper %s.%s missing from package scope (got %T)", row.display(), row.helperPkg, row.helper, hobj)
		return
	}
	hsig, ok := helper.Type().(*types.Signature)
	if !ok {
		t.Errorf("%s: helper %s has non-signature type %v", row.display(), row.helper, helper.Type())
		return
	}
	// The helper takes the call's [receiver,] arguments positionally.
	var wantParams []types.Type
	if sig.Recv() != nil {
		wantParams = append(wantParams, sig.Recv().Type())
	}
	for i := 0; i < sig.Params().Len(); i++ {
		wantParams = append(wantParams, sig.Params().At(i).Type())
	}
	if hsig.Params().Len() != len(wantParams) {
		t.Errorf("%s: helper %s takes %d params, multi-result call has %d ([recv,] params)", row.display(), row.helper, hsig.Params().Len(), len(wantParams))
		return
	}
	for i, wp := range wantParams {
		if !desugarArgMatches(wp, hsig.Params().At(i).Type()) {
			t.Errorf("%s: helper %s param %d type %v does not accept call arg type %v", row.display(), row.helper, i, hsig.Params().At(i).Type(), wp)
			return
		}
	}
	if hsig.Results().Len() != 1 {
		t.Errorf("%s: helper %s has %d results, want 1 (the pair)", row.display(), row.helper, hsig.Results().Len())
		return
	}
	pobj := hsp.pkg.Scope().Lookup(row.pairType)
	if pobj == nil {
		t.Errorf("%s: pair type %s.%s missing from package scope", row.display(), row.helperPkg, row.pairType)
		return
	}
	nt, ok := pobj.Type().(*types.Named)
	if !ok {
		t.Errorf("%s: pair %s has type %T, want named struct", row.display(), row.pairType, pobj.Type())
		return
	}
	st, ok := nt.Underlying().(*types.Struct)
	if !ok {
		t.Errorf("%s: pair %s underlying is %T, want struct", row.display(), row.pairType, nt.Underlying())
		return
	}
	if st.NumFields() != len(row.pairFields) {
		t.Errorf("%s: pair %s has %d fields, want %d", row.display(), row.pairType, st.NumFields(), len(row.pairFields))
		return
	}
	for i, fname := range row.pairFields {
		f := st.Field(i)
		if f.Name() != fname {
			t.Errorf("%s: pair %s field %d is %q, want %q", row.display(), row.pairType, i, f.Name(), fname)
			return
		}
		if !desugarTypesEqual(f.Type(), sig.Results().At(i).Type()) {
			t.Errorf("%s: pair %s field %q type %v != result %d type %v", row.display(), row.pairType, fname, f.Type(), i, sig.Results().At(i).Type())
			return
		}
	}
}

// checkDesugarRowForwardAST verifies one concrete row against sources
// that parse but do not type-check (archive/zip, mime/multipart):
// existence + arity + pair field names/order, without type identity.
func checkDesugarRowForwardAST(t *testing.T, row desugarRow, loadErr string) {
	t.Helper()
	declFile, _, errMsg := parseDesugarSource(row.pkgPath)
	if errMsg != "" {
		t.Errorf("%s: declaring package %q does not load (%s) and does not parse (%s)", row.display(), row.pkgPath, loadErr, errMsg)
		return
	}
	mrFd := astFindFunc(declFile, row.name, row.recvType)
	if mrFd == nil {
		t.Errorf("%s: multi-result function/method missing in synthetic source %s (package does not type-check: %s)", row.display(), row.pkgPath, loadErr)
		return
	}
	if n := astCountFields(mrFd.Type.Results); n != len(row.pairFields) {
		t.Errorf("%s: synthetic %s has %d results, want %d (pairFields) (package does not type-check: %s)", row.display(), row.name, n, len(row.pairFields), loadErr)
		return
	}
	helperFile := declFile
	if row.helperPkg != row.pkgPath {
		var errMsg2 string
		helperFile, _, errMsg2 = parseDesugarSource(row.helperPkg)
		if errMsg2 != "" {
			t.Errorf("%s: helper package %q does not parse: %s", row.display(), row.helperPkg, errMsg2)
			return
		}
	}
	helperFd := astFindFunc(helperFile, row.helper, "")
	if helperFd == nil {
		t.Errorf("%s: helper %s missing in synthetic source %s (package does not type-check: %s)", row.display(), row.helper, row.helperPkg, loadErr)
		return
	}
	// Arity only (no types available): [receiver,] params.
	wantParams := astCountFields(mrFd.Type.Params)
	if row.recvType != "" {
		wantParams++
	}
	if got := astCountFields(helperFd.Type.Params); got != wantParams {
		t.Errorf("%s: helper %s takes %d params, multi-result call has %d ([recv,] params) (package does not type-check: %s)", row.display(), row.helper, got, wantParams, loadErr)
		return
	}
	if n := astCountFields(helperFd.Type.Results); n != 1 {
		t.Errorf("%s: helper %s has %d results, want 1 (the pair) (package does not type-check: %s)", row.display(), row.helper, n, loadErr)
		return
	}
	fields, ok := astPairFields(helperFile, row.pairType)
	if !ok {
		t.Errorf("%s: pair type %s missing or not a struct in synthetic source %s (package does not type-check: %s)", row.display(), row.pairType, row.helperPkg, loadErr)
		return
	}
	if len(fields) != len(row.pairFields) {
		t.Errorf("%s: pair %s has %d fields %v, want %d %v (package does not type-check: %s)", row.display(), row.pairType, len(fields), fields, len(row.pairFields), row.pairFields, loadErr)
		return
	}
	for i, fname := range row.pairFields {
		if fields[i] != fname {
			t.Errorf("%s: pair %s field %d is %q, want %q (package does not type-check: %s)", row.display(), row.pairType, i, fields[i], fname, loadErr)
			return
		}
	}
}

// checkDesugarIfaceRowForward verifies one m8IfaceMultiResultMethods row:
// the pair struct must exist in the classifier-resolved pair package with
// exactly the listed fields in order. Field types are enforced by
// classifyIfaceMultiResultCall structurally at each call site, so this
// test pins existence + names/order (the same strength the issue asks
// for: "matching field names/order in the synthetic source").
func checkDesugarIfaceRowForward(t *testing.T, row desugarIfaceRow) {
	t.Helper()
	id := "iface " + row.pkgPath + "." + row.method
	if row.pairType == "" || len(row.pairFields) == 0 {
		t.Errorf("%s: empty pairType/pairFields", id)
		return
	}
	if _, hasSrc := syntheticSources[row.pairPkg]; !hasSrc {
		if reason, ok := desugarRetiredPkgs[row.pairPkg]; ok {
			t.Logf("%s: retired package, exempt (%s)", id, reason)
			return
		}
		t.Errorf("%s: pair package %q has no synthetic source and is not in desugarRetiredPkgs", id, row.pairPkg)
		return
	}
	sp, errMsg := loadDesugarPackage(row.pairPkg)
	if errMsg != "" {
		t.Errorf("%s: pair package %q does not load: %s", id, row.pairPkg, errMsg)
		return
	}
	pobj := sp.pkg.Scope().Lookup(row.pairType)
	if pobj == nil {
		t.Errorf("%s: pair type %s.%s missing from package scope", id, row.pairPkg, row.pairType)
		return
	}
	nt, ok := pobj.Type().(*types.Named)
	if !ok {
		t.Errorf("%s: pair %s has type %T, want named struct", id, row.pairType, pobj.Type())
		return
	}
	st, ok := nt.Underlying().(*types.Struct)
	if !ok {
		t.Errorf("%s: pair %s underlying is %T, want struct", id, row.pairType, nt.Underlying())
		return
	}
	if st.NumFields() != len(row.pairFields) {
		t.Errorf("%s: pair %s has %d fields, want %d", id, row.pairType, st.NumFields(), len(row.pairFields))
		return
	}
	for i, fname := range row.pairFields {
		if st.Field(i).Name() != fname {
			t.Errorf("%s: pair %s field %d is %q, want %q", id, row.pairType, i, st.Field(i).Name(), fname)
			return
		}
	}
}

func TestDesugarTablesExhaustive(t *testing.T) {
	// The test never passes vacuously: every table must be non-empty.
	counts := map[string]int{
		"m7": len(m7MultiResultFuncs), "m8": len(m8MultiResultFuncs),
		"os": len(osMultiResultFuncs), "m9": len(m9MultiResultFuncs),
		"io": len(ioMultiResultFuncs), "iface": len(m8IfaceMultiResultMethods),
	}
	for name, n := range counts {
		if n == 0 {
			t.Fatalf("desugar table %s is empty (test would pass vacuously)", name)
		}
	}
	t.Logf("desugar tables: m7=%d m8=%d os=%d m9=%d io=%d iface=%d",
		counts["m7"], counts["m8"], counts["os"], counts["m9"], counts["io"], counts["iface"])

	rows := allDesugarConcreteRows()
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.table+"|"+r.key()]++
	}
	for k, c := range seen {
		if c > 1 {
			t.Errorf("duplicate %s entry x%d (the classifier takes the first match; the rest are dead)", k, c)
		}
	}
	for _, r := range rows {
		r := r
		t.Run(r.display(), func(t *testing.T) {
			checkDesugarRowForward(t, r)
		})
	}

	iseen := map[string]int{}
	for _, r := range allDesugarIfaceRows() {
		iseen[r.pkgPath+"|"+r.method]++
	}
	for k, c := range iseen {
		if c > 1 {
			t.Errorf("duplicate iface entry %s x%d (the classifier takes the first match; the rest are dead)", k, c)
		}
	}
	for _, r := range allDesugarIfaceRows() {
		r := r
		t.Run("iface "+r.pkgPath+"."+r.method, func(t *testing.T) {
			checkDesugarIfaceRowForward(t, r)
		})
	}
}

// desugarReverseExemptions maps a stub-bodied multi-result function with
// no table row to the scope reason it stays a fail-fast stub. Key is
// "pkg|name|recv". Every entry's reason names the condition in the stub's
// own panic message, and the test verifies that marker is still present
// (desugarScopeMarker) and fails on an exemption no stub consumes, so
// the list cannot rot silently: adding a row for an exempted function, or
// giving it a real body, requires removing the exemption here.
//
// Deliberately NOT exempted (the test fails on them until a follow-up
// lane adds the rows): the io/fs package-level functions and methods,
// os.dirFS methods, and crypto/tls Dialer.Dial/DialContext, whose stubs
// say "unreachable (Appendix-M desugar did not fire...)" and whose
// __-helpers and pair structs already exist.
var desugarReverseExemptions = map[string]string{
	// bytes: "not in M7/M8 scope (multi-result calls need WS D; needs
	// 2-result interface dispatch, WS D; re-test when WS D lands)".
	"bytes|Peek|Buffer": "M7 scope", "bytes|ReadByte|Buffer": "M7 scope",
	"bytes|ReadBytes|Buffer": "M7 scope", "bytes|ReadFrom|Buffer": "M8 scope",
	"bytes|ReadRune|Buffer": "M7 scope", "bytes|ReadString|Buffer": "M7 scope",
	"bytes|WriteRune|Buffer": "M7 scope", "bytes|WriteTo|Buffer": "M8 scope",
	"bytes|readSlice|Buffer": "M7 scope",
	"bytes|ReadAt|Reader":    "M7 scope", "bytes|ReadByte|Reader": "M7 scope",
	"bytes|ReadRune|Reader": "M7 scope", "bytes|Seek|Reader": "M7 scope",
	"bytes|WriteTo|Reader": "M8 scope",
	// crypto/cipher: "no entropy (re-test: syscall entropy)".
	"crypto/cipher|NewGCMWithRandomNonce|": "no entropy source",
	// crypto/sha3: "multi-result call needs the mrM8 desugar (re-test: WS D)".
	"crypto/sha3|AppendBinary|SHA3":   "mrM8 desugar / WS D",
	"crypto/sha3|AppendBinary|SHAKE":  "mrM8 desugar / WS D",
	"crypto/sha3|MarshalBinary|SHAKE": "mrM8 desugar / WS D",
	// encoding/asn1: "not in M7 scope (pointer store needs WS D; needs
	// reflect; re-test when WS D / reflect lands)".
	"encoding/asn1|MarshalWithParams|":   "M7 scope (reflect)",
	"encoding/asn1|Unmarshal|":           "M7 scope (pointer store needs WS D)",
	"encoding/asn1|UnmarshalWithParams|": "M7 scope (reflect)",
	// encoding/base32: "not in M8 scope (needs 2-result interface
	// dispatch, WS D; re-test when WS D lands)".
	"encoding/base32|Read|decoder":  "M8 scope (interface dispatch)",
	"encoding/base32|Write|encoder": "M8 scope (interface dispatch)",
	// encoding/binary: "not in M7 scope (needs reflect; re-test when
	// reflect lands)".
	"encoding/binary|Append|": "M7 scope (reflect)",
	"encoding/binary|Decode|": "M7 scope (reflect)",
	"encoding/binary|Encode|": "M7 scope (reflect)",
	// io: "not in M8 scope (needs 2-result interface dispatch, WS D;
	// re-test when WS D lands)"; eofReader.Read and pipe.read/write say
	// "not in M8 scope (multi-result calls need WS D; re-test when it lands)".
	"io|Write|OffsetWriter":        "M8 scope (interface dispatch)",
	"io|WriteAt|OffsetWriter":      "M8 scope (interface dispatch)",
	"io|Read|SectionReader":        "M8 scope (interface dispatch)",
	"io|ReadAt|SectionReader":      "M8 scope (interface dispatch)",
	"io|Read|teeReader":            "M8 scope (interface dispatch)",
	"io|ReadFrom|discard":          "M8 scope (interface dispatch)",
	"io|Read|eofReader":            "M8 scope (WS D)",
	"io|Read|multiReader":          "M8 scope (interface dispatch)",
	"io|WriteTo|multiReader":       "M8 scope (interface dispatch)",
	"io|Write|multiWriter":         "M8 scope (interface dispatch)",
	"io|WriteString|multiWriter":   "M8 scope (interface dispatch)",
	"io|WriteTo|nopCloserWriterTo": "M8 scope (interface dispatch)",
	"io|read|pipe":                 "M8 scope (WS D)", "io|write|pipe": "M8 scope (WS D)",
	// math: "not in M7 scope (multi-result calls need WS D; re-test when
	// it lands)". NOTE: math.Modf's helper __modf already exists and is
	// used internally (synth_math.go); the row is a one-liner when the
	// scope decision lifts. Lgamma has no helper.
	"math|Lgamma|": "M7 scope", "math|Modf|": "M7 scope",
	// math/big: "Scan not supported in frontend/v1 scope (WS D)".
	"math/big|ReadRune|byteReader": "v1 scope (Scan)",
	"math/big|Token|byteReader":    "v1 scope (Scan)",
	// net: "not in M9 scope (no resolver; re-test when DNS lands)" and
	// "not in M9 scope (re-test when interface listing lands)".
	"net|InterfaceAddrs|": "M9 scope (interface listing)",
	"net|LookupHost|":     "M9 scope (no resolver)",
	"net|LookupIP|":       "M9 scope (no resolver)",
	"net|LookupPort|":     "M9 scope (no resolver)",
	// net/url: "not in M9 scope (re-test when WS D lands)".
	"net/url|AppendBinary|URL":  "M9 scope",
	"net/url|MarshalBinary|URL": "M9 scope",
	// os: FindProcess "not in M8 scope (re-test: process-spawning
	// milestone)"; getShellName "not in M8 scope (multi-result; re-test: WS D)".
	"os|FindProcess|":  "M8 scope (process-spawning milestone)",
	"os|getShellName|": "M8 scope (WS D)",
	// os.Root: "not in M8 scope (needs openat2(2); re-test: Panama
	// openat2 or a vetted root-emulation design)".
	"os|Create|Root": "M8 scope (openat2)", "os|Lstat|Root": "M8 scope (openat2)",
	"os|Open|Root": "M8 scope (openat2)", "os|OpenFile|Root": "M8 scope (openat2)",
	"os|ReadDir|Root": "M8 scope (openat2)", "os|ReadFile|Root": "M8 scope (openat2)",
	"os|Stat|Root": "M8 scope (openat2)",
	// strings: "not in M8 scope (needs 2-result interface dispatch, WS D;
	// re-test when WS D lands)".
	"strings|WriteTo|Reader":       "M8 scope (interface dispatch)",
	"strings|WriteString|Replacer": "M8 scope (interface dispatch)",
	// syscall: panic(failX) where the fail* constants read "temporary
	// until ASM backend (his 2026-10-15 order)"; no __-helper exists.
	"syscall|Accept4|":               "temporary until ASM backend",
	"syscall|Getpeername|":           "temporary until ASM backend",
	"syscall|Getsockname|":           "temporary until ASM backend",
	"syscall|NetlinkRIB|":            "temporary until ASM backend",
	"syscall|Openat|":                "temporary until ASM backend",
	"syscall|ParseNetlinkMessage|":   "temporary until ASM backend",
	"syscall|ParseNetlinkRouteAttr|": "temporary until ASM backend",
	// archive/zip + mime/multipart: "not in M7 scope (multi-result calls
	// need WS D; re-test when it lands)". Both are also exclusions in
	// .github/scripts/check_mr_table_subset.py, which mechanically
	// verifies the reachability claim there (unexported; findBodyOffset's
	// internal calls are rewritten in-source to __zipFilefindBodyOffset;
	// readForm is called only from __mpReaderReadForm).
	"archive/zip|findBodyOffset|File": "M7 scope (check_mr_table_subset exclusion)",
	"mime/multipart|readForm|Reader":  "M7 scope (check_mr_table_subset exclusion)",
	// mime/multipart readMIMEHeader: "not in M9 scope". No __-helper
	// exists, so no row can be written without also writing a helper +
	// pair. NOTE: unlike the other exemptions this one has a live
	// internal call site (synth_mime_multipart.go:221, called from the
	// multipart Reader); lifting it needs the helper, not just a row.
	"mime/multipart|readMIMEHeader|": "M9 scope (no helper)",
}

// desugarScopeMarkers are phrases a stub's panic message must contain
// for its reverse exemption to stay valid. A stub whose message stops
// declaring a scope limitation (e.g. it starts saying "desugar did not
// fire") no longer qualifies for exemption and must gain a row.
var desugarScopeMarkers = []string{
	"scope", "WS D", "no entropy", "needs openat2",
	"process-spawning", "temporary until ASM backend",
	"needs the mrM8 desugar",
}

func desugarHasScopeMarker(msg string) bool {
	for _, m := range desugarScopeMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

func TestDesugarTablesReverseStubCoverage(t *testing.T) {
	// Coverage keys from every concrete table plus the M5 static table.
	covered := map[string]bool{}
	anyRecv := map[string]bool{}
	for _, r := range allDesugarConcreteRows() {
		covered[r.key()] = true
	}
	for _, me := range m5StaticMREntries {
		if me.anyRecv {
			anyRecv[me.pkgPath+"|"+me.name] = true
		} else {
			covered[me.pkgPath+"|"+me.name+"|"+me.recvType] = true
		}
	}
	isCovered := func(pkg, name, recv string) bool {
		return covered[pkg+"|"+name+"|"+recv] || anyRecv[pkg+"|"+name]
	}

	// Packages under reverse enumeration: the declaring packages of the
	// concrete tables. Other synthetic packages' multi-result functions
	// are served by other mechanisms (m5 entries, runtime stack methods,
	// user-MR transform) or are out of this issue's scope.
	pkgs := map[string]bool{}
	for _, r := range allDesugarConcreteRows() {
		pkgs[r.pkgPath] = true
	}
	var names []string
	for p := range pkgs {
		names = append(names, p)
	}
	sort.Strings(names)

	used := map[string]bool{}
	checkStub := func(pkg, name, recv, panicMsg string, isStub bool) {
		t.Helper()
		id := pkg + "|" + name + "|" + recv
		if !isStub {
			return // real body: the exporter transforms it; no row needed.
		}
		if isCovered(pkg, name, recv) {
			return
		}
		reason, exempt := desugarReverseExemptions[id]
		if !exempt {
			t.Errorf("stub-bodied multi-result %s.%s(%s) has no desugar row and no reverse exemption (panic %q): add a row or an exemption with a scope reason",
				pkg, recv, name, panicMsg)
			return
		}
		used[id] = true
		if !desugarHasScopeMarker(panicMsg) {
			t.Errorf("reverse exemption for stub %s.%s(%s) is stale: panic %q no longer declares a scope limitation (exemption reason: %s)",
				pkg, recv, name, panicMsg, reason)
		}
	}

	for _, p := range names {
		sp, errMsg := loadDesugarPackage(p)
		if errMsg != "" {
			// No synthetic source (retired): nothing to enumerate.
			if _, hasSrc := syntheticSources[p]; !hasSrc {
				t.Logf("reverse: package %s retired, skipped (%s)", p, errMsg)
				continue
			}
			// Source exists but does not type-check: AST-only
			// enumeration (archive/zip, mime/multipart).
			file, _, perr := parseDesugarSource(p)
			if perr != "" {
				t.Errorf("reverse: package %s does not load (%s) and does not parse (%s)", p, errMsg, perr)
				continue
			}
			seen := map[string]bool{}
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				recv := desugarRecvBase(fd)
				key := p + "|" + fd.Name.Name + "|" + recv
				if seen[key] {
					continue
				}
				seen[key] = true
				if astCountFields(fd.Type.Results) < 2 {
					continue
				}
				msg, _ := astPanicMessage(fd, file)
				checkStub(p, fd.Name.Name, recv, msg, astIsStub(fd))
			}
			continue
		}
		seen := map[string]bool{}
		for _, file := range sp.files {
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				recv := desugarRecvBase(fd)
				key := p + "|" + fd.Name.Name + "|" + recv
				if seen[key] {
					continue
				}
				seen[key] = true
				// Resolve the declared function for an exact result count
				// (AST counting is unreliable for generic signatures).
				fn := lookupDesugarMRFunc(sp, fd.Name.Name, recv)
				if fn == nil {
					continue
				}
				sig, ok := fn.Type().(*types.Signature)
				if !ok || sig.Results().Len() < 2 {
					continue
				}
				msg, _ := astPanicMessage(fd, file)
				checkStub(p, fd.Name.Name, recv, msg, astIsStub(fd))
			}
		}
	}
	for id, reason := range desugarReverseExemptions {
		if !used[id] {
			t.Errorf("reverse exemption %q (%s) was not consumed by any stub: the function gained a row or a real body; remove the exemption", id, reason)
		}
	}
}
