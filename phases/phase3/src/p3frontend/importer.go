package p3frontend

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
)

// Importer is a types.Importer that graduates the frozen overlayImporter:
//   - "runtime" and "fmt" resolve to synthetic packages with the exact
//     frozen signatures (reimplemented here from the freeze spec —
//     export.go's overlayImporter is unexported and frozen, so it cannot
//     be reused, only matched);
//   - "sync" resolves to the synthetic P3-M2b package (ADR-0002) with the
//     exact frozen signatures (four primitives + the defined-but-out-of-
//     scope methods, so the same named scope errors fire);
//   - "errors" and "sort" resolve to the synthetic P3-M7 wave-1 packages
//     (real-Go implementations; fail-fast members keep exact gc 1.27.1
//     signatures);
//   - module-local packages are type-checked from source with go/types,
//     recursively, with results cached by import path;
//   - stdlib packages resolve (proving the GOROOT mapping) but fail with
//     a clear scope error: compiling the real stdlib is P3-M7+ work.
//
// Importer is single-threaded (go/types drives it synchronously).
type Importer struct {
	loader  *Loader
	mainDir string // anchors module-mode resolution
	fset    *token.FileSet

	pkgs  map[string]*types.Package
	infos map[string]*types.Info
	files map[string][]*ast.File
	stack []string // import paths currently being type-checked (cycle guard)
}

// NewImporter builds an Importer. fset is shared across every package so
// the flattener can merge declarations into one file for go/format.
func NewImporter(l *Loader, mainDir string, fset *token.FileSet) *Importer {
	return &Importer{
		loader:  l,
		mainDir: mainDir,
		fset:    fset,
		pkgs:    map[string]*types.Package{},
		infos:   map[string]*types.Info{},
		files:   map[string][]*ast.File{},
	}
}

// Import implements types.Importer.
func (imp *Importer) Import(path string) (*types.Package, error) {
	if pkg, ok := imp.pkgs[path]; ok {
		return pkg, nil
	}
	for _, p := range imp.stack {
		if p == path {
			cycle := append(append([]string{}, imp.stack...), path)
			return nil, fmt.Errorf("p3frontend: import cycle: %s", strings.Join(cycle, " -> "))
		}
	}
	switch path {
	case "runtime":
		pkg := syntheticRuntimePackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "fmt":
		pkg := syntheticFmtPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "sync":
		pkg := syntheticSyncPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "sync/atomic":
		pkg := syntheticAtomicPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "time":
		pkg := syntheticTimePackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "context":
		// Context references time.Time/time.Duration: ensure the
		// synthetic time package is built and cached first.
		timePkg, ok := imp.pkgs["time"]
		if !ok {
			timePkg = syntheticTimePackage()
			imp.pkgs["time"] = timePkg
		}
		pkg := syntheticContextPackage(timePkg)
		imp.pkgs[path] = pkg
		return pkg, nil
	case "errors":
		// P3-M7 (wave 1): synthetic errors package (real-Go New/Is/Unwrap/
		// Join; As/AsType fail-fast). Self-contained: no synthetic
		// dependencies.
		pkg := syntheticErrorsPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "sort":
		// P3-M7 (wave 1): synthetic sort package (real-Go pdqsort/stable/
		// search; Find/Slice/SliceStable/SliceIsSorted fail-fast).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticSortPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "bytes":
		// P3-M7 (wave 1): synthetic bytes package (synth_bytes.go).
		// Requires m7-errors-sort for the errors dependency.
		pkg := syntheticBytesPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "strings":
		// P3-M7 (wave 1): synthetic strings package (synth_strings.go).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticStringsPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "unsafe":
		pkg := syntheticUnsafePackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "math":
		// P3-M7 (wave 1): synthetic math package (real-Go ports +
		// bit-conversion/Sqrt/FMA intrinsics; see synth_math.go).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticMathPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	}
	ref, err := imp.loader.ResolveImport(imp.mainDir, path)
	if err != nil {
		return nil, err
	}
	if ref.Kind == kindStdlib {
		return nil, fmt.Errorf("p3frontend: import %q: stdlib package compilation lands in the P3-M7+ waves (M1 scope: synthetic runtime/fmt only; resolved at %s)", path, ref.Dir)
	}
	if ref.Kind != kindLocal {
		return nil, fmt.Errorf("p3frontend: import %q: unexpected resolution kind", path)
	}
	return imp.checkRef(ref)
}

// ImportRef type-checks an already-resolved local package (used for the
// main package, which has no import path of its own).
func (imp *Importer) ImportRef(ref *PackageRef) (*types.Package, error) {
	if pkg, ok := imp.pkgs[ref.ImportPath]; ok {
		return pkg, nil
	}
	return imp.checkRef(ref)
}

// checkRef parses and type-checks one local package, recording its files
// and type info for the flattener.
func (imp *Importer) checkRef(ref *PackageRef) (*types.Package, error) {
	imp.stack = append(imp.stack, ref.ImportPath)
	defer func() { imp.stack = imp.stack[:len(imp.stack)-1] }()

	var files []*ast.File
	for _, f := range ref.Files {
		src, err := os.ReadFile(filepath.Join(ref.Dir, f))
		if err != nil {
			return nil, fmt.Errorf("p3frontend: reading %s: %w", f, err)
		}
		af, err := parser.ParseFile(imp.fset, filepath.Join(ref.Dir, f), src, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("p3frontend: parsing %s: %w", f, err)
		}
		files = append(files, af)
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Implicits:  make(map[ast.Node]types.Object),
	}
	conf := types.Config{
		Importer: imp,
		Sizes:    types.SizesFor("gc", "amd64"),
	}
	pkg, err := conf.Check(ref.ImportPath, imp.fset, files, info)
	if err != nil {
		return nil, fmt.Errorf("p3frontend: type-checking %s: %w", ref.ImportPath, err)
	}
	imp.pkgs[ref.ImportPath] = pkg
	imp.infos[ref.ImportPath] = info
	imp.files[ref.ImportPath] = files
	return pkg, nil
}

// Package returns the type-checked package for a path previously imported.
func (imp *Importer) Package(path string) *types.Package { return imp.pkgs[path] }

// Info returns the type info recorded while checking path.
func (imp *Importer) Info(path string) *types.Info { return imp.infos[path] }

// Files returns the parsed files of a previously checked package.
func (imp *Importer) Files(path string) []*ast.File { return imp.files[path] }

// ---------------------------------------------------------------------------
// Synthetic packages: exact frozen signatures (INTERFACE_FREEZE.md;
// phases/phase1/frontend/export.go's overlayImporter, reimplemented).
// ---------------------------------------------------------------------------

// syntheticRuntimePackage mirrors the frozen synthetic "runtime": Goexit,
// SetFinalizer, KeepAlive, GC — signatures match gc's runtime package.
func syntheticRuntimePackage() *types.Package {
	pkg := types.NewPackage("runtime", "runtime")
	anyT := types.NewInterfaceType(nil, nil).Complete()
	mkSig := func(params ...*types.Var) *types.Signature {
		return types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), nil, false)
	}
	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "Goexit",
		types.NewSignatureType(nil, nil, nil, nil, nil, false)))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "SetFinalizer", mkSig(
		newParam("obj", anyT),
		newParam("finalizer", anyT))))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "KeepAlive", mkSig(
		newParam("x", anyT))))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "GC", mkSig()))
	pkg.MarkComplete()
	return pkg
}

// syntheticFmtPackage mirrors the frozen D4 fmt stub: exactly the seven
// pure functions with exact variadic signatures.
func syntheticFmtPackage() *types.Package {
	pkg := types.NewPackage("fmt", "fmt")
	anyT := types.NewInterfaceType(nil, nil).Complete()
	anySlice := types.NewSlice(anyT)
	errorT := types.Universe.Lookup("error").Type()
	intT := types.Typ[types.Int]
	stringT := types.Typ[types.String]
	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	insert := func(name string, params, results *types.Tuple) {
		sig := types.NewSignatureType(nil, nil, nil, params, results, true)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	a := newParam("a", anySlice)
	format := newParam("format", stringT)
	n := newParam("n", intT)
	errRes := newParam("err", errorT)
	strRes := newParam("", stringT)
	errResOnly := newParam("", errorT)
	insert("Print", types.NewTuple(a), types.NewTuple(n, errRes))
	insert("Printf", types.NewTuple(format, a), types.NewTuple(n, errRes))
	insert("Println", types.NewTuple(a), types.NewTuple(n, errRes))
	insert("Sprint", types.NewTuple(a), types.NewTuple(strRes))
	insert("Sprintf", types.NewTuple(format, a), types.NewTuple(strRes))
	insert("Sprintln", types.NewTuple(a), types.NewTuple(strRes))
	insert("Errorf", types.NewTuple(format, a), types.NewTuple(errResOnly))
	pkg.MarkComplete()
	return pkg
}

// syncMethodSpecs is the P3-M2b (ADR-0002) method surface each synthetic
// sync primitive exposes: the M2-supported methods plus the defined-but-
// out-of-scope ones (TryLock/TryRLock/RLocker/WaitGroup.Go), which the
// frozen exporter rejects with a named "not in M2 scope" error. Kept as
// data (not code) so the signature-equality test can enumerate it.
var syncMethodSpecs = map[string][]struct {
	name    string
	params  []string // "name:type" — int, bool, func(), Locker
	results []string
}{
	"Mutex": {
		{"Lock", nil, nil},
		{"Unlock", nil, nil},
		{"TryLock", nil, []string{":bool"}},
	},
	"RWMutex": {
		{"Lock", nil, nil},
		{"Unlock", nil, nil},
		{"RLock", nil, nil},
		{"RUnlock", nil, nil},
		{"TryLock", nil, []string{":bool"}},
		{"TryRLock", nil, []string{":bool"}},
		{"RLocker", nil, []string{":Locker"}},
	},
	"WaitGroup": {
		{"Add", []string{"delta:int"}, nil},
		{"Done", nil, nil},
		{"Wait", nil, nil},
		{"Go", []string{"f:func()"}, nil},
	},
	"Once": {
		{"Do", []string{"f:func()"}, nil},
	},
}

// syntheticSyncPackage mirrors the frozen P3-M2b synthetic "sync"
// (phases/phase1/frontend/export.go's syntheticSyncPackage, reimplemented
// — the frozen original is unexported, so this is matched, not reused).
// All methods have pointer receivers with exact gc 1.27.1 signatures.
func syntheticSyncPackage() *types.Package {
	pkg := types.NewPackage("sync", "sync")
	emptyStruct := types.NewStruct(nil, nil)
	typeOf := func(spec string) types.Type {
		name := spec
		if i := strings.Index(spec, ":"); i >= 0 {
			name = spec[i+1:]
		}
		switch name {
		case "int":
			return types.Typ[types.Int]
		case "bool":
			return types.Typ[types.Bool]
		case "func()":
			return types.NewSignatureType(nil, nil, nil, nil, nil, false)
		case "Locker":
			return syncLockerInterface(pkg)
		}
		panic("unknown sync param type " + spec)
	}
	names := []string{"Mutex", "RWMutex", "WaitGroup", "Once"}
	for _, name := range names {
		tname := types.NewTypeName(token.NoPos, pkg, name, nil)
		pkg.Scope().Insert(tname)
		named := types.NewNamed(tname, emptyStruct, nil)
		for _, ms := range syncMethodSpecs[name] {
			var params []*types.Var
			for _, ps := range ms.params {
				pname := ps[:strings.Index(ps, ":")]
				params = append(params, types.NewParam(token.NoPos, pkg, pname, typeOf(ps)))
			}
			var results []*types.Var
			for _, rs := range ms.results {
				results = append(results, types.NewParam(token.NoPos, pkg, "", typeOf(rs)))
			}
			recv := types.NewParam(token.NoPos, pkg, "", types.NewPointer(named))
			sig := types.NewSignatureType(recv, nil, nil,
				types.NewTuple(params...), types.NewTuple(results...), false)
			named.AddMethod(types.NewFunc(token.NoPos, pkg, ms.name, sig))
		}
	}
	pkg.MarkComplete()
	return pkg
}

// syncLockerInterface builds the synthesized sync.Locker interface
// (Lock/Unlock) used as RWMutex.RLocker's result type.
func syncLockerInterface(pkg *types.Package) types.Type {
	return types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Lock",
			types.NewSignatureType(nil, nil, nil, nil, nil, false)),
		types.NewFunc(token.NoPos, pkg, "Unlock",
			types.NewSignatureType(nil, nil, nil, nil, nil, false)),
	}, nil).Complete()
}

// syntheticAtomicPackage mirrors the frozen P3-M4 synthetic "sync/atomic"
// (phases/phase1/frontend/export.go's syntheticAtomicPackage, reimplemented
// — the frozen original is unexported, so this is matched, not reused).
// The six typed wrappers with exact gc 1.27.1 method signatures (pointer
// receivers), plus the package-level functions with exact signatures.
func syntheticAtomicPackage() *types.Package {
	pkg := types.NewPackage("sync/atomic", "atomic")
	int64T := types.Typ[types.Int64]
	uint64T := types.Typ[types.Uint64]
	int32T := types.Typ[types.Int32]
	uint32T := types.Typ[types.Uint32]
	boolT := types.Typ[types.Bool]
	uintptrT := types.Typ[types.Uintptr]
	emptyStruct := types.NewStruct(nil, nil)
	param := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	tuple := func(v *types.Var) *types.Tuple {
		return types.NewTuple(v)
	}

	newWrapper := func(name string) *types.Named {
		tname := types.NewTypeName(token.NoPos, pkg, name, nil)
		pkg.Scope().Insert(tname)
		return types.NewNamed(tname, emptyStruct, nil)
	}
	addMethod := func(named *types.Named, name string, params, results *types.Tuple) {
		recv := types.NewParam(token.NoPos, pkg, "", types.NewPointer(named))
		sig := types.NewSignatureType(recv, nil, nil, params, results, false)
		named.AddMethod(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	// The five M4 ops per wrapper: Load() T, Store(T), Add(T) T,
	// Swap(T) T, CompareAndSwap(T, T) bool. And/Or defined, rejected.
	fiveOps := func(named *types.Named, t types.Type) {
		boolRes := tuple(param("", boolT))
		addMethod(named, "Load", nil, tuple(param("", t)))
		addMethod(named, "Store", tuple(param("val", t)), nil)
		addMethod(named, "Add", tuple(param("delta", t)), tuple(param("new", t)))
		addMethod(named, "Swap", tuple(param("new", t)), tuple(param("old", t)))
		addMethod(named, "CompareAndSwap",
			types.NewTuple(param("old", t), param("new", t)), boolRes)
		addMethod(named, "And", tuple(param("mask", t)), tuple(param("old", t)))
		addMethod(named, "Or", tuple(param("mask", t)), tuple(param("old", t)))
	}
	fiveOps(newWrapper("Int64"), int64T)
	fiveOps(newWrapper("Uint64"), uint64T)
	fiveOps(newWrapper("Int32"), int32T)
	fiveOps(newWrapper("Uint32"), uint32T)
	fiveOps(newWrapper("Uintptr"), uintptrT)
	// Bool has no Add/And/Or in gc.
	b := newWrapper("Bool")
	boolRes := tuple(param("", boolT))
	addMethod(b, "Load", nil, tuple(param("", boolT)))
	addMethod(b, "Store", tuple(param("val", boolT)), nil)
	addMethod(b, "Swap", tuple(param("new", boolT)), tuple(param("old", boolT)))
	addMethod(b, "CompareAndSwap",
		types.NewTuple(param("old", boolT), param("new", boolT)), boolRes)

	// Package-level functions: exact gc signatures.
	addFunc := func(name string, params, results *types.Tuple) {
		sig := types.NewSignatureType(nil, nil, nil, params, results, false)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	for _, wt := range []types.Type{int32T, int64T} {
		nm := map[types.Type]string{int32T: "Int32", int64T: "Int64"}[wt]
		paddr := types.NewPointer(wt)
		addFunc("Load"+nm, tuple(param("addr", paddr)), tuple(param("", wt)))
		addFunc("Store"+nm, types.NewTuple(param("addr", paddr), param("val", wt)), nil)
		addFunc("Add"+nm, types.NewTuple(param("addr", paddr), param("delta", wt)), tuple(param("new", wt)))
		addFunc("Swap"+nm, types.NewTuple(param("addr", paddr), param("new", wt)), tuple(param("old", wt)))
		addFunc("CompareAndSwap"+nm,
			types.NewTuple(param("addr", paddr), param("old", wt), param("new", wt)), boolRes)
	}
	for _, wt := range []types.Type{uint32T, uint64T, uintptrT} {
		nm := map[types.Type]string{uint32T: "Uint32", uint64T: "Uint64", uintptrT: "Uintptr"}[wt]
		paddr := types.NewPointer(wt)
		addFunc("Load"+nm, tuple(param("addr", paddr)), tuple(param("", wt)))
		addFunc("Store"+nm, types.NewTuple(param("addr", paddr), param("val", wt)), nil)
		addFunc("Add"+nm, types.NewTuple(param("addr", paddr), param("delta", wt)), tuple(param("new", wt)))
		addFunc("Swap"+nm, types.NewTuple(param("addr", paddr), param("new", wt)), tuple(param("old", wt)))
		addFunc("CompareAndSwap"+nm,
			types.NewTuple(param("addr", paddr), param("old", wt), param("new", wt)), boolRes)
	}
	{
		paddr := types.NewPointer(boolT)
		addFunc("LoadBool", tuple(param("addr", paddr)), tuple(param("", boolT)))
		addFunc("StoreBool", types.NewTuple(param("addr", paddr), param("val", boolT)), nil)
		addFunc("SwapBool", types.NewTuple(param("addr", paddr), param("new", boolT)), tuple(param("old", boolT)))
		addFunc("CompareAndSwapBool",
			types.NewTuple(param("addr", paddr), param("old", boolT), param("new", boolT)), boolRes)
	}

	pkg.MarkComplete()
	return pkg
}

// syntheticUnsafePackage mirrors the frozen P3-M4 empty synthetic "unsafe"
// (phases/phase1/frontend/export.go's syntheticUnsafePackage): it exists
// only so `import _ "unsafe"` (required by //go:linkname directives)
// type-checks.
func syntheticUnsafePackage() *types.Package {
	pkg := types.NewPackage("unsafe", "unsafe")
	pkg.MarkComplete()
	return pkg
}
