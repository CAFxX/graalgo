package main

// Exhaustive desugar-table consistency test (F1/F10, REVIEW-2026-11-06-15).
//
// The five helper/pair desugar tables (m7, io, os, m8, m9) plus the
// interface-dispatch table (m8Iface) and the M5 static table are swept
// row by row, asserting:
//
//  1. no duplicate (pkg, name, recv) keys — a duplicate row is dead
//     weight that hides drift (F6: 7 duplicated elliptic/ecdh iface
//     rows + 1 duplicated crypto/sha3 SHAKE row, both deleted by the
//     F1 lane; this test pins them gone);
//  2. every helper exists in a synthetic package scope, or the package
//     is disk-owned with a generated helper — the exact failure F1
//     exposed: 17 m7 rows named __-helpers (__httpGet, __newRequest,
//     __clientDo, ...) that existed only in the deleted synth_net_http.go,
//     so classifyM7MultiResultCall called getSyntheticPackage("net/http")
//     and panicked before the disk table was ever consulted;
//  3. pair-field arity matches the real signature — the pair struct has
//     exactly the listed fields in order, and the multi-result function
//     (or method) has exactly that many results.
//
// Verification is two-tier per package. Most synthetic packages load
// cleanly, so the check runs against the type-checked package scope
// (the strongest form). Two packages do not type-check on this base —
// archive/zip (time.FixedZone) and mime/multipart (os used as a value)
// — both with carried fixes on other branches; for those the check
// falls back to the parsed synthetic source (AST-level existence +
// arity), which still catches F1-class staleness (a missing helper or
// pair, a wrong arity) without depending on the package type-checking.
// A package that fails to load for any OTHER reason fails the test:
// the fallback is keyed to the two known-broken packages, not a
// blanket excuse.
//
// Disk-owned rows (helper package absent from syntheticSources) are
// verified against the built disk package: the disk spec must exist
// with the generated helper name and matching pair arity, and the
// helper + pair must exist in the disk package scope. No such rows
// remain after the F1 deletion; the disk branch is pinned explicitly
// by TestNetHTTPDiskOwned (net/http + net/http/httptest).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// desugarRow is one normalized desugar-table row across all tables.
type desugarRow struct {
	table     string // "m7", "io", "os", "m8", "m9", "iface"
	pkgPath   string // declaring package (where the MR func/method lives)
	name      string // function/method name
	recvType  string // receiver named type ("" = package-level func)
	helper    string // __-helper name ("" for iface rows: pair only)
	helperPkg string // package holding the helper + pair type
	pairType  string // pair/triple struct type name
	fields    []string
}

func allDesugarRows() []desugarRow {
	var rows []desugarRow
	for i := range m7MultiResultFuncs {
		mf := &m7MultiResultFuncs[i]
		hp := mf.pkgPath
		if ov, ok := m7HelperPkgOverride[mf.pkgPath]; ok {
			hp = ov
		}
		rows = append(rows, desugarRow{"m7", mf.pkgPath, mf.name, mf.recvType, mf.helper, hp, mf.pairType, mf.pairFields})
	}
	for i := range ioMultiResultFuncs {
		mf := &ioMultiResultFuncs[i]
		hp := mf.pkgPath
		if ov, ok := m7HelperPkgOverride[mf.pkgPath]; ok {
			hp = ov
		}
		rows = append(rows, desugarRow{"io", mf.pkgPath, mf.name, mf.recvType, mf.helper, hp, mf.pairType, mf.pairFields})
	}
	for i := range osMultiResultFuncs {
		mf := &osMultiResultFuncs[i]
		hp := mf.helperPkg
		if hp == "" {
			hp = mf.pkgPath
		}
		rows = append(rows, desugarRow{"os", mf.pkgPath, mf.name, mf.recvType, mf.helper, hp, mf.pairType, mf.pairFields})
	}
	for i := range m8MultiResultFuncs {
		mf := &m8MultiResultFuncs[i]
		hp := mf.helperPkg
		if hp == "" {
			hp = mf.pkgPath
		}
		rows = append(rows, desugarRow{"m8", mf.pkgPath, mf.name, mf.recvType, mf.helper, hp, mf.pairType, mf.pairFields})
	}
	for i := range m9MultiResultFuncs {
		mf := &m9MultiResultFuncs[i]
		hp := mf.helperPkg
		if hp == "" {
			hp = mf.pkgPath
		}
		rows = append(rows, desugarRow{"m9", mf.pkgPath, mf.name, mf.recvType, mf.helper, hp, mf.pairType, mf.pairFields})
	}
	for i := range m8IfaceMultiResultMethods {
		m := &m8IfaceMultiResultMethods[i]
		hp := m.pkgPath
		if ov, ok := m7HelperPkgOverride[m.pkgPath]; ok {
			hp = ov
		}
		rows = append(rows, desugarRow{"iface", m.pkgPath, m.method, "", "", hp, m.pairType, m.pairFields})
	}
	return rows
}

// pkgLoad is the cached outcome of loading one synthetic package for
// the sweep: either the type-checked package or the parsed source
// (AST fallback for the two known-broken packages).
type pkgLoad struct {
	sp      *syntheticPackage // non-nil when the package loaded
	astFile *ast.File         // non-nil when only the AST fallback applies
	astErr  error
	failed  bool // load panicked AND no AST fallback applies
	panic   any  // the load panic, when failed
	astOnly bool // true when the AST fallback is in use
}

var knownASTOnlyPkgs = map[string]string{
	// package -> why it does not type-check on this base (carried fix).
	"archive/zip":    "time.FixedZone (branch review-1105-zip-fixedzone)",
	"mime/multipart": "os used as a value (branch review-1105-multipart-typeerr)",
}

func loadSyntheticForSweep(t *testing.T, cache map[string]*pkgLoad, path string) *pkgLoad {
	t.Helper()
	if pl, ok := cache[path]; ok {
		return pl
	}
	pl := &pkgLoad{}
	cache[path] = pl
	func() {
		defer func() {
			if r := recover(); r != nil {
				pl.panic = r
				pl.failed = true
			}
		}()
		pl.sp = getSyntheticPackage(path)
	}()
	if pl.failed {
		if why, ok := knownASTOnlyPkgs[path]; ok {
			// AST fallback: parse the synthetic source (no type-check).
			// A parse failure here is a test failure, not a skip.
			src, inSynth := syntheticSources[path]
			if !inSynth {
				t.Fatalf("package %q failed to load (%v) and has no synthetic source for the AST fallback", path, pl.panic)
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path+".go", src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("AST fallback parse of synthetic %q failed: %v (load panic was: %v)", path, err, pl.panic)
			}
			pl.astFile = f
			pl.astOnly = true
			pl.failed = false
			t.Logf("package %q uses the AST fallback (does not type-check: %s)", path, why)
		}
	}
	return pl
}

// checkPairStruct verifies the pair type in a loaded synthetic package:
// named struct with exactly the listed fields in order.
func checkPairStruct(t *testing.T, sp *syntheticPackage, row desugarRow) {
	t.Helper()
	name := row.helperPkg + "." + row.pairType
	pobj := sp.pkg.Scope().Lookup(row.pairType)
	if pobj == nil {
		t.Fatalf("%s: pair type %s missing from package scope (F1-class stale row: the pair the table names does not exist)", row.table, name)
	}
	nt, ok := pobj.Type().(*types.Named)
	if !ok {
		t.Fatalf("%s: pair type %s has type %T, want named struct", row.table, name, pobj.Type())
	}
	st, ok := nt.Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("%s: pair type %s underlying is %T, want struct", row.table, name, nt.Underlying())
	}
	if st.NumFields() != len(row.fields) {
		t.Fatalf("%s: pair %s has %d fields, want %d (%v)", row.table, name, st.NumFields(), len(row.fields), row.fields)
	}
	for i, fname := range row.fields {
		if st.Field(i).Name() != fname {
			t.Fatalf("%s: pair %s field %d is %q, want %q", row.table, name, i, st.Field(i).Name(), fname)
		}
	}
}

// checkPairStructAST is the AST-fallback form of checkPairStruct.
func checkPairStructAST(t *testing.T, f *ast.File, row desugarRow) {
	t.Helper()
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != row.pairType {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("%s: pair type %s.%s is not a struct (AST fallback)", row.table, row.helperPkg, row.pairType)
			}
			var got []string
			for _, field := range st.Fields.List {
				if len(field.Names) == 0 {
					got = append(got, "")
				} else {
					for _, n := range field.Names {
						got = append(got, n.Name)
					}
				}
			}
			if len(got) != len(row.fields) {
				t.Fatalf("%s: pair %s.%s has %d fields %v, want %d %v (AST fallback)", row.table, row.helperPkg, row.pairType, len(got), got, len(row.fields), row.fields)
			}
			for i, fname := range row.fields {
				if got[i] != fname {
					t.Fatalf("%s: pair %s.%s field %d is %q, want %q (AST fallback)", row.table, row.helperPkg, row.pairType, i, got[i], fname)
				}
			}
			return
		}
	}
	t.Fatalf("%s: pair type %s.%s missing from synthetic source (AST fallback; F1-class stale row)", row.table, row.helperPkg, row.pairType)
}

// countFuncResults counts the results of an AST func type: each field
// counts once per name (unnamed fields count once).
func countFuncResults(ft *ast.FuncType) int {
	if ft.Results == nil {
		return 0
	}
	n := 0
	for _, field := range ft.Results.List {
		if len(field.Names) == 0 {
			n++
		} else {
			n += len(field.Names)
		}
	}
	return n
}

// lookupMRFuncAST finds the multi-result func/method declaration in the
// parsed synthetic source and returns its result count.
func lookupMRFuncAST(t *testing.T, f *ast.File, row desugarRow) int {
	t.Helper()
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != row.name {
			continue
		}
		if row.recvType == "" {
			if fd.Recv != nil {
				continue
			}
			return countFuncResults(fd.Type)
		}
		if fd.Recv == nil || len(fd.Recv.List) == 0 {
			continue
		}
		if recvBaseName(fd.Recv.List[0].Type) == row.recvType {
			return countFuncResults(fd.Type)
		}
	}
	t.Fatalf("%s: multi-result %s.%s (recv %q) missing from synthetic source (AST fallback; F1-class stale row)", row.table, row.pkgPath, row.name, row.recvType)
	return 0
}

// helperExistsAST reports whether a package-level func with this name
// is declared in the parsed synthetic source.
func helperExistsAST(f *ast.File, helper string) bool {
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == helper && fd.Recv == nil {
			return true
		}
	}
	return false
}

// lookupMRFunc finds the multi-result func (package-level) or method
// (on the row's recvType named type) in a loaded synthetic package.
func lookupMRFunc(t *testing.T, sp *syntheticPackage, row desugarRow) *types.Func {
	t.Helper()
	name := row.pkgPath + "." + row.name
	if row.recvType == "" {
		obj := sp.pkg.Scope().Lookup(row.name)
		fn, ok := obj.(*types.Func)
		if !ok || fn == nil {
			t.Fatalf("%s: multi-result %s missing or not a func (got %T)", row.table, name, obj)
		}
		return fn
	}
	tnobj := sp.pkg.Scope().Lookup(row.recvType)
	if tnobj == nil {
		t.Fatalf("%s: receiver type %s.%s missing", row.table, row.pkgPath, row.recvType)
	}
	nt, ok := tnobj.Type().(*types.Named)
	if !ok {
		t.Fatalf("%s: %s.%s has type %T, want named type", row.table, row.pkgPath, row.recvType, tnobj.Type())
	}
	// Interface receivers (io/fs.DirEntry, crypto/cipher.AEAD, ...)
	// have no pointer method set distinct from the interface's own:
	// look the method up in the interface method set itself.
	var ms *types.MethodSet
	if _, isIface := nt.Underlying().(*types.Interface); isIface {
		ms = types.NewMethodSet(nt)
	} else {
		ms = types.NewMethodSet(types.NewPointer(nt))
	}
	sel := ms.Lookup(sp.pkg, row.name)
	if sel == nil {
		t.Fatalf("%s: method %s.%s(%s) missing", row.table, row.pkgPath, row.recvType, row.name)
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		t.Fatalf("%s: %s.%s(%s) is %T, want *types.Func", row.table, row.pkgPath, row.recvType, row.name, sel.Obj())
	}
	return fn
}

func TestDesugarTablesConsistent(t *testing.T) {
	rows := allDesugarRows()
	if len(rows) == 0 {
		t.Fatal("no desugar rows found (table sweep is vacuous)")
	}

	// (a) No duplicate (table, pkg, name, recv) keys.
	seen := map[string]string{}
	for _, row := range rows {
		key := row.table + "|" + row.pkgPath + "|" + row.name + "|" + row.recvType
		if prev, ok := seen[key]; ok {
			t.Fatalf("duplicate desugar-table key %q (first at %s)", key, prev)
		}
		seen[key] = row.table
	}
	// The M5 static table has no helper/pair; duplicate-check it too.
	seenM5 := map[string]bool{}
	for _, me := range m5StaticMREntries {
		recv := me.recvType
		if me.anyRecv {
			recv = "*"
		}
		key := me.pkgPath + "|" + me.name + "|" + recv
		if seenM5[key] {
			t.Fatalf("duplicate m5StaticMREntries key %q", key)
		}
		seenM5[key] = true
	}

	// (b)+(c) Per-row: helper + pair exist, pair-field arity matches
	// the real signature. Packages load once each (cached).
	loads := map[string]*pkgLoad{}
	diskLoads := map[string]*syntheticPackage{}
	for _, row := range rows {
		row := row
		name := row.table + ":" + row.pkgPath + "." + row.name
		if row.recvType != "" {
			name += "(" + row.recvType + ")"
		}
		t.Run(name, func(t *testing.T) {
			if _, inSynth := syntheticSources[row.helperPkg]; inSynth {
				// Helper + pair live in the helper package; the MR
				// func/method lives in the declaring package (they
				// differ on helperPkg overrides, e.g. io/fs Info
				// -> os). Each side is verified in its own package.
				hpl := loadSyntheticForSweep(t, loads, row.helperPkg)
				if hpl.failed {
					t.Fatalf("synthetic package %q failed to load (%v) and is not a known AST-fallback package; refusing to skip it (a missing package is exactly the F1 failure)", row.helperPkg, hpl.panic)
				}
				// Iface rows name no MR func/helper — only the pair
				// type, which lives in the helper package. Never load
				// the declaring package for them (e.g. reflect has no
				// synthetic source; its pairs live in reflect_m7).
				var mpl *pkgLoad
				if row.table != "iface" {
					if row.pkgPath == row.helperPkg {
						mpl = hpl
					} else {
						mpl = loadSyntheticForSweep(t, loads, row.pkgPath)
						if mpl.failed {
							t.Fatalf("declaring package %q failed to load (%v)", row.pkgPath, mpl.panic)
						}
					}
				}
				if row.table != "iface" {
					// (c) Pair-field arity matches the real signature.
					if mpl.astOnly {
						n := lookupMRFuncAST(t, mpl.astFile, row)
						if n != len(row.fields) {
							t.Fatalf("%s: %s.%s has %d results, want %d (pairFields %v) (AST fallback)", row.table, row.pkgPath, row.name, n, len(row.fields), row.fields)
						}
					} else {
						fn := lookupMRFunc(t, mpl.sp, row)
						sig, ok := fn.Type().(*types.Signature)
						if !ok {
							t.Fatalf("%s: %s.%s is not a function signature", row.table, row.pkgPath, row.name)
						}
						if sig.Results().Len() != len(row.fields) {
							t.Fatalf("%s: %s.%s has %d results, want %d (pairFields %v)", row.table, row.pkgPath, row.name, sig.Results().Len(), len(row.fields), row.fields)
						}
					}
					// (b) The __-helper exists in the helper package.
					if hpl.astOnly {
						if !helperExistsAST(hpl.astFile, row.helper) {
							t.Fatalf("%s: helper %s.%s missing from synthetic source (AST fallback; F1-class stale row)", row.table, row.helperPkg, row.helper)
						}
					} else {
						hobj := hpl.sp.pkg.Scope().Lookup(row.helper)
						if _, ok := hobj.(*types.Func); !ok {
							t.Fatalf("%s: helper %s.%s missing or not a func (got %T; F1-class stale row)", row.table, row.helperPkg, row.helper, hobj)
						}
					}
				}
				// (b)+(c) The pair struct exists with exactly the
				// listed fields in order.
				if hpl.astOnly {
					checkPairStructAST(t, hpl.astFile, row)
				} else {
					checkPairStruct(t, hpl.sp, row)
				}
				return
			}
			// Disk-owned helper package: the disk transform must own
			// this MR (generated helper + pair, registered spec).
			if !isDiskImportable(row.helperPkg) {
				t.Fatalf("%s: helper package %q is neither synthetic nor disk-importable (F1-class stale row: no such package)", row.table, row.helperPkg)
			}
			sp, ok := diskLoads[row.helperPkg]
			if !ok {
				var err error
				sp, err = getDiskPackage(row.helperPkg)
				if err != nil {
					t.Fatalf("%s: disk package %q failed to build: %v", row.table, row.helperPkg, err)
				}
				diskLoads[row.helperPkg] = sp
			}
			spec := lookupDiskMR(row.pkgPath, row.name, row.recvType)
			if spec == nil {
				t.Fatalf("%s: no disk MR spec for %s.%s (recv %q): the disk transform does not own this MR", row.table, row.pkgPath, row.name, row.recvType)
			}
			if row.table != "iface" {
				if spec.helper == "" {
					t.Fatalf("%s: disk spec for %s.%s (recv %q) is an interface spec (no helper), but the table row names helper %q", row.table, row.pkgPath, row.name, row.recvType, row.helper)
				}
				if spec.helper != row.helper {
					t.Fatalf("%s: disk spec helper for %s.%s is %q, table says %q", row.table, row.pkgPath, row.name, spec.helper, row.helper)
				}
			} else if spec.helper != "" {
				t.Fatalf("%s: iface row for %s.%s resolves to a concrete disk spec with helper %q, want an interface spec", row.table, row.pkgPath, row.name, spec.helper)
			}
			if spec.pairType != row.pairType {
				t.Fatalf("%s: disk spec pair type for %s.%s is %q, table says %q", row.table, row.pkgPath, row.name, spec.pairType, row.pairType)
			}
			if len(spec.pairFields) != len(row.fields) {
				t.Fatalf("%s: disk spec pair fields for %s.%s are %v, table says %v", row.table, row.pkgPath, row.name, spec.pairFields, row.fields)
			}
			for i, f := range row.fields {
				if spec.pairFields[i] != f {
					t.Fatalf("%s: disk spec pair fields for %s.%s are %v, table says %v", row.table, row.pkgPath, row.name, spec.pairFields, row.fields)
				}
			}
			// The generated helper + pair must exist in the disk
			// package scope (concrete rows only; interface specs
			// have no helper, their pair type is generated).
			if row.table != "iface" {
				hobj := sp.pkg.Scope().Lookup(row.helper)
				if _, ok := hobj.(*types.Func); !ok {
					t.Fatalf("%s: disk helper %s.%s missing or not a func (got %T)", row.table, row.helperPkg, row.helper, hobj)
				}
			}
			pobj := sp.pkg.Scope().Lookup(row.pairType)
			nt, ok := pobj.Type().(*types.Named)
			if !ok {
				t.Fatalf("%s: disk pair type %s.%s has type %T, want named struct", row.table, row.helperPkg, row.pairType, pobj.Type())
			}
			st, ok := nt.Underlying().(*types.Struct)
			if !ok || st.NumFields() != len(row.fields) {
				t.Fatalf("%s: disk pair type %s.%s is not a struct with %d fields", row.table, row.helperPkg, row.pairType, len(row.fields))
			}
			for i, fname := range row.fields {
				if st.Field(i).Name() != fname {
					t.Fatalf("%s: disk pair %s.%s field %d is %q, want %q", row.table, row.helperPkg, row.pairType, i, st.Field(i).Name(), fname)
				}
			}
		})
	}
}

// TestDesugarTablesNonEmpty pins the sweep above to a real table size:
// a refactor that silently empties a table must fail loudly instead
// of passing vacuously.
func TestDesugarTablesNonEmpty(t *testing.T) {
	want := map[string]int{
		"m7": len(m7MultiResultFuncs), "io": len(ioMultiResultFuncs),
		"os": len(osMultiResultFuncs), "m8": len(m8MultiResultFuncs),
		"m9": len(m9MultiResultFuncs), "iface": len(m8IfaceMultiResultMethods),
	}
	total := 0
	for table, n := range want {
		if n == 0 {
			t.Fatalf("desugar table %q is empty", table)
		}
		total += n
	}
	// m7 had 295 rows at the F1 fix (17 net/http rows deleted); the
	// exact total is pinned loosely (tables grow) but a big drop fails.
	if total < 700 {
		t.Fatalf("desugar tables hold %d rows total, want >= 700 (a table lost rows)", total)
	}
	// Sorted row counts, for the log.
	keys := []string{"m7", "io", "os", "m8", "m9", "iface"}
	for _, k := range keys {
		t.Logf("table %s: %d rows", k, want[k])
	}
}
