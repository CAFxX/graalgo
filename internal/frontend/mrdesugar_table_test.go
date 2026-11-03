package main

// Mechanical integrity test for the multi-result desugar tables
// (m7MultiResultFuncs, ioMultiResultFuncs, osMultiResultFuncs,
// m8MultiResultFuncs, m9MultiResultFuncs, m8IfaceMultiResultMethods).
// Added by the 2026-11-03 table cleanup (REVIEW-2026-11-03-05 P1):
// the tables previously contained 19 entries naming __-helpers and
// pair types that exist nowhere (net/http + net/http/httptest rows
// left over from the retired synthetic net/http drop, archive/zip
// Close rows for single-result methods), plus exact duplicate keys
// (crypto/sha3 Write/SHAKE, net Accept/TCPListener, a duplicated
// crypto/elliptic + crypto/ecdh interface block). Nothing caught any
// of it: the old table comment claimed verification that no test
// performed, and the net/http rows were worse than dead — a direct
// call matching one panicked the frontend in getSyntheticPackage
// ("no synthetic source for net/http") before the disk classifier ran.
//
// What this test asserts, per row:
//  1. No duplicate (pkg, method, recv) keys across the concrete tables
//     (first match wins everywhere, so a duplicate is drift even when
//     the rows are identical), and no duplicate (pkg, method) keys in
//     the interface table.
//  2. Every row's helper and pair type are declared in the synthetic
//     source the row names (through its helperPkg, or
//     m7HelperPkgOverride for m7 rows). Rows may name synthetic
//     packages only: a disk package is served by the disk-MR transform
//     (transformDiskMultiResult), never by a static row — a row naming
//     a package with no synthetic source fails here instead of
//     panicking the frontend at the first matching call.
//  3. Every row's pair arity matches the real signature: the pair
//     struct's fields in order equal the row's pairFields, and for
//     concrete rows the stub method's result count in the same source
//     equals len(pairFields) (so a 2-field pair can never be attached
//     to a single-result method like zip Close again).
//
// Method: declaration scan, NOT a type-check. The sources are parsed
// (after the same restoreRawStrings + hoistSyntheticImports repairs
// buildSyntheticPackage applies) but never type-checked, so this test
// stays green through unrelated synthetic type breakage (e.g. a
// synthetic package whose imports currently fail to type-check) and
// stays fast: only the packages the tables actually name are parsed.
// What it does NOT check: that pair field TYPES match the method's
// result types positionally — that needs type information and is
// checked at export by the classifiers' types.Identical guards
// (classifyM7MultiResultCall's pair lookup, multiResultHelperFunc's
// positional shape check, classifyIfaceMultiResultCall's field check).
// A row that passes here but mismatches types still fails closed at
// export; it cannot silently desugar to wrong fields.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// tableRow is one desugar-table row normalized across the table types.
type tableRow struct {
	table      string // table variable name, for error messages
	pkgPath    string // declaring package
	name       string // function/method name
	recvType   string // receiver named type ("" for package-level funcs)
	helper     string // __-helper name ("" for interface rows: no helper)
	helperPkg  string // package holding helper+pair ("" = pkgPath)
	pairType   string
	pairFields []string
	iface      bool // interface-dispatch row (no helper, no stub signature)
}

func collectDesugarTableRows() (concrete []tableRow, iface []tableRow) {
	for i := range m7MultiResultFuncs {
		mf := &m7MultiResultFuncs[i]
		helperPkg := mf.pkgPath
		if override, ok := m7HelperPkgOverride[mf.pkgPath]; ok {
			helperPkg = override
		}
		concrete = append(concrete, tableRow{
			table: "m7MultiResultFuncs", pkgPath: mf.pkgPath,
			name: mf.name, recvType: mf.recvType, helper: mf.helper,
			helperPkg: helperPkg, pairType: mf.pairType,
			pairFields: mf.pairFields,
		})
	}
	for i := range ioMultiResultFuncs {
		mf := &ioMultiResultFuncs[i]
		concrete = append(concrete, tableRow{
			table: "ioMultiResultFuncs", pkgPath: mf.pkgPath,
			name: mf.name, recvType: mf.recvType, helper: mf.helper,
			helperPkg: mf.pkgPath, pairType: mf.pairType,
			pairFields: mf.pairFields,
		})
	}
	addOsRows := func(table string, rows []osMultiResultFunc) {
		for i := range rows {
			mf := &rows[i]
			helperPkg := mf.helperPkg
			if helperPkg == "" {
				helperPkg = mf.pkgPath
			}
			concrete = append(concrete, tableRow{
				table: table, pkgPath: mf.pkgPath,
				name: mf.name, recvType: mf.recvType, helper: mf.helper,
				helperPkg: helperPkg, pairType: mf.pairType,
				pairFields: mf.pairFields,
			})
		}
	}
	addOsRows("osMultiResultFuncs", osMultiResultFuncs)
	addOsRows("m8MultiResultFuncs", m8MultiResultFuncs)
	// m9MultiResultFunc is a distinct struct type with identical fields.
	for i := range m9MultiResultFuncs {
		mf := &m9MultiResultFuncs[i]
		helperPkg := mf.helperPkg
		if helperPkg == "" {
			helperPkg = mf.pkgPath
		}
		concrete = append(concrete, tableRow{
			table: "m9MultiResultFuncs", pkgPath: mf.pkgPath,
			name: mf.name, recvType: mf.recvType, helper: mf.helper,
			helperPkg: helperPkg, pairType: mf.pairType,
			pairFields: mf.pairFields,
		})
	}
	for i := range m8IfaceMultiResultMethods {
		m := &m8IfaceMultiResultMethods[i]
		helperPkg := m.pkgPath
		if override, ok := m7HelperPkgOverride[m.pkgPath]; ok {
			helperPkg = override
		}
		iface = append(iface, tableRow{
			table: "m8IfaceMultiResultMethods", pkgPath: m.pkgPath,
			name: m.method, helperPkg: helperPkg, pairType: m.pairType,
			pairFields: m.pairFields, iface: true,
		})
	}
	return concrete, iface
}

// synthDecls is the declaration-level view of one synthetic source.
type synthDecls struct {
	// funcs maps a function/method name to its declarations. For a
	// method the key is recvType + "." + name; for a package-level
	// func it is "." + name. A name may have several declarations
	// (e.g. the same method on different receivers).
	funcs map[string][]*ast.FuncDecl
	// ifaceMethods maps an interface method to its result count, keyed
	// ifaceType + "." + method (e.g. "AEAD.Open"): interface methods
	// have no FuncDecl, but rows like cipher AEAD.Open still need a
	// stub-arity check against the interface declaration.
	ifaceMethods map[string]int
	// pairStructs maps a type name to its struct field names in order.
	// Aliases (type X = Y) resolve to Y's fields.
	pairStructs map[string][]string
	// helpers maps a package-level func name to its single result's
	// base type name (unwrapping *, [], generics, parens).
	helpers map[string]string
}

// typeBaseName strips *, [], generic instantiation, and parens down to
// the named type's identifier, or "" when the expression is not a
// named type at all.
func typeBaseName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeBaseName(t.X)
	case *ast.ParenExpr:
		return typeBaseName(t.X)
	case *ast.IndexExpr:
		return typeBaseName(t.X)
	case *ast.IndexListExpr:
		return typeBaseName(t.X)
	case *ast.ArrayType:
		return typeBaseName(t.Elt)
	case *ast.SelectorExpr:
		// Qualified type (pkg.Type): the pair types are always local,
		// so a selector here means "not the pair type".
		return ""
	}
	return ""
}

// recvBaseNameOf mirrors recvBaseName in mr_disk.go for AST receiver
// expressions: the named type behind T, *T, or T[args].
func recvBaseNameOf(e ast.Expr) string {
	return typeBaseName(e)
}

func parseSyntheticDecls(t *testing.T, pkgPath string) *synthDecls {
	t.Helper()
	src, ok := syntheticSources[pkgPath]
	if !ok {
		t.Fatalf("no synthetic source for %q", pkgPath)
	}
	src = hoistSyntheticImports(restoreRawStrings(src))
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, pkgPath+".go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing synthetic source for %q: %v", pkgPath, err)
	}
	d := &synthDecls{
		funcs:        map[string][]*ast.FuncDecl{},
		ifaceMethods: map[string]int{},
		pairStructs:  map[string][]string{},
		helpers:      map[string]string{},
	}
	// Interface methods: TypeSpec whose type is an interface.
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok || it.Methods == nil {
				continue
			}
			for _, m := range it.Methods.List {
				ft, ok := m.Type.(*ast.FuncType)
				if !ok || ft.Results == nil {
					continue
				}
				n := 0
				for _, fld := range ft.Results.List {
					if len(fld.Names) == 0 {
						n++
					} else {
						n += len(fld.Names)
					}
				}
				for _, name := range m.Names {
					d.ifaceMethods[ts.Name.Name+"."+name.Name] = n
				}
			}
		}
	}
	// First pass: struct types (so aliases resolve in any order).
	typeSpecs := map[string]ast.Expr{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			typeSpecs[ts.Name.Name] = ts.Type
		}
	}
	// Declared with var (not :=) so the closure may call itself for
	// alias chains: a :=-declared variable is not in scope inside its
	// own initializer.
	var structFields func(name string, depth int) ([]string, bool)
	structFields = func(name string, depth int) ([]string, bool) {
		if depth > 4 {
			return nil, false
		}
		typ, ok := typeSpecs[name]
		if !ok {
			return nil, false
		}
		if id, ok := typ.(*ast.Ident); ok {
			// Alias: type X = Y.
			return structFields(id.Name, depth+1)
		}
		st, ok := typ.(*ast.StructType)
		if !ok {
			return nil, false
		}
		var fields []string
		for _, fld := range st.Fields.List {
			if len(fld.Names) != 1 {
				return nil, false
			}
			fields = append(fields, fld.Names[0].Name)
		}
		return fields, true
	}
	for name := range typeSpecs {
		if fields, ok := structFields(name, 0); ok {
			d.pairStructs[name] = fields
		}
	}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		recv := ""
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			recv = recvBaseNameOf(fd.Recv.List[0].Type)
		}
		d.funcs[recv+"."+fd.Name.Name] = append(d.funcs[recv+"."+fd.Name.Name], fd)
		if recv == "" && fd.Type.Results != nil && len(fd.Type.Results.List) == 1 {
			// Single-result package-level func: candidate __-helper.
			// Count result names: one field may declare several
			// results (e.g. "(a, b int)" is 2 results, not 1).
			n := 0
			for _, fld := range fd.Type.Results.List {
				if len(fld.Names) == 0 {
					n++
				} else {
					n += len(fld.Names)
				}
			}
			if n == 1 {
				d.helpers[fd.Name.Name] = typeBaseName(fd.Type.Results.List[0].Type)
			}
		}
	}
	return d
}

func TestDesugarTablesResolve(t *testing.T) {
	concrete, iface := collectDesugarTableRows()
	if len(concrete) == 0 || len(iface) == 0 {
		t.Fatalf("collectDesugarTableRows returned %d concrete, %d iface rows; expected both non-empty (table collection broke?)", len(concrete), len(iface))
	}

	// 1. Duplicate keys.
	seen := map[string]string{}
	for _, r := range concrete {
		key := r.pkgPath + "\x00" + r.name + "\x00" + r.recvType
		if prev, ok := seen[key]; ok {
			t.Errorf("duplicate concrete desugar key (%s, %s, %s): %s and %s (first match wins; delete one)",
				r.pkgPath, r.name, r.recvType, prev, r.table)
		} else {
			seen[key] = r.table
		}
	}
	seenIface := map[string]string{}
	for _, r := range iface {
		key := r.pkgPath + "\x00" + r.name
		if prev, ok := seenIface[key]; ok {
			t.Errorf("duplicate interface desugar key (%s, %s): %s and %s (first match wins; delete one)",
				r.pkgPath, r.name, prev, r.table)
		} else {
			seenIface[key] = r.table
		}
	}

	// 2+3. Helper/pair resolution and pair arity, per row.
	parsed := map[string]*synthDecls{}
	declsFor := func(pkgPath string) *synthDecls {
		if d, ok := parsed[pkgPath]; ok {
			return d
		}
		if _, ok := syntheticSources[pkgPath]; !ok {
			return nil
		}
		d := parseSyntheticDecls(t, pkgPath)
		parsed[pkgPath] = d
		return d
	}
	checkRow := func(r tableRow) {
		key := r.pkgPath + "." + r.name
		if r.recvType != "" {
			key = r.pkgPath + "." + r.recvType + "." + r.name
		}
		d := declsFor(r.helperPkg)
		if d == nil {
			t.Errorf("%s row %s: helper package %q has no synthetic source — "+
				"static rows may name synthetic packages only; a disk package is served by "+
				"transformDiskMultiResult, never by a static row (delete the row)",
				r.table, key, r.helperPkg)
			return
		}
		fields, ok := d.pairStructs[r.pairType]
		if !ok {
			t.Errorf("%s row %s: pair type %q not declared as a struct in %q",
				r.table, key, r.pairType, r.helperPkg)
			return
		}
		if len(fields) != len(r.pairFields) {
			t.Errorf("%s row %s: pair type %q has %d fields %v, row declares %d pairFields %v",
				r.table, key, r.pairType, len(fields), fields, len(r.pairFields), r.pairFields)
			return
		}
		for i, want := range r.pairFields {
			if fields[i] != want {
				t.Errorf("%s row %s: pair type %q field %d = %q, row declares %q (fields must match in order)",
					r.table, key, r.pairType, i, fields[i], want)
			}
		}
		if r.iface {
			return
		}
		// Helper: a single-result func returning exactly the pair type.
		gotPair, ok := d.helpers[r.helper]
		if !ok {
			t.Errorf("%s row %s: helper %q not declared as a single-result func in %q",
				r.table, key, r.helper, r.helperPkg)
		} else if gotPair != r.pairType {
			t.Errorf("%s row %s: helper %q returns %q, row declares pair type %q",
				r.table, key, r.helper, gotPair, r.pairType)
		}
		// Stub signature arity: the multi-result method/func must have
		// exactly len(pairFields) results. The stub lives in the
		// declaring package's source (r.pkgPath) — except when that
		// package has no synthetic source (retired packages whose
		// helpers live under an override, e.g. reflect -> reflect_m7),
		// where the helper package's source carries the stub. An
		// interface-declared method (cipher AEAD.Open, io/fs
		// DirEntry.Info) counts via its interface declaration.
		stubDecls := declsFor(r.pkgPath)
		stubPkg := r.pkgPath
		if stubDecls == nil {
			stubDecls = d
			stubPkg = r.helperPkg
		}
		matched := false
		if n, ok := stubDecls.ifaceMethods[r.recvType+"."+r.name]; ok && n == len(r.pairFields) {
			matched = true
		}
		for _, stub := range stubDecls.funcs[r.recvType+"."+r.name] {
			if stub.Type.Results == nil {
				continue
			}
			n := 0
			for _, fld := range stub.Type.Results.List {
				if len(fld.Names) == 0 {
					n++
				} else {
					n += len(fld.Names)
				}
			}
			if n == len(r.pairFields) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("%s row %s: no stub in %q has %d results (pair arity); row pairFields = %v",
				r.table, key, stubPkg, len(r.pairFields), r.pairFields)
		}
	}
	for _, r := range concrete {
		checkRow(r)
	}
	for _, r := range iface {
		checkRow(r)
	}
	t.Logf("checked %d concrete + %d interface desugar rows across %d synthetic packages",
		len(concrete), len(iface), len(parsed))
}

// TestDesugarTablesNoDiskPackages pins the rule the resolver check
// enforces from the other side: after the net/http retirement, no
// concrete or interface row may name a package outside
// syntheticSources at all (the resolver check above reports those
// rows one by one; this test names the rule so a future row that
// "only" breaks the rule is caught even if its helper happens to
// resolve elsewhere). It is intentionally redundant with the loop
// above: the loop proves each row resolves, this test states the
// invariant a reader can grep for.
func TestDesugarTablesNoDiskPackages(t *testing.T) {
	concrete, iface := collectDesugarTableRows()
	for _, r := range concrete {
		if _, ok := syntheticSources[r.helperPkg]; !ok {
			t.Errorf("concrete row %s.%s(%s) in %s names non-synthetic helper package %q",
				r.pkgPath, r.name, r.recvType, r.table, r.helperPkg)
		}
	}
	for _, r := range iface {
		if _, ok := syntheticSources[r.helperPkg]; !ok {
			t.Errorf("interface row %s.%s in %s names non-synthetic pair package %q",
				r.pkgPath, r.name, r.table, r.helperPkg)
		}
	}
}
