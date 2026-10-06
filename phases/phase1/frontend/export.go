// Package main implements gofrontend, the graalgo Go frontend service.
//
// Architecture (PLAN.md D1): the vendored go/* packages (see VENDORING.md)
// type-check one single-file package; this file walks the typed AST and
// emits the frontend/v1 JSON document described in ../docs/frontend-v1.md.
//
// Scope of this revision (M4 control flow + composite types, WS E):
//   - exactly one input file, no imports (import overlay arrives with WS G)
//   - exactly one function, `func main`, zero params, at most one result
//   - statements: blocks, var declarations (var / :=), assignments
//     (= and op=; lhs may be an identifier, index, or selector),
//     expression statements, return, ++/-- (identifier/index/selector),
//     if/else (with init), for (3-clause, condition-only, infinite),
//     range (slice/array/string/map; no channels), switch (expression +
//     type switches), break/continue (labeled and unlabeled), goto/labels,
//     fallthrough
//   - expressions: identifiers, basic literals, binary/unary ops,
//     parentheses, builtin calls (emitted; NodeFactory v1 rejects them),
//     composite literals (incl. keyed, nested elision, &T{}),
//     index (incl. comma-ok), slice expressions (2- and 3-index),
//     selectors (incl. embedded/promoted fields), type assertions
//     (incl. comma-ok)
//
// Anything outside this scope fails fast with an explicit error naming the
// workstream that extends the scope. There are no silently dropped nodes,
// except *ast.EmptyStmt which the exporter normalizes away (documented in
// docs/frontend-v1.md).
//
// D1 falsifier watch (GG-0012): this file uses only long-stable go/* APIs
// (parser.ParseFile, types.Config.Check, Info.{Types,Defs,Uses}). If a future
// re-vendoring breaks compilation or changes behavior of these APIs, STOP and
// report: that is a spike-3 toil re-measurement trigger, not a cue to adapt
// silently.
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"
)

// frontendVersion is the JSON protocol version emitted. Additive-only changes
// keep "v1"; any breaking change bumps to "v2" (docs/frontend-v1.md §8).
const frontendVersion = "v1"

// langVersion is the Go language version assumed when type-checking.
// Per-file -lang gates arrive with WS G (GG-0013); until then the vendored
// toolchain default applies.
const langVersion = "go1.27"

// ---------------------------------------------------------------------------
// JSON model (field-for-field contract: docs/frontend-v1.md).
// ---------------------------------------------------------------------------

// Doc is the root of a frontend/v1 document.
type Doc struct {
	Frontend  string      `json:"frontend"`
	Generator Generator   `json:"generator"`
	Package   Pkg         `json:"package"`
	Types     []TypeEnt   `json:"types"`
	Objects   []ObjectEnt `json:"objects"`
	Funcs     []*Node     `json:"funcs"`
	// Globals lists package-level variables in initialization
	// (dependency) order (M7). A nil Init means the zero value;
	// Obj == -1 means "evaluate Init for side effects, discard"
	// (package-level `var _ = ...`).
	Globals []GlobalEnt `json:"globals"`
}

// GlobalEnt is one package-level variable (M7): the object-table id of
// the variable plus its initializer expression (nil when absent).
type GlobalEnt struct {
	Obj  int   `json:"obj"`
	Init *Node `json:"init"`
	Pos  *Pos  `json:"pos,omitempty"`
}

type Generator struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	GoRoot  string `json:"goroot"`
}

type Pkg struct {
	Name  string   `json:"name"`
	Files []string `json:"files"`
	Lang  string   `json:"lang"`
}

// TypeEnt is one entry of the type table. Kind selects the Detail schema;
// every go/types type reachable from the program appears exactly once and
// expressions reference entries by index.
type TypeEnt struct {
	Kind   string         `json:"kind"`
	Detail map[string]any `json:"detail"`
}

// ObjectEnt is one entry of the object table: variables ("var"), top-level
// functions and methods ("func", M6/WS H), and monomorphized instantiations
// ("inst", M6/WS H). Identifiers reference entries by index; the id is
// assigned at the declaration in a single deterministic AST walk, so ids are
// stable across runs.
type ObjectEnt struct {
	Kind  string `json:"kind"` // "var", "func", "inst"
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Param bool   `json:"param"`
	// InstOf and TypeArgs are meaningful only for kind "inst": the generic
	// function/method object this instantiation was monomorphized from, and
	// the concrete type-argument ids (receiver args then method args).
	InstOf   int   `json:"instof"`
	TypeArgs []int `json:"typeargs"`
}

// Node is one frontend/v1 AST node. Kind selects the Fields schema;
// the contract per kind is docs/frontend-v1.md, enforced by
// tools/validate_frontend_v1.py.
type Node struct {
	Kind   string         `json:"kind"`
	Pos    *Pos           `json:"pos,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
}

// (FuncDecl/Param folded into Node: every node carries kind/pos/fields;
// see the Node type above. Params are emitted as kind:"param" nodes.)

// Pos is a source position: file index into Doc.Package.Files, 1-based
// line/column (go/token convention, but COLUMN IS RUNE-BASED — see below),
// byte offset, and rune length of the node's source text.
//
// M8: col and len are rune (character) counts, not bytes. Truffle's
// Source.createSection(line, col, len) is char-based; emitting byte-based
// values crashed it ("charIndex out of range") on any non-ASCII source.
// off remains the byte offset of the node's first character (NodeFactory
// does not consume it).
type Pos struct {
	File int `json:"file"`
	Line int `json:"line"`
	Col  int `json:"col"`
	Off  int `json:"off"`
	Len  int `json:"len"`
}

// ---------------------------------------------------------------------------
// Export entry point.
// ---------------------------------------------------------------------------

// failImporter rejects every import. The Phase-1 import overlay (WS G)
// replaces this; until then an import is a scope error, reported before
// type-checking so the failure is never a confusing types.Config error.
type failImporter struct{}

func (failImporter) Import(path string) (*types.Package, error) {
	return nil, fmt.Errorf("import %q: imports are not supported in frontend/v1 scope (WS G import overlay)", path)
}

// overlayImporter is the M5 (WS F) minimal import overlay, extended by M6
// (WS H) with the D4 fmt stub and by P3-M2b (WS D, ADR-0002) with the
// synthetic sync package: it resolves exactly the synthetic package
// "runtime" exposing runtime.Goexit (plus the P2-M4 GC-interop surface),
// the synthetic package "fmt" exposing the seven D4 symbols (Print,
// Printf, Println, Sprint, Sprintf, Sprintln, Errorf) with exact variadic
// signatures, and the synthetic package "sync" exposing the four P3-M2b
// primitives (Mutex, RWMutex, WaitGroup, Once) with exact gc method
// signatures. Every other import still fails via failImporter. WS G's full
// import overlay absorbs this carve-out.
type overlayImporter struct{}

func (overlayImporter) Import(path string) (*types.Package, error) {
	switch path {
	case "runtime":
		pkg := types.NewPackage("runtime", "runtime")
		sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "Goexit", sig))
		// P2-M4 (GC interop, ADR-0002 D4): the Go-observable GC surface.
		// Signatures match gc's runtime package exactly; the Java side is
		// graalgo.runtime.GcInterop (Phase-1 stub, Phase-2 real).
		anyT := types.NewInterfaceType(nil, nil).Complete()
		mkSig := func(params ...*types.Var) *types.Signature {
			return types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), nil, false)
		}
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "SetFinalizer", mkSig(
			types.NewVar(token.NoPos, pkg, "obj", anyT),
			types.NewVar(token.NoPos, pkg, "finalizer", anyT))))
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "KeepAlive", mkSig(
			types.NewVar(token.NoPos, pkg, "x", anyT))))
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "GC", mkSig()))
		pkg.MarkComplete()
		return pkg, nil
	case "fmt":
		return syntheticFmtPackage(), nil
	case "sync":
		return syntheticSyncPackage(), nil
	}
	return failImporter{}.Import(path)
}

// syntheticFmtPackage builds the D4 fmt stub package for the Phase-1 import
// overlay (PLAN.md D4): exactly the seven pure functions with exact
// variadic signatures. Calls to any other fmt.X are a scope error naming
// Phase 3.
func syntheticFmtPackage() *types.Package {
	pkg := types.NewPackage("fmt", "fmt")
	anyT := types.NewInterfaceType(nil, nil).Complete()
	anySlice := types.NewSlice(anyT)
	errorT := types.Universe.Lookup("error").Type()
	intT := types.Typ[types.Int]
	stringT := types.Typ[types.String]
	param := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	insert := func(name string, params, results *types.Tuple) {
		sig := types.NewSignatureType(nil, nil, nil, params, results, true)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	a := param("a", anySlice)
	format := param("format", stringT)
	n := param("n", intT)
	errRes := param("err", errorT)
	strRes := param("", stringT)
	errResOnly := param("", errorT)
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

// fmtOverlayFuncs are the seven D4 symbols (PLAN.md D4).
var fmtOverlayFuncs = map[string]bool{
	"Print": true, "Printf": true, "Println": true,
	"Sprint": true, "Sprintf": true, "Sprintln": true, "Errorf": true,
}

// syncPrimitiveNames are the four P3-M2b (WS D, ADR-0002) primitives bound
// to the Phase-2 Java runtime (graalgo.runtime.sync).
var syncPrimitiveNames = map[string]bool{
	"Mutex": true, "RWMutex": true, "WaitGroup": true, "Once": true,
}

// syncOpsInScope is the M2-supported method set per primitive (ADR-0002
// D6). Defined-but-out-of-scope methods (TryLock/TryRLock/RLocker,
// WaitGroup.Go) fail fast in exportSyncOp with a named scope error.
var syncOpsInScope = map[string]map[string]bool{
	"Mutex":     {"Lock": true, "Unlock": true},
	"RWMutex":   {"Lock": true, "Unlock": true, "RLock": true, "RUnlock": true},
	"WaitGroup": {"Add": true, "Done": true, "Wait": true},
	"Once":      {"Do": true},
}

// syncPrimitiveNamed reports whether t is exactly one of the synthetic
// sync primitive named types (no pointer dereference).
func syncPrimitiveNamed(t types.Type) *types.Named {
	nt, ok := t.(*types.Named)
	if !ok {
		return nil
	}
	obj := nt.Obj()
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Name() != "sync" {
		return nil
	}
	if !syncPrimitiveNames[obj.Name()] {
		return nil
	}
	return nt
}

// syncPrimitiveRecv is syncPrimitiveNamed after one pointer dereference,
// for method-call receivers (all real sync methods have pointer receivers;
// the Go value IS the Java object, so &/* is a no-op at the value level).
func syncPrimitiveRecv(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	return syncPrimitiveNamed(t)
}

// syntheticSyncPackage builds the P3-M2b (WS D, ADR-0002) synthetic "sync"
// package for the import overlay: the four Phase-2-bound primitives with
// exact gc 1.27.1 method signatures (pointer receivers, like the real
// package; verified against ~/tools/go-1.27.1/src/sync/*.go). Out-of-scope
// methods (TryLock/TryRLock/RLocker/WaitGroup.Go) are defined with exact
// signatures so misuse fails fast with a named "not in M2 scope" error
// instead of a confusing "undefined" type error.
func syntheticSyncPackage() *types.Package {
	pkg := types.NewPackage("sync", "sync")
	intT := types.Typ[types.Int]
	boolT := types.Typ[types.Bool]
	funcT := types.NewSignatureType(nil, nil, nil, nil, nil, false) // func()
	emptyStruct := types.NewStruct(nil, nil)
	param := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	tuple := func(v *types.Var) *types.Tuple {
		return types.NewTuple(v)
	}

	// Locker interface (sync.Locker): result type of RWMutex.RLocker only.
	// Never callable in M2 (RLocker is out of scope); the interface just
	// needs to exist for the signature to type-check.
	locker := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Lock",
			types.NewSignatureType(nil, nil, nil, nil, nil, false)),
		types.NewFunc(token.NoPos, pkg, "Unlock",
			types.NewSignatureType(nil, nil, nil, nil, nil, false)),
	}, nil).Complete()

	newPrimitive := func(name string) *types.Named {
		tname := types.NewTypeName(token.NoPos, pkg, name, nil)
		pkg.Scope().Insert(tname)
		return types.NewNamed(tname, emptyStruct, nil)
	}
	addMethod := func(named *types.Named, name string, params, results *types.Tuple) {
		recv := types.NewParam(token.NoPos, pkg, "", types.NewPointer(named))
		sig := types.NewSignatureType(recv, nil, nil, params, results, false)
		named.AddMethod(types.NewFunc(token.NoPos, pkg, name, sig))
	}

	mutex := newPrimitive("Mutex")
	addMethod(mutex, "Lock", nil, nil)
	addMethod(mutex, "Unlock", nil, nil)
	addMethod(mutex, "TryLock", nil, tuple(param("", boolT)))

	rwmutex := newPrimitive("RWMutex")
	addMethod(rwmutex, "Lock", nil, nil)
	addMethod(rwmutex, "Unlock", nil, nil)
	addMethod(rwmutex, "RLock", nil, nil)
	addMethod(rwmutex, "RUnlock", nil, nil)
	addMethod(rwmutex, "TryLock", nil, tuple(param("", boolT)))
	addMethod(rwmutex, "TryRLock", nil, tuple(param("", boolT)))
	addMethod(rwmutex, "RLocker", nil, tuple(param("", locker)))

	wg := newPrimitive("WaitGroup")
	addMethod(wg, "Add", tuple(param("delta", intT)), nil)
	addMethod(wg, "Done", nil, nil)
	addMethod(wg, "Wait", nil, nil)
	addMethod(wg, "Go", tuple(param("f", funcT)), nil)

	once := newPrimitive("Once")
	addMethod(once, "Do", tuple(param("f", funcT)), nil)

	pkg.MarkComplete()
	return pkg
}

// ExportFile type-checks the single Go source file at path and returns the
// frontend/v1 JSON document.
func ExportFile(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("gofrontend: %v", err)
	}
	if fi.IsDir() {
		return "", fmt.Errorf("gofrontend: package directories are not supported in frontend/v1 scope (WS G): %s", path)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("gofrontend: %v", err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.AllErrors)
	if err != nil {
		return "", fmt.Errorf("gofrontend: parse error: %v", err)
	}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"`")
		// M5 (WS F): the single import "runtime" is allowed — resolved by
		// overlayImporter to a synthetic package exposing runtime.Goexit.
		// M6 (WS H): the import "fmt" is allowed — resolved by
		// overlayImporter to the D4 fmt stub (seven pure functions).
		// P3-M2b (WS D, ADR-0002): the import "sync" is allowed — resolved
		// by overlayImporter to the synthetic sync package (the four
		// Phase-2-bound primitives).
		// Every other import is still a scope error (WS G import overlay).
		if path != "runtime" && path != "fmt" && path != "sync" {
			return "", fmt.Errorf("gofrontend: import %s: imports are not supported in frontend/v1 scope (WS G import overlay)", imp.Path.Value)
		}
	}

	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Implicits:  make(map[ast.Node]types.Object),
		// M6 (WS H): go/types records generic instantiations here only
		// when the map is non-nil (verified by probe).
		Instances: make(map[*ast.Ident]types.Instance),
	}
	conf := types.Config{Importer: failImporter{}}
	// M5 (WS F): with the "runtime" import present, resolve it via the
	// synthetic overlay package (runtime.Goexit only).
	// M6 (WS H): with the "fmt" import present, resolve it via the
	// synthetic overlay package (D4 fmt stub).
	// P3-M2b (WS D, ADR-0002): with the "sync" import present, resolve it
	// via the synthetic overlay package (four primitives).
	for _, imp := range file.Imports {
		p := strings.Trim(imp.Path.Value, "\"`")
		if p == "runtime" || p == "fmt" || p == "sync" {
			conf.Importer = overlayImporter{}
			break
		}
	}
	if _, err := conf.Check("main", fset, []*ast.File{file}, info); err != nil {
		return "", fmt.Errorf("gofrontend: type error: %v", err)
	}

	e := &exporter{
		fset:     fset,
		src:      src,
		info:     info,
		typeIDs:  make(map[types.Type]int),
		namedIDs: make(map[string]int),
		objIDs:   make(map[types.Object]int),
		sizes:    types.SizesFor("gc", "amd64"),
	}
	doc := Doc{
		Frontend:  frontendVersion,
		Generator: Generator{Tool: "gofrontend", Version: toolVersion, GoRoot: runtime.Version()},
		Package:   Pkg{Name: file.Name.Name, Files: []string{filepath.Base(path)}, Lang: langVersion},
		Types:     []TypeEnt{},
		Objects:   []ObjectEnt{},
		Funcs:     []*Node{},
	}
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			// M5 (WS F): the single allowed import ("runtime", for
			// runtime.Goexit) is a GenDecl; it was validated above and
			// carries no code to export. Type declarations are also
			// GenDecls; v1 supports only struct types without methods
			// (named types for panic rendering and type assertions).
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
				continue
			}
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						return "", scopeErrorf("package-level type spec %T is not supported (WS D)", spec)
					}
					if _, err := e.typeID(e.info.Types[ts.Type].Type); err != nil {
						return "", err
					}
				}
				continue
			}
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.CONST {
				// M7: package-level constants need no codegen — every
				// reference folds through go/types (the *types.Const case
				// in exportIdent), which has already evaluated them
				// (iota included). Validate the specs are value specs.
				for _, spec := range gd.Specs {
					if _, ok := spec.(*ast.ValueSpec); !ok {
						return "", scopeErrorf("package-level const spec %T is not supported (M7)", spec)
					}
				}
				continue
			}
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
				// M7: package-level vars become globals; initializers are
				// exported after the declaration scan (dependency order).
				if err := e.collectPackageVars(gd); err != nil {
					return "", err
				}
				continue
			}
			return "", scopeErrorf("package-level %T is not supported in frontend/v1 scope (WS G/H)", decl)
		}
		_ = fd // collected below (M6, WS H)
	}
	// M6 (WS H): collect function/method declarations, enumerate
	// monomorphized instantiations, then emit in source order (declared
	// functions) followed by deterministic instantiation order.
	funcs, funcByObj, methodIndex, err := e.collectFuncDecls(file)
	if err != nil {
		return "", err
	}
	e.funcByObj = funcByObj
	e.methodIndex = methodIndex
	insts, err := e.enumerateInstantiations(file, funcs)
	if err != nil {
		return "", err
	}
	e.instByKey = make(map[string]*instantiation, len(insts))
	for _, inst := range insts {
		e.instByKey[inst.key] = inst
	}
	// Assign instantiation object ids before any body is emitted: call
	// sites resolve to these ids during body export.
	for _, inst := range insts {
		if err := e.prepareInstantiation(inst); err != nil {
			return "", err
		}
	}
	// M7: package-level vars — dependency-order initialization. This runs
	// after instantiation enumeration so initializers can call
	// package-level functions (funcByObj is populated) and generic
	// instantiations inside initializers are already enqueued.
	globals, err := e.exportPackageVars()
	if err != nil {
		return "", err
	}
	doc.Globals = globals
	hasMain := false
	for _, fi := range funcs {
		if fi.isTemplate {
			continue
		}
		f, err := e.exportFuncDecl(fi)
		if err != nil {
			return "", err
		}
		doc.Funcs = append(doc.Funcs, f)
		if fi.fd.Name.Name == "main" && fi.recv == nil {
			hasMain = true
		}
	}
	for _, inst := range insts {
		f, err := e.exportInstantiation(inst)
		if err != nil {
			return "", err
		}
		doc.Funcs = append(doc.Funcs, f)
	}
	if !hasMain {
		return "", scopeErrorf("no func main found (frontend/v1 scope: single-file program with func main)")
	}
	doc.Types = e.types
	doc.Objects = e.objects
	if doc.Objects == nil {
		doc.Objects = []ObjectEnt{}
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("gofrontend: JSON encoding failed: %v", err)
	}
	return string(out), nil
}

func scopeErrorf(format string, args ...any) error {
	return fmt.Errorf("gofrontend: "+format, args...)
}

// checkVariadicCall rejects non-spread calls to variadic signatures.
// Remediation (R1 P1-1, R2 P1-2): the spread form f(s...) passes the slice
// directly and is implemented; zero/single/multi-arg calls need argument
// packing adaptation that v1 does not implement. Letting them through
// produced internal ArrayIndexOutOfBoundsException/ClassCastException at
// runtime instead of the clean scope error frontend-v1.md §7 requires.
// (The fmt overlay handles its own variadic builtins and is not routed
// through this check.)
func checkVariadicCall(c *ast.CallExpr, sig *types.Signature) error {
	if sig != nil && sig.Variadic() && !c.Ellipsis.IsValid() {
		return scopeErrorf("call to variadic function without ... spread" +
			" (argument packing) is not supported in frontend/v1 scope (WS D)")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Package-level variables (M7).
// ---------------------------------------------------------------------------

// collectPackageVars records one package-level `var` GenDecl. Variables are
// defined in the object table now (so a later initializer can reference an
// earlier variable); initializer export and dependency ordering happen in
// exportPackageVars after the full declaration scan.
func (e *exporter) collectPackageVars(gd *ast.GenDecl) error {
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			return scopeErrorf("package-level var spec %T is not supported (M7)", spec)
		}
		pv := &pkgVarDecl{spec: vs}
		for _, n := range vs.Names {
			if n.Name == "_" {
				continue
			}
			v, ok := e.info.Defs[n].(*types.Var)
			if !ok {
				return fmt.Errorf("gofrontend: package var %q has no *types.Var object", n.Name)
			}
			oid, err := e.defVar(v, false)
			if err != nil {
				return err
			}
			pv.vars = append(pv.vars, v)
			pv.oids = append(pv.oids, oid)
		}
		e.pkgVars = append(e.pkgVars, pv)
	}
	return nil
}

// exportPackageVars exports initializers for the collected package-level
// vars and returns the document's Globals in initialization (dependency)
// order.
//
// Go spec (Package initialization): "Given the list of all variables, the
// initialization order is determined by analyzing dependencies" — x depends
// on y when x's initializer refers to y (only references to package-level
// variables are ordering constraints, and functions need no initialization).
// The dependency walk covers the whole initializer expression, including
// funclit bodies (ast.Inspect does not skip them) — matching gc, which
// rejects `var f = func() int { return g }; var g = f` as an initialization
// cycle. Unconstrained variables initialize in declaration order. A
// dependency cycle is a build error, matching gc's "initialization cycle"
// rejection.
func (e *exporter) exportPackageVars() ([]GlobalEnt, error) {
	type ginit struct {
		oid  int      // object id, or -1 for blank (`var _ = ...`)
		init *Node    // exported initializer, nil when absent (zero value)
		expr ast.Expr // source initializer, nil when absent
		pos  *Pos     // source position of the declaration
	}
	inits := []ginit{}
	for _, pv := range e.pkgVars {
		vs := pv.spec
		specPos := e.pos(vs)
		if len(vs.Values) != 0 && len(vs.Values) != len(vs.Names) {
			// Multi-valued initializers (`var a, b = f()`) are WS D.
			return nil, scopeErrorf(
				"package-level var with multi-valued initializer is not supported in v1 (WS D)")
		}
		for i, n := range vs.Names {
			var initNode *Node
			var initExpr ast.Expr
			if i < len(vs.Values) {
				initExpr = vs.Values[i]
				x, err := e.exportExpr(initExpr)
				if err != nil {
					return nil, err
				}
				initNode = x
			} else {
				// P3-M2b (ADR-0002 D3): package-level `var m sync.Mutex`
				// (no init) normalizes to an implicit syncnew — gc's
				// ready-to-use zero value — mirroring the local-var path.
				// Without it the frozen factory's struct zero (GoStruct)
				// would be installed and every method call would CCE.
				if v, ok := e.info.Defs[n].(*types.Var); ok {
					if nt := syncPrimitiveNamed(v.Type()); nt != nil {
						tid, err := e.typeID(v.Type())
						if err != nil {
							return nil, err
						}
						initNode = e.node("syncnew", vs, map[string]any{
							"synctyp": nt.Obj().Name(), "type": tid,
						})
					}
				}
			}
			if n.Name == "_" {
				// Blank: no variable, but the initializer is evaluated
				// for side effects (Go spec, Package initialization).
				if initNode != nil {
					inits = append(inits, ginit{oid: -1, init: initNode, expr: initExpr, pos: specPos})
				}
				continue
			}
			v, ok := e.info.Defs[n].(*types.Var)
			if !ok {
				return nil, fmt.Errorf("gofrontend: package var %q has no *types.Var object", n.Name)
			}
			oid, ok := e.objIDs[v]
			if !ok {
				return nil, fmt.Errorf("gofrontend: package var %q missing from object table", n.Name)
			}
			inits = append(inits, ginit{oid: oid, init: initNode, expr: initExpr, pos: specPos})
		}
	}

	// Dependency analysis: the package-level variables each initializer
	// refers to (via go/types Uses — selectors and calls are covered
	// because the referenced variable appears as an *ast.Ident).
	pkgVar := map[int]bool{}
	for _, g := range inits {
		if g.oid >= 0 {
			pkgVar[g.oid] = true
		}
	}
	deps := make([][]int, len(inits))
	for i, g := range inits {
		if g.expr == nil {
			continue
		}
		seen := map[int]bool{}
		ast.Inspect(g.expr, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if v, ok := e.info.Uses[id].(*types.Var); ok {
				if oid, ok := e.objIDs[v]; ok && pkgVar[oid] && !seen[oid] {
					seen[oid] = true
					deps[i] = append(deps[i], oid)
				}
			}
			return true
		})
	}

	// Topological order, stable by declaration order: at each step the
	// earliest-declared variable whose dependencies are all initialized
	// goes next (this matches gc's source-order tie-breaking). A
	// self-reference is a cycle, like any other.
	oidx := map[int]int{}
	for i, g := range inits {
		if g.oid >= 0 {
			oidx[g.oid] = i
		}
	}
	indeg := make([]int, len(inits))
	dependents := make([][]int, len(inits))
	for i, ds := range deps {
		for _, d := range ds {
			j := oidx[d]
			dependents[j] = append(dependents[j], i)
			indeg[i]++
		}
	}
	order := make([]int, 0, len(inits))
	done := make([]bool, len(inits))
	for len(order) < len(inits) {
		next := -1
		for i := range inits {
			if !done[i] && indeg[i] == 0 {
				next = i
				break
			}
		}
		if next < 0 {
			break // cycle; reported below
		}
		done[next] = true
		order = append(order, next)
		for _, k := range dependents[next] {
			indeg[k]--
		}
	}
	if len(order) < len(inits) {
		cycle := []string{}
		for i := range inits {
			if indeg[i] > 0 && inits[i].oid >= 0 {
				cycle = append(cycle, e.objects[inits[i].oid].Name)
			}
		}
		return nil, scopeErrorf("initialization cycle involving package-level var(s): %s",
			strings.Join(cycle, ", "))
	}

	out := make([]GlobalEnt, 0, len(inits))
	for _, i := range order {
		out = append(out, GlobalEnt{Obj: inits[i].oid, Init: inits[i].init, Pos: inits[i].pos})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Exporter state.
// ---------------------------------------------------------------------------

type exporter struct {
	fset    *token.FileSet
	src     []byte // raw source bytes of the single input file (M8: rune-based positions)
	info    *types.Info
	types   []TypeEnt
	typeIDs map[types.Type]int
	objects []ObjectEnt
	objIDs  map[types.Object]int
	sizes   types.Sizes
	// M6 (WS H): active type-parameter substitution while exporting a
	// monomorphized instantiation body (nil outside instantiations). See
	// typeSubst below.
	subst *typeSubst
	// M6 (WS H): per-instantiation type memo, keyed by unsubstituted type.
	// The global typeIDs memo is only valid without substitution.
	substTypeIDs map[types.Type]int
	// M6 (WS H, issue52026): named types deduplicated by qualified name
	// (including substituted type arguments). A substituted *types.Named
	// (e.g. Some[T] under T=int) is a fresh go/types object, but it denotes
	// the same type as the Some[int] from non-generic code — they must share
	// one wire type id, or type assertions on monomorphized values fail.
	namedIDs map[string]int
	// M6 (WS H): top-level function/method declarations by object, and
	// monomorphized instantiations by dedup key (see enumerateInstantiations).
	funcByObj map[*types.Func]*funcDeclInfo
	// M6 (WS H): method declarations by (receiver type name, method name).
	methodIndex map[methodKey]*funcDeclInfo
	instByKey   map[string]*instantiation
	// M7: package-level var declarations in source order (collected by
	// collectPackageVars during the declaration scan; exported in
	// dependency order by exportPackageVars after the scan).
	pkgVars []*pkgVarDecl
}

// M10 (P1-4): struct-nesting depth cap for offset computation.
// Measured: 10-deep ~165us, 20-deep ~810ms (2^k blowup in go/types'
// unmemoized gcSizes). 16 is safely below the pain point; deeper structs
// get -1 offsets (position-indexed, never byte-offset, per the M6 comment
// below).
const maxStructDepth = 16

// structValueDepth estimates the value-nesting depth of t without calling
// Sizeof (which is exponential on deep nesting). Structs add one level per
// nesting; arrays add one; other composites are O(1). Cycle-safe via the
// depth cap: returns maxStructDepth+1 if the cap is exceeded.
func structValueDepth(t types.Type, depth int) int {
	if depth > maxStructDepth {
		return depth
	}
	switch t := t.(type) {
	case *types.Named:
		return structValueDepth(t.Underlying(), depth)
	case *types.Struct:
		max := depth
		for i := 0; i < t.NumFields(); i++ {
			if d := structValueDepth(t.Field(i).Type(), depth+1); d > max {
				max = d
			}
			if max > maxStructDepth {
				return max
			}
		}
		return max
	case *types.Array:
		return structValueDepth(t.Elem(), depth+1)
	default:
		return depth
	}
}

// pkgVarDecl is one package-level `var` spec (M7).
type pkgVarDecl struct {
	spec *ast.ValueSpec
	vars []*types.Var // non-blank declared variables, in source order
	oids []int        // object ids parallel to vars
	// blankInit is set when the spec declares only blank identifiers:
	// the values are still evaluated for side effects (Go spec).
	blankInit []ast.Expr
}

// ---------------------------------------------------------------------------
// Type-parameter substitution (M6, WS H).
// ---------------------------------------------------------------------------
//
// Monomorphization (PLAN.md D2) emits one function-body subtree per
// go/types instantiation. A typeSubst binds a template's type parameters to
// their actual arguments; the chain links nested instantiations (a generic
// function instantiated from inside another generic function's body). Bind
// values are written in the PARENT scope (they come from info.Instances
// recorded inside the parent's body), so resolve() returns both the bound
// type and the scope to interpret it in.
//
// Termination: every instantiation the enumerator produces is fully
// concrete — go/types rejects instantiation cycles ("instantiation cycle"
// is a type error, verified against gc 1.27.1), so the fixpoint below always
// terminates for accepted programs.

type typeSubst struct {
	parent *typeSubst
	bind   map[*types.TypeParam]types.Type
}

// resolve returns the type bound to tp and the scope it lives in
// (nil scope = top level, no substitution active).
func (s *typeSubst) resolve(tp *types.TypeParam) (types.Type, *typeSubst) {
	for ss := s; ss != nil; ss = ss.parent {
		if c, ok := ss.bind[tp]; ok {
			return c, ss.parent
		}
	}
	return nil, nil
}

// bindParams builds the bind map pairing a type-parameter list with its
// actual arguments.
func bindParams(tparams *types.TypeParamList, targs []types.Type) map[*types.TypeParam]types.Type {
	bind := make(map[*types.TypeParam]types.Type, len(targs))
	for i := 0; i < tparams.Len() && i < len(targs); i++ {
		bind[tparams.At(i)] = targs[i]
	}
	return bind
}

// substString renders t as a canonical structural string with type
// parameters resolved through s. It is the dedup key for instantiations
// (go/types object identity is not stable across use sites) and the basis
// for instantiation display names. Two structurally identical types render
// identically, so identical instantiations discovered via different paths
// deduplicate.
func substString(t types.Type, s *typeSubst) string {
	var sb strings.Builder
	writeSubstString(&sb, t, s)
	return sb.String()
}

func writeSubstString(sb *strings.Builder, t types.Type, s *typeSubst) {
	switch t := t.(type) {
	case *types.TypeParam:
		if s != nil {
			if c, scope := s.resolve(t); c != nil {
				writeSubstString(sb, c, scope)
				return
			}
		}
		// Unbound under substitution: defensive marker. Instantiation keys
		// containing it are rejected by the enumerator (isConcreteKey).
		sb.WriteString("!typeparam:" + t.Obj().Name() + "!")
	case *types.Named:
		sb.WriteString("named:" + qualifiedName(t.Obj()) + "[")
		args := t.TypeArgs()
		for i := 0; i < args.Len(); i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			writeSubstString(sb, args.At(i), s)
		}
		sb.WriteString("]")
	case *types.Pointer:
		sb.WriteString("ptr:")
		writeSubstString(sb, t.Elem(), s)
	case *types.Slice:
		sb.WriteString("slice:")
		writeSubstString(sb, t.Elem(), s)
	case *types.Array:
		sb.WriteString("array:")
		sb.WriteString(fmt.Sprint(t.Len()))
		sb.WriteString(":")
		writeSubstString(sb, t.Elem(), s)
	case *types.Map:
		sb.WriteString("map:")
		writeSubstString(sb, t.Key(), s)
		sb.WriteString(":")
		writeSubstString(sb, t.Elem(), s)
	case *types.Chan:
		sb.WriteString("chan:")
		sb.WriteString(fmt.Sprint(t.Dir()))
		sb.WriteString(":")
		writeSubstString(sb, t.Elem(), s)
	case *types.Signature:
		sb.WriteString("sig:(")
		writeSubstTuple(sb, t.Params(), s)
		sb.WriteString("):(")
		writeSubstTuple(sb, t.Results(), s)
		sb.WriteString(")")
		if t.Variadic() {
			sb.WriteString(":variadic")
		}
	case *types.Basic:
		sb.WriteString("basic:" + t.Name())
	default:
		// Interfaces, unions, aliases: go/types rendering is deterministic
		// and these never carry substitutable type params in v1 scope.
		sb.WriteString("other:" + t.String())
	}
}

func writeSubstTuple(sb *strings.Builder, tup *types.Tuple, s *typeSubst) {
	for i := 0; i < tup.Len(); i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		writeSubstString(sb, tup.At(i).Type(), s)
	}
}

// isConcreteKey reports whether a dedup key resolved fully (no unbound
// type parameter markers).
func isConcreteKey(key string) bool {
	return !strings.Contains(key, "!typeparam:")
}

// joinSubstKeys renders type args for keys and display names.
func joinSubstKeys(targs []types.Type, s *typeSubst) string {
	parts := make([]string, len(targs))
	for i, ta := range targs {
		parts[i] = substString(ta, s)
	}
	return strings.Join(parts, ",")
}

func (e *exporter) pos(n ast.Node) *Pos {
	s := e.fset.Position(n.Pos())
	t := e.fset.Position(n.End())
	col, length := s.Column, t.Offset-s.Offset
	// M8: Truffle Source.createSection(line, col, len) is char-based, but
	// go/token positions are byte-based. For non-ASCII sources the byte
	// length can exceed the char length, and createSection throws
	// IllegalArgumentException ("charIndex out of range") — a hard crash on
	// valid programs (found by the M8 fuzzer: fmt.Println("日本語")).
	// Emit rune-based col/len; off stays a byte offset (NodeFactory ignores it).
	if e.src != nil && s.IsValid() && t.IsValid() {
		if f := e.fset.File(n.Pos()); f != nil && s.Offset <= t.Offset && t.Offset <= len(e.src) {
			if lo := f.Offset(f.LineStart(s.Line)); lo <= s.Offset {
				col = utf8.RuneCount(e.src[lo:s.Offset]) + 1
			}
			length = utf8.RuneCount(e.src[s.Offset:t.Offset])
		}
	}
	return &Pos{File: 0, Line: s.Line, Col: col, Off: s.Offset, Len: length}
}

func (e *exporter) node(kind string, n ast.Node, fields map[string]any) *Node {
	return &Node{Kind: kind, Pos: e.pos(n), Fields: fields}
}

// defVar records a *types.Var in the object table (idempotent) and returns
// its object id.
func (e *exporter) defVar(v *types.Var, param bool) (int, error) {
	if id, ok := e.objIDs[v]; ok {
		return id, nil
	}
	t, err := e.typeID(v.Type())
	if err != nil {
		return 0, err
	}
	id := len(e.objects)
	e.objects = append(e.objects, ObjectEnt{Kind: "var", Name: v.Name(), Type: t, Param: param})
	e.objIDs[v] = id
	return id, nil
}

// defFunc records a top-level function or method object in the object table
// (idempotent) and returns its object id (M6, WS H).
func (e *exporter) defFunc(obj *types.Func) (int, error) {
	if id, ok := e.objIDs[obj]; ok {
		return id, nil
	}
	t, err := e.typeID(obj.Type())
	if err != nil {
		return 0, err
	}
	id := len(e.objects)
	e.objects = append(e.objects, ObjectEnt{Kind: "func", Name: obj.Name(), Type: t})
	e.objIDs[obj] = id
	return id, nil
}

// ---------------------------------------------------------------------------
// Type table.
// ---------------------------------------------------------------------------

// sizesWouldPanic reports whether asking go/types for the size/alignment of
// t would panic: gcSizes asserts on bare type parameters, which are reached
// by descending through array element types, struct field types, and named
// type underlyings. (Slice/map/pointer/chan positions are fine — gcSizes
// returns WordSize without descending.)
func sizesWouldPanic(t types.Type) bool {
	seen := make(map[types.Type]bool)
	var walk func(t types.Type) bool
	walk = func(t types.Type) bool {
		if t == nil {
			return false
		}
		if _, ok := t.(*types.TypeParam); ok {
			return true
		}
		if seen[t] {
			return false
		}
		seen[t] = true
		switch u := t.(type) {
		case *types.Named:
			return walk(u.Underlying())
		case *types.Array:
			return walk(u.Elem())
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				if walk(u.Field(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	return walk(t)
}

// typeID returns the type-table id for t, serializing t on first sight.
// Recursive types are handled by registering the id before describing.
//
// M6 (WS H): while exporting a monomorphized instantiation (e.subst != nil),
// bound type parameters resolve through the substitution chain before the
// per-instantiation memo is consulted. The global typeIDs memo is only
// valid without substitution: the same unsubstituted type denotes different
// concrete types under different substitutions.
// substType returns t with every type parameter bound in e.subst replaced
// by its binding (M6, WS H — issue52026). Bindings resolve in the scope
// where they were written (the chain links nested instantiations). Returns
// t unchanged when no substitution is active or t contains no bound type
// parameter. Composite types are rebuilt only when a component changed, so
// concrete types keep their canonical go/types identity.
func (e *exporter) substType(t types.Type) types.Type {
	if e.subst == nil {
		return t
	}
	switch t := t.(type) {
	case *types.TypeParam:
		if c, scope := e.subst.resolve(t); c != nil {
			saved := e.subst
			e.subst = scope
			c = e.substType(c)
			e.subst = saved
			return c
		}
		return t
	case *types.Named:
		targs := t.TypeArgs()
		if targs == nil || targs.Len() == 0 {
			return t
		}
		changed := false
		newArgs := make([]types.Type, targs.Len())
		for i := 0; i < targs.Len(); i++ {
			newArgs[i] = e.substType(targs.At(i))
			if newArgs[i] != targs.At(i) {
				changed = true
			}
		}
		if !changed {
			return t
		}
		inst, err := types.Instantiate(nil, t.Origin(), newArgs, false)
		if err != nil {
			return t // defensive; describeType fails loudly on bad types
		}
		return inst
	case *types.Pointer:
		if elem := e.substType(t.Elem()); elem != t.Elem() {
			return types.NewPointer(elem)
		}
		return t
	case *types.Slice:
		if elem := e.substType(t.Elem()); elem != t.Elem() {
			return types.NewSlice(elem)
		}
		return t
	case *types.Array:
		if elem := e.substType(t.Elem()); elem != t.Elem() {
			return types.NewArray(elem, t.Len())
		}
		return t
	case *types.Map:
		key, elem := e.substType(t.Key()), e.substType(t.Elem())
		if key != t.Key() || elem != t.Elem() {
			return types.NewMap(key, elem)
		}
		return t
	case *types.Chan:
		if elem := e.substType(t.Elem()); elem != t.Elem() {
			return types.NewChan(t.Dir(), elem)
		}
		return t
	case *types.Signature:
		params, results := t.Params(), t.Results()
		changed := false
		newParams := make([]*types.Var, params.Len())
		for i := 0; i < params.Len(); i++ {
			v := params.At(i)
			if nt := e.substType(v.Type()); nt != v.Type() {
				changed = true
				newParams[i] = types.NewVar(v.Pos(), v.Pkg(), v.Name(), nt)
			} else {
				newParams[i] = v
			}
		}
		newResults := make([]*types.Var, results.Len())
		for i := 0; i < results.Len(); i++ {
			v := results.At(i)
			if nt := e.substType(v.Type()); nt != v.Type() {
				changed = true
				newResults[i] = types.NewVar(v.Pos(), v.Pkg(), v.Name(), nt)
			} else {
				newResults[i] = v
			}
		}
		if !changed {
			return t
		}
		return types.NewSignatureType(t.Recv(), typeParamSlice(t.RecvTypeParams()),
			typeParamSlice(t.TypeParams()), types.NewTuple(newParams...),
			types.NewTuple(newResults...), t.Variadic())
	case *types.Struct:
		changed := false
		fields := make([]*types.Var, t.NumFields())
		tags := make([]string, t.NumFields())
		for i := 0; i < t.NumFields(); i++ {
			v := t.Field(i)
			if nt := e.substType(v.Type()); nt != v.Type() {
				changed = true
				fields[i] = types.NewField(v.Pos(), v.Pkg(), v.Name(), nt, v.Embedded())
			} else {
				fields[i] = v
			}
			tags[i] = t.Tag(i)
		}
		if !changed {
			return t
		}
		return types.NewStruct(fields, tags)
	default:
		return t
	}
}

// typeParamSlice converts a *types.TypeParamList to a slice.
func typeParamSlice(l *types.TypeParamList) []*types.TypeParam {
	if l == nil {
		return nil
	}
	s := make([]*types.TypeParam, l.Len())
	for i := 0; i < l.Len(); i++ {
		s[i] = l.At(i)
	}
	return s
}

// namedTypeName renders the qualified name of a named type including its
// (already substituted) type arguments — the canonical identity key.
func namedTypeName(nt *types.Named) string {
	name := qualifiedName(nt.Obj())
	if targs := nt.TypeArgs(); targs != nil && targs.Len() > 0 {
		args := make([]string, targs.Len())
		for i := 0; i < targs.Len(); i++ {
			args[i] = qualifiedNameForType(targs.At(i))
		}
		name += "[" + strings.Join(args, ", ") + "]"
	}
	return name
}

func (e *exporter) typeID(t types.Type) (int, error) {
	if e.subst != nil {
		// M6 (WS H, issue52026): substitute bound type parameters before
		// the memo lookup, so Some[T] under T=int denotes Some[int].
		// The substituted *types.Named is a fresh object — deduplicate
		// named types by qualified name, or the monomorphized body and
		// the call site would mint different ids for the same type.
		t = e.substType(t)
		if nt, ok := t.(*types.Named); ok {
			name := namedTypeName(nt)
			if id, ok := e.namedIDs[name]; ok {
				return id, nil
			}
			id := len(e.types)
			e.types = append(e.types, TypeEnt{})
			e.namedIDs[name] = id
			ent, err := e.describeType(t)
			if err != nil {
				return 0, err
			}
			e.types[id] = ent
			return id, nil
		}
		if id, ok := e.substTypeIDs[t]; ok {
			return id, nil
		}
		id := len(e.types)
		e.types = append(e.types, TypeEnt{})
		e.substTypeIDs[t] = id
		ent, err := e.describeType(t)
		if err != nil {
			return 0, err
		}
		e.types[id] = ent
		return id, nil
	}
	if nt, ok := t.(*types.Named); ok {
		// Named types deduplicate by qualified name globally as well, so a
		// substituted Some[int] (via namedIDs) shares the id with the
		// Some[int] from non-generic code (issue52026).
		name := namedTypeName(nt)
		if id, ok := e.namedIDs[name]; ok {
			return id, nil
		}
		id := len(e.types)
		e.types = append(e.types, TypeEnt{})
		e.namedIDs[name] = id
		ent, err := e.describeType(t)
		if err != nil {
			return 0, err
		}
		e.types[id] = ent
		return id, nil
	}
	if id, ok := e.typeIDs[t]; ok {
		return id, nil
	}
	id := len(e.types)
	e.types = append(e.types, TypeEnt{})
	e.typeIDs[t] = id
	ent, err := e.describeType(t)
	if err != nil {
		return 0, err
	}
	e.types[id] = ent
	return id, nil
}

func basicClass(b *types.Basic) (class string, untyped bool) {
	if b.Kind() == types.UnsafePointer {
		return "pointer", false
	}
	info := b.Info()
	untyped = info&types.IsUntyped != 0
	switch {
	case info&types.IsBoolean != 0:
		return "bool", untyped
	case info&types.IsInteger != 0:
		if info&types.IsUnsigned != 0 {
			return "uint", untyped
		}
		return "int", untyped
	case info&types.IsFloat != 0:
		return "float", untyped
	case info&types.IsComplex != 0:
		return "complex", untyped
	case info&types.IsString != 0:
		return "string", untyped
	}
	return "unknown", untyped
}

func qualifiedName(obj *types.TypeName) string {
	if pkg := obj.Pkg(); pkg != nil {
		return pkg.Path() + "." + obj.Name()
	}
	return obj.Name()
}

// qualifiedNameForType returns the qualified name for a type argument
// (e.g. "main.foo" for type foo int). Used in instantiated type names.
// Named type arguments render WITH their own type arguments (via
// namedTypeName): dropping them collapses Box[Box[int]] and Box[Box[string]]
// to the same dedup key, which mints a cyclic type graph and overflows the
// frontend (M10 self-attack: 4-deep Box nesting). The recursion terminates:
// type arguments form a finite tree.
func qualifiedNameForType(t types.Type) string {
	if n, ok := t.(*types.Named); ok {
		return namedTypeName(n)
	}
	if b, ok := t.(*types.Basic); ok {
		return b.Name()
	}
	return types.TypeString(t, nil)
}

func (e *exporter) tupleList(t *types.Tuple) ([]any, error) {
	out := make([]any, 0, t.Len())
	for i := 0; i < t.Len(); i++ {
		v := t.At(i)
		tid, err := e.typeID(v.Type())
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"name": v.Name(), "type": tid})
	}
	return out, nil
}

func (e *exporter) describeType(t types.Type) (TypeEnt, error) {
	mk := func(kind string, detail map[string]any) TypeEnt {
		return TypeEnt{Kind: kind, Detail: detail}
	}
	switch t := t.(type) {
	case *types.Basic:
		class, untyped := basicClass(t)
		return mk("basic", map[string]any{
			"name": t.Name(), "class": class, "untyped": untyped,
		}), nil
	case *types.Named:
		// M6 (WS H): named types may have methods (emitted as funcdecls /
		// instantiations; dispatch resolves by object id). The methods list
		// stays empty here — populated by WS D per docs/frontend-v1.md.
		// M6: instantiated generic types (e.g. F[foo]) include the type
		// arguments in the name for %T formatting (issue49547).
		// P2-3: single renderer (namedTypeName) — a second copy here would
		// silently mint two wire ids for one type if they ever drifted,
		// breaking the t == dyn identity B2a/B4 rest on.
		under, err := e.typeID(t.Underlying())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("named", map[string]any{
			"name": namedTypeName(t), "underlying": under, "methods": []any{},
		}), nil
	case *types.Pointer:
		elem, err := e.typeID(t.Elem())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("pointer", map[string]any{"elem": elem}), nil
	case *types.Slice:
		// P3-M2b (ADR-0002 D3): the factory manufactures element zeros
		// (frozen); a sync primitive element would zero to an unusable
		// empty struct. *sync.X elements are fine (nil is correct).
		if nt := syncPrimitiveNamed(t.Elem()); nt != nil {
			return TypeEnt{}, scopeErrorf("sync.%s as a slice element is not in M2 scope", nt.Obj().Name())
		}
		elem, err := e.typeID(t.Elem())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("slice", map[string]any{"elem": elem}), nil
	case *types.Array:
		// P3-M2b (ADR-0002 D3): as for slices — array zeros are factory-made.
		if nt := syncPrimitiveNamed(t.Elem()); nt != nil {
			return TypeEnt{}, scopeErrorf("sync.%s as an array element is not in M2 scope", nt.Obj().Name())
		}
		elem, err := e.typeID(t.Elem())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("array", map[string]any{"len": t.Len(), "elem": elem}), nil
	case *types.Map:
		// P3-M2b (ADR-0002 D3): missing-key reads manufacture the element
		// zero in the factory; keys compare by value semantics the opaque
		// model cannot provide. Both positions scope-error.
		if nt := syncPrimitiveNamed(t.Key()); nt != nil {
			return TypeEnt{}, scopeErrorf("sync.%s as a map key is not in M2 scope", nt.Obj().Name())
		}
		if nt := syncPrimitiveNamed(t.Elem()); nt != nil {
			return TypeEnt{}, scopeErrorf("sync.%s as a map element is not in M2 scope", nt.Obj().Name())
		}
		key, err := e.typeID(t.Key())
		if err != nil {
			return TypeEnt{}, err
		}
		elem, err := e.typeID(t.Elem())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("map", map[string]any{"key": key, "elem": elem}), nil
	case *types.Chan:
		var dir string
		switch t.Dir() {
		case types.SendRecv:
			dir = "both"
		case types.SendOnly:
			dir = "send"
		case types.RecvOnly:
			dir = "recv"
		}
		elem, err := e.typeID(t.Elem())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("chan", map[string]any{"dir": dir, "elem": elem}), nil
	case *types.Struct:
		fields := make([]any, 0, t.NumFields())
		vars := make([]*types.Var, 0, t.NumFields())
		for i := 0; i < t.NumFields(); i++ {
			// P3-M2b (ADR-0002 D3): the factory manufactures field zeros
			// (frozen); a sync primitive field would zero to an unusable
			// empty struct. *sync.X fields are fine (nil is correct).
			if nt := syncPrimitiveNamed(t.Field(i).Type()); nt != nil {
				return TypeEnt{}, scopeErrorf("sync.%s as a struct field is not in M2 scope", nt.Obj().Name())
			}
			vars = append(vars, t.Field(i))
		}
		// M6 (WS H): go/types' gcSizes asserts on bare type parameters
		// (Alignof/Sizeof panic), reachable by descending through array
		// element and struct field types — e.g. struct{ t T }. Field
		// offsets of a generic struct are instantiation-dependent anyway,
		// so they are not computed: the wire carries -1 ("unknown"; the
		// factory indexes fields by position, never by byte offset).
		offsets := make([]int64, len(vars))
		generic := false
		for _, v := range vars {
			if sizesWouldPanic(v.Type()) {
				generic = true
				break
			}
		}
		if generic {
			for i := range offsets {
				offsets[i] = -1
			}
		} else if structValueDepth(t, 0) > maxStructDepth {
			// M10 (P1-4): depth cap. Computing real offsets here would
			// invoke go/types' exponential Sizeof; -1 is safe (the
			// factory indexes fields by position, never by byte offset).
			for i := range offsets {
				offsets[i] = -1
			}
		} else {
			offsets = e.sizes.Offsetsof(vars)
		}
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			ft, err := e.typeID(f.Type())
			if err != nil {
				return TypeEnt{}, err
			}
			fields = append(fields, map[string]any{
				"name": f.Name(), "type": ft, "tag": t.Tag(i),
				"embedded": f.Embedded(), "offset": offsets[i],
			})
		}
		return mk("struct", map[string]any{"fields": fields}), nil
	case *types.Signature:
		params, err := e.tupleList(t.Params())
		if err != nil {
			return TypeEnt{}, err
		}
		results, err := e.tupleList(t.Results())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("signature", map[string]any{
			"params": params, "results": results, "variadic": t.Variadic(),
		}), nil
	case *types.Interface:
		methods := make([]any, 0, t.NumMethods())
		for i := 0; i < t.NumMethods(); i++ {
			m := t.Method(i)
			sig, err := e.typeID(m.Type())
			if err != nil {
				return TypeEnt{}, err
			}
			methods = append(methods, map[string]any{"name": m.Name(), "sig": sig})
		}
		embedded := make([]any, 0, t.NumEmbeddeds())
		for i := 0; i < t.NumEmbeddeds(); i++ {
			id, err := e.typeID(t.EmbeddedType(i))
			if err != nil {
				return TypeEnt{}, err
			}
			embedded = append(embedded, id)
		}
		return mk("interface", map[string]any{
			"empty":   t.NumMethods() == 0 && t.NumEmbeddeds() == 0,
			"methods": methods, "embedded": embedded,
		}), nil
	case *types.Union:
		terms := make([]any, 0, t.Len())
		for i := 0; i < t.Len(); i++ {
			term := t.Term(i)
			id, err := e.typeID(term.Type())
			if err != nil {
				return TypeEnt{}, err
			}
			terms = append(terms, map[string]any{"tilde": term.Tilde(), "type": id})
		}
		return mk("union", map[string]any{"terms": terms}), nil
	case *types.TypeParam:
		c, err := e.typeID(t.Constraint())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("typeparam", map[string]any{"name": t.Obj().Name(), "constraint": c}), nil
	case *types.Alias:
		rhs, err := e.typeID(t.Rhs())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("alias", map[string]any{"name": qualifiedName(t.Obj()), "rhs": rhs}), nil
	default:
		return TypeEnt{}, fmt.Errorf("gofrontend: unsupported go/types type %T", t)
	}
}

// ---------------------------------------------------------------------------
// Functions and methods (M6, WS H).
// ---------------------------------------------------------------------------

// funcDeclInfo is one top-level function or method declaration.
type funcDeclInfo struct {
	fd     *ast.FuncDecl
	obj    *types.Func
	funcID int // object-table id of the "func" entry
	sig    *types.Signature
	recv   *types.Var // nil for plain functions (methods: sig.Recv())
	// isTemplate: generic function, method on a generic type, or generic
	// method — emitted only via monomorphized instantiations, never
	// directly.
	isTemplate bool
}

// instantiation is one monomorphized function/method body to emit
// (PLAN.md D2: one RootNode per go/types instantiation).
type instantiation struct {
	decl *funcDeclInfo
	// subst binds the template's type params (function params; for
	// methods, receiver-origin params then method params) to actual args.
	// Values live in the parent scope (chained substitution).
	subst *typeSubst
	// rawTargs are the actual argument types in bind order, for the wire
	// "typeargs" field (resolved through subst at emission).
	rawTargs []types.Type
	key      string // dedup key (deterministic)
	name     string // display name, e.g. "Min[int]", "Stack[int].Size"
	instID   int    // object-table id of the "inst" entry (assigned at emission)
	sigID    int    // type-table id of the instantiated signature (assigned at emission)
	targIDs  []int  // type-table ids of rawTargs (assigned at emission)
}

// methodKey indexes method declarations by receiver type name + method name.
type methodKey struct {
	recv types.Object // the receiver's *types.TypeName object
	name string
}

// derefNamed strips one pointer layer and reports the named type, if any.
func derefNamed(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, _ := t.(*types.Named)
	return n
}

// bindRecvParams binds the type parameters appearing in a method
// declaration's receiver type (the actual *types.TypeParam objects in the
// declared receiver's type arguments — note these are NOT identical to the
// origin's type params, verified by probe) to the call-site receiver's
// type arguments.
func bindRecvParams(bind map[*types.TypeParam]types.Type, fi *funcDeclInfo, callRN *types.Named) error {
	declRN := derefNamed(fi.recv.Type())
	if declRN == nil {
		return fmt.Errorf("gofrontend: method %s has non-named declared receiver", fi.obj.Name())
	}
	declArgs := typeList(declRN.TypeArgs())
	callArgs := typeList(callRN.TypeArgs())
	if len(declArgs) != len(callArgs) {
		return fmt.Errorf("gofrontend: receiver arity mismatch for method %s", fi.obj.Name())
	}
	for i := range declArgs {
		tp, ok := declArgs[i].(*types.TypeParam)
		if !ok {
			continue // concrete in the declaration; nothing to bind
		}
		bind[tp] = callArgs[i]
	}
	return nil
}

// typeList converts a *types.TypeList to a slice.
func typeList(l *types.TypeList) []types.Type {
	if l == nil {
		return nil
	}
	out := make([]types.Type, l.Len())
	for i := 0; i < l.Len(); i++ {
		out[i] = l.At(i)
	}
	return out
}

// denotedFunc returns the *types.Func denoted by id (Uses or Defs).
func denotedFunc(info *types.Info, id *ast.Ident) (*types.Func, bool) {
	obj := info.Uses[id]
	if obj == nil {
		obj = info.Defs[id]
	}
	fn, ok := obj.(*types.Func)
	return fn, ok
}

// unwrapSelector strips index/paren wrappers to find a selector expression
// (method calls with explicit type arguments: s.M[T](...)).
func unwrapSelector(fun ast.Expr) (*ast.SelectorExpr, bool) {
	for {
		switch f := fun.(type) {
		case *ast.SelectorExpr:
			return f, true
		case *ast.IndexExpr:
			fun = f.X
		case *ast.IndexListExpr:
			fun = f.X
		case *ast.ParenExpr:
			fun = f.X
		default:
			return nil, false
		}
	}
}

// callTargetIdent unwraps index/paren expressions to the called identifier,
// or nil for non-identifier call targets (funclits, selector calls, ...).
func callTargetIdent(fun ast.Expr) *ast.Ident {
	for {
		switch f := fun.(type) {
		case *ast.Ident:
			return f
		case *ast.IndexExpr:
			fun = f.X
		case *ast.IndexListExpr:
			fun = f.X
		case *ast.ParenExpr:
			fun = f.X
		default:
			return nil
		}
	}
}

// collectFuncDecls walks the file's top-level declarations, registering
// every function/method object and indexing method declarations.
func (e *exporter) collectFuncDecls(file *ast.File) (funcs []*funcDeclInfo, funcByObj map[*types.Func]*funcDeclInfo, methodIndex map[methodKey]*funcDeclInfo, err error) {
	funcByObj = make(map[*types.Func]*funcDeclInfo)
	methodIndex = make(map[methodKey]*funcDeclInfo)
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		obj, ok := e.info.Defs[fd.Name].(*types.Func)
		if !ok {
			return nil, nil, nil, fmt.Errorf("gofrontend: %s has no *types.Func object", fd.Name.Name)
		}
		funcID, err := e.defFunc(obj)
		if err != nil {
			return nil, nil, nil, err
		}
		sig, ok := obj.Type().(*types.Signature)
		if !ok {
			return nil, nil, nil, fmt.Errorf("gofrontend: %s has non-signature type %s", fd.Name.Name, obj.Type())
		}
		fi := &funcDeclInfo{fd: fd, obj: obj, funcID: funcID, sig: sig}
		if fd.Recv != nil {
			fi.recv = sig.Recv()
			if fi.recv == nil {
				return nil, nil, nil, fmt.Errorf("gofrontend: method %s has no receiver var", fd.Name.Name)
			}
			rn := derefNamed(fi.recv.Type())
			if rn == nil {
				return nil, nil, nil, scopeErrorf("method %s has non-named receiver type %s (WS D)", fd.Name.Name, fi.recv.Type())
			}
			methodIndex[methodKey{rn.Obj(), obj.Name()}] = fi
			fi.isTemplate = sig.TypeParams().Len() > 0 || rn.Origin().TypeParams().Len() > 0
		} else {
			fi.isTemplate = sig.TypeParams().Len() > 0
		}
		funcs = append(funcs, fi)
		funcByObj[obj] = fi
	}
	return funcs, funcByObj, methodIndex, nil
}

// instKeyForFunc builds the dedup key for a generic-function instantiation.
func instKeyForFunc(fi *funcDeclInfo, targs []types.Type, subst *typeSubst) string {
	sub := &typeSubst{parent: subst, bind: bindParams(fi.sig.TypeParams(), targs)}
	return "F:" + fi.obj.Name() + "|" + joinSubstKeys(targs, sub)
}

// instNameForFunc builds the display name, e.g. "Min[int]".
func instNameForFunc(fi *funcDeclInfo, targs []types.Type, subst *typeSubst) string {
	sub := &typeSubst{parent: subst, bind: bindParams(fi.sig.TypeParams(), targs)}
	pretty := make([]string, len(targs))
	for i, ta := range targs {
		pretty[i] = prettyType(ta, sub)
	}
	return fi.obj.Name() + "[" + strings.Join(pretty, ", ") + "]"
}

// prettyType renders a type for display names (Go-like, type params
// resolved).
func prettyType(t types.Type, s *typeSubst) string {
	if tp, ok := t.(*types.TypeParam); ok && s != nil {
		if c, scope := s.resolve(tp); c != nil {
			return prettyType(c, scope)
		}
	}
	return types.TypeString(t, nil)
}

// methodCall describes one resolved method call site.
type methodCall struct {
	decl      *funcDeclInfo
	subst     *typeSubst // binds receiver-origin + method type params
	key       string     // dedup key ("" when !needsInst)
	name      string     // display name ("" when !needsInst)
	needsInst bool
	// rawTargsForEmit are the actual argument types in bind order, for the
	// wire "typeargs" field (valid when needsInst).
	rawTargsForEmit []types.Type
}

// resolveMethodCall resolves a method call (selection kind MethodVal) to its
// declaration and, when the receiver type or the method is generic, to a
// monomorphized instantiation. subst is the enclosing scope's substitution
// (nil at top level). methodTargs are the method's own type arguments from
// info.Instances[sel.Sel] (nil for non-generic methods).
//
// Receiver-based dispatch only: the receiver expression's type must match the
// declared receiver type after substitution (checked by the caller); implicit
// &/* adjustments are a scope error (WS D).
func (e *exporter) resolveMethodCall(sel *ast.SelectorExpr, selection *types.Selection, methodTargs []types.Type, subst *typeSubst) (*methodCall, error) {
	mname := sel.Sel.Name
	rn := derefNamed(selection.Recv())
	if rn != nil {
		fi, ok := e.methodIndex[methodKey{rn.Obj(), mname}]
		if !ok {
			return nil, fmt.Errorf("gofrontend: method %s not found on %s", mname, rn.Obj().Name())
		}
		if !fi.isTemplate {
			return &methodCall{decl: fi, subst: subst}, nil
		}
		bind := make(map[*types.TypeParam]types.Type)
		var rawTargs []types.Type
		if err := bindRecvParams(bind, fi, rn); err != nil {
			return nil, err
		}
		rawTargs = append(rawTargs, typeList(rn.TypeArgs())...)
		mtps := fi.sig.TypeParams()
		if mtps.Len() > 0 {
			if len(methodTargs) != mtps.Len() {
				return nil, fmt.Errorf("gofrontend: generic method %s called without recorded type args", mname)
			}
			for i := 0; i < mtps.Len(); i++ {
				bind[mtps.At(i)] = methodTargs[i]
			}
			rawTargs = append(rawTargs, methodTargs...)
		}
		sub := &typeSubst{parent: subst, bind: bind}
		recvKey := substString(selection.Recv(), sub)
		key := "M:" + recvKey + "." + mname
		name := prettyType(selection.Recv(), sub) + "." + mname
		if len(methodTargs) > 0 {
			key += "|" + joinSubstKeys(methodTargs, sub)
			pretty := make([]string, len(methodTargs))
			for i, ta := range methodTargs {
				pretty[i] = prettyType(ta, sub)
			}
			name += "[" + strings.Join(pretty, ", ") + "]"
		}
		if !isConcreteKey(key) {
			return nil, fmt.Errorf("gofrontend: unbound type parameter in instantiation of method %s", mname)
		}
		return &methodCall{decl: fi, subst: sub, key: key, name: name, needsInst: true,
			rawTargsForEmit: rawTargs}, nil
	}
	// The receiver is not a named type: it must be a type parameter being
	// instantiated (bound-method call on a type param, e.g. v.String() with
	// v: T). Resolve the concrete type through the substitution and look
	// the method up in its method set (devirtualized per instantiation).
	tp, ok := selection.Recv().(*types.TypeParam)
	if !ok {
		return nil, scopeErrorf("method call %s on non-named receiver %s (WS H)", mname, selection.Recv())
	}
	if subst == nil {
		return nil, fmt.Errorf("gofrontend: method call on type parameter %s outside generic context", tp.Obj().Name())
	}
	c, scope := subst.resolve(tp)
	if c == nil {
		return nil, fmt.Errorf("gofrontend: unbound type parameter %s in method call", tp.Obj().Name())
	}
	cn := derefNamed(c)
	if cn == nil {
		return nil, scopeErrorf("method %s on type parameter with non-named argument type: interface dispatch on type parameters is not supported (WS H)", mname)
	}
	// M6: look the method up in the declaration index by (origin type
	// name, method name). We do NOT use types.NewMethodSet here: for an
	// instantiated receiver go/types returns *types.Func objects that are
	// not identical to the declared ones (verified by probe), so a
	// funcByObj lookup by object identity fails. The index is keyed by the
	// origin's TypeName, so partial-instantiation receivers (e.g.
	// Ledger[T, _]) resolve correctly.
	fi, ok := e.methodIndex[methodKey{cn.Obj(), mname}]
	if !ok {
		return nil, fmt.Errorf("gofrontend: type %s has no method %s", substString(c, scope), mname)
	}
	if !fi.isTemplate {
		return &methodCall{decl: fi, subst: subst}, nil
	}
	// Template reached via a type parameter: the receiver args come from
	// the concrete named type.
	bind := make(map[*types.TypeParam]types.Type)
	var rawTargs []types.Type
	if err := bindRecvParams(bind, fi, cn); err != nil {
		return nil, err
	}
	rawTargs = append(rawTargs, typeList(cn.TypeArgs())...)
	mtps := fi.sig.TypeParams()
	if mtps.Len() > 0 {
		if len(methodTargs) != mtps.Len() {
			return nil, fmt.Errorf("gofrontend: generic method %s called without recorded type args", mname)
		}
		for i := 0; i < mtps.Len(); i++ {
			bind[mtps.At(i)] = methodTargs[i]
		}
		rawTargs = append(rawTargs, methodTargs...)
	}
	sub := &typeSubst{parent: scope, bind: bind}
	recvKey := substString(c, scope)
	key := "M:" + recvKey + "." + mname
	name := prettyType(c, scope) + "." + mname
	if len(methodTargs) > 0 {
		key += "|" + joinSubstKeys(methodTargs, sub)
		pretty := make([]string, len(methodTargs))
		for i, ta := range methodTargs {
			pretty[i] = prettyType(ta, sub)
		}
		name += "[" + strings.Join(pretty, ", ") + "]"
	}
	if !isConcreteKey(key) {
		return nil, fmt.Errorf("gofrontend: unbound type parameter in instantiation of method %s", mname)
	}
	return &methodCall{decl: fi, subst: sub, key: key, name: name, needsInst: true,
		rawTargsForEmit: rawTargs}, nil
}

// enumerateInstantiations computes the full monomorphization fixpoint: every
// (generic function, type args) and (method, receiver args, method args)
// the program needs. Roots come from info.Instances / info.Selections in
// non-generic code; nested instantiations inside generic bodies are found by
// walking each template with its substitution.
//
// Termination is guaranteed by go/types: instantiation cycles are type
// errors, so the graph is finite for every accepted program. A defensive cap
// guards against enumerator bugs (internal error, never silent).
func (e *exporter) enumerateInstantiations(file *ast.File, funcs []*funcDeclInfo) ([]*instantiation, error) {
	// idents inside generic templates: instantiations recorded there are
	// nested (found in the fixpoint), not roots.
	inTemplate := make(map[*ast.Ident]bool)
	for _, fi := range funcs {
		if !fi.isTemplate {
			continue
		}
		ast.Inspect(fi.fd, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				inTemplate[id] = true
			}
			return true
		})
	}
	// Sel ident -> SelectorExpr (method instantiations via info.Instances).
	selOf := make(map[*ast.Ident]*ast.SelectorExpr)
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			selOf[sel.Sel] = sel
		}
		return true
	})

	seen := make(map[string]*instantiation)
	var order []*instantiation
	enqueue := func(inst *instantiation) {
		if _, ok := seen[inst.key]; ok {
			return
		}
		seen[inst.key] = inst
		order = append(order, inst)
	}
	newFuncInst := func(fi *funcDeclInfo, targs []types.Type, subst *typeSubst) (*instantiation, error) {
		key := instKeyForFunc(fi, targs, subst)
		if !isConcreteKey(key) {
			return nil, fmt.Errorf("gofrontend: unbound type parameter in instantiation of %s", fi.obj.Name())
		}
		sub2 := &typeSubst{parent: subst, bind: bindParams(fi.sig.TypeParams(), targs)}
		return &instantiation{
			decl: fi, subst: sub2, rawTargs: targs,
			key: key, name: instNameForFunc(fi, targs, subst),
		}, nil
	}

	// Roots: generic-function instantiations in non-generic code.
	for id, inst := range e.info.Instances {
		if inTemplate[id] {
			continue
		}
		if sel, ok := selOf[id]; ok {
			// Generic method via its Sel ident.
			selection, ok := e.info.Selections[sel]
			if !ok {
				return nil, fmt.Errorf("gofrontend: no selection for %s", sel.Sel.Name)
			}
			mc, err := e.resolveMethodCall(sel, selection, typeList(inst.TypeArgs), nil)
			if err != nil {
				return nil, err
			}
			if mc.needsInst {
				enqueue(&instantiation{decl: mc.decl, subst: mc.subst, rawTargs: mc.rawTargsForEmit, key: mc.key, name: mc.name})
			}
			continue
		}
		fn, ok := denotedFunc(e.info, id)
		if !ok {
			continue // type instantiation (only the type table needs it)
		}
		fi, ok := e.funcByObj[fn]
		if !ok {
			return nil, fmt.Errorf("gofrontend: call to unknown function %s", fn.Name())
		}
		if !fi.isTemplate {
			continue // non-generic: emitted from its declaration
		}
		ni, err := newFuncInst(fi, typeList(inst.TypeArgs), nil)
		if err != nil {
			return nil, err
		}
		enqueue(ni)
	}
	// Roots: method calls in non-generic code.
	for sel, selection := range e.info.Selections {
		if inTemplate[sel.Sel] {
			continue
		}
		if selection.Kind() != types.MethodVal {
			continue
		}
		// P3-M2b (ADR-0002): sync primitive method calls lower to syncop
		// nodes, never through resolveMethodCall/methodIndex (the
		// synthetic methods have no funcdecl to instantiate).
		if syncPrimitiveRecv(selection.Recv()) != nil {
			continue
		}
		var mtargs []types.Type
		if ii, ok := e.info.Instances[sel.Sel]; ok {
			mtargs = typeList(ii.TypeArgs)
		}
		mc, err := e.resolveMethodCall(sel, selection, mtargs, nil)
		if err != nil {
			return nil, err
		}
		if mc.needsInst {
			enqueue(&instantiation{decl: mc.decl, subst: mc.subst, rawTargs: mc.rawTargsForEmit, key: mc.key, name: mc.name})
		}
	}
	// Fixpoint: nested instantiations inside generic bodies.
	for i := 0; i < len(order); i++ {
		if len(order) > 100000 {
			return nil, fmt.Errorf("gofrontend: instantiation explosion (%d): internal error", len(order))
		}
		if err := e.findNested(order[i], enqueue, newFuncInst); err != nil {
			return nil, err
		}
	}
	// Deterministic emission order (TestDeterministic).
	sort.Slice(order, func(i, j int) bool { return order[i].name < order[j].name })
	return order, nil
}

// findNested walks one instantiation's template body for nested generic
// calls and method calls, enqueueing their (substituted) instantiations.
func (e *exporter) findNested(inst *instantiation, enqueue func(*instantiation), newFuncInst func(*funcDeclInfo, []types.Type, *typeSubst) (*instantiation, error)) error {
	subst := inst.subst
	var err error
	ast.Inspect(inst.decl.fd, func(n ast.Node) bool {
		if err != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.CallExpr:
			id := callTargetIdent(n.Fun)
			if id == nil {
				return true
			}
			ii, ok := e.info.Instances[id]
			if !ok {
				return true
			}
			fn, ok := denotedFunc(e.info, id)
			if !ok {
				return true // type instantiation
			}
			fi, ok := e.funcByObj[fn]
			if !ok {
				err = fmt.Errorf("gofrontend: call to unknown function %s", fn.Name())
				return false
			}
			if !fi.isTemplate {
				return true
			}
			ni, nerr := newFuncInst(fi, typeList(ii.TypeArgs), subst)
			if nerr != nil {
				err = nerr
				return false
			}
			enqueue(ni)
		case *ast.SelectorExpr:
			selection, ok := e.info.Selections[n]
			if !ok || selection.Kind() != types.MethodVal {
				return true
			}
			// P3-M2b (ADR-0002): sync primitives bypass the method index
			// (same as the roots loop above).
			if syncPrimitiveRecv(selection.Recv()) != nil {
				return true
			}
			var mtargs []types.Type
			if ii, ok := e.info.Instances[n.Sel]; ok {
				mtargs = typeList(ii.TypeArgs)
			}
			mc, merr := e.resolveMethodCall(n, selection, mtargs, subst)
			if merr != nil {
				err = merr
				return false
			}
			if mc.needsInst {
				enqueue(&instantiation{decl: mc.decl, subst: mc.subst, rawTargs: mc.rawTargsForEmit, key: mc.key, name: mc.name})
			}
		case *ast.Ident:
			// Generic function in value position (e.g. min[T] passed as an
			// argument inside a generic body): go/types records it in
			// info.Instances, but the CallExpr case above only sees call
			// targets, so the instantiation would never be enqueued and
			// export later fails with "not enumerated (internal error)".
			// Method idents (Sel of a SelectorExpr) are excluded: method
			// calls/values go through the SelectorExpr case.
			ii, ok := e.info.Instances[n]
			if !ok {
				return true
			}
			fn, ok := denotedFunc(e.info, n)
			if !ok {
				return true
			}
			fi, ok := e.funcByObj[fn]
			if !ok || !fi.isTemplate || fi.recv != nil {
				return true
			}
			ni, nerr := newFuncInst(fi, typeList(ii.TypeArgs), subst)
			if nerr != nil {
				err = nerr
				return false
			}
			enqueue(ni)
		}
		return true
	})
	return err
}

// exportFuncDecl emits a non-generic top-level function or method. Methods
// export their receiver as params[0] (the wire "signature" matches).
func (e *exporter) exportFuncDecl(fi *funcDeclInfo) (*Node, error) {
	fd := fi.fd
	if fd.Body == nil {
		return nil, scopeErrorf("func %s without body", fd.Name.Name)
	}
	params := []*Node{}
	if fi.recv != nil {
		rp, err := e.exportParamVar(fd, fi.recv)
		if err != nil {
			return nil, err
		}
		params = append(params, rp)
	}
	pp, err := e.exportSignatureVars(fd.Type, fi.sig.Params())
	if err != nil {
		return nil, err
	}
	params = append(params, pp...)
	results, err := e.exportSignatureVars(fd.Type, fi.sig.Results())
	if err != nil {
		return nil, err
	}
	if len(results) > 1 {
		return nil, scopeErrorf("func %s has %d results; at most one result is supported in frontend/v1 scope (WS D)", fd.Name.Name, len(results))
	}
	sig, err := e.freshSigID(fi.recv, fi.sig.Params(), fi.sig.Results(), fi.sig.Variadic())
	if err != nil {
		return nil, err
	}
	body, err := e.exportBlock(fd.Body)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{
		"name": fi.obj.Name(), "obj": fi.funcID, "sig": sig, "params": params, "body": body,
	}
	if len(results) > 0 {
		fields["results"] = results
	}
	return e.node("funcdecl", fd, fields), nil
}

// exportInstantiation emits one monomorphized instantiation: a funcdecl with
// "instof"/"typeargs", an "inst" object entry, and fresh object ids for the
// template's variables (the *types.Var objects are shared across
// instantiations).
//
// The "inst" object entry (ids) is assigned by prepareInstantiation, which
// runs for all instantiations before any body is emitted: call sites in
// already-emitted bodies resolve to these ids.
func (e *exporter) prepareInstantiation(inst *instantiation) error {
	// Scope substitution to this instantiation.
	savedSubst := e.subst
	e.subst = inst.subst
	savedSubstTypes := e.substTypeIDs
	e.substTypeIDs = make(map[types.Type]int)
	defer func() {
		e.subst = savedSubst
		e.substTypeIDs = savedSubstTypes
	}()

	var recv *types.Var
	if inst.decl.recv != nil {
		recv = inst.decl.recv
	}
	sigID, err := e.freshSigID(recv, inst.decl.sig.Params(), inst.decl.sig.Results(), inst.decl.sig.Variadic())
	if err != nil {
		return err
	}
	targIDs := make([]int, len(inst.rawTargs))
	for i, ta := range inst.rawTargs {
		id, err := e.typeID(ta)
		if err != nil {
			return err
		}
		targIDs[i] = id
	}
	instID := len(e.objects)
	e.objects = append(e.objects, ObjectEnt{
		Kind: "inst", Name: inst.name, Type: sigID,
		InstOf: inst.decl.funcID, TypeArgs: targIDs,
	})
	inst.instID = instID
	inst.sigID = sigID
	inst.targIDs = targIDs
	return nil
}

func (e *exporter) exportInstantiation(inst *instantiation) (*Node, error) {
	fd := inst.decl.fd
	if fd.Body == nil {
		return nil, scopeErrorf("func %s without body", fd.Name.Name)
	}
	// Scope object ids and substitution to this instantiation.
	savedObjs := e.objIDs
	e.objIDs = make(map[types.Object]int)
	savedSubst := e.subst
	e.subst = inst.subst
	savedSubstTypes := e.substTypeIDs
	e.substTypeIDs = make(map[types.Type]int)
	defer func() {
		e.objIDs = savedObjs
		e.subst = savedSubst
		e.substTypeIDs = savedSubstTypes
	}()

	params := []*Node{}
	if inst.decl.recv != nil {
		rp, err := e.exportParamVar(fd, inst.decl.recv)
		if err != nil {
			return nil, err
		}
		params = append(params, rp)
	}
	pp, err := e.exportSignatureVars(fd.Type, inst.decl.sig.Params())
	if err != nil {
		return nil, err
	}
	params = append(params, pp...)
	results, err := e.exportSignatureVars(fd.Type, inst.decl.sig.Results())
	if err != nil {
		return nil, err
	}
	if len(results) > 1 {
		return nil, scopeErrorf("func %s has %d results; at most one result is supported in frontend/v1 scope (WS D)", inst.name, len(results))
	}
	body, err := e.exportBlock(fd.Body)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{
		"name": inst.name, "obj": inst.instID, "instof": inst.decl.funcID,
		"typeargs": inst.targIDs, "sig": inst.sigID, "params": params, "body": body,
	}
	if len(results) > 0 {
		fields["results"] = results
	}
	return e.node("funcdecl", fd, fields), nil
}

// exportParamVar emits one kind:"param" node for a *types.Var.
func (e *exporter) exportParamVar(pos ast.Node, v *types.Var) (*Node, error) {
	id, err := e.defVar(v, true)
	if err != nil {
		return nil, err
	}
	tid, err := e.typeID(v.Type())
	if err != nil {
		return nil, err
	}
	return e.node("param", pos, map[string]any{
		"name": v.Name(), "obj": id, "type": tid,
	}), nil
}

// freshSigID emits a fresh (unmemoized) signature type-table entry. Used for
// funcdecl "sig" fields so the entry always matches the emitted params
// (methods: receiver prepended as params[0]; instantiations: substituted).
func (e *exporter) freshSigID(recv *types.Var, params, results *types.Tuple, variadic bool) (int, error) {
	pp, err := e.tupleList(params)
	if err != nil {
		return 0, err
	}
	if recv != nil {
		rid, err := e.typeID(recv.Type())
		if err != nil {
			return 0, err
		}
		pp = append([]any{map[string]any{"name": recv.Name(), "type": rid}}, pp...)
	}
	rr, err := e.tupleList(results)
	if err != nil {
		return 0, err
	}
	id := len(e.types)
	e.types = append(e.types, TypeEnt{Kind: "signature", Detail: map[string]any{
		"params": pp, "results": rr, "variadic": variadic,
	}})
	return id, nil
}

// ---------------------------------------------------------------------------
// Statements.
// ---------------------------------------------------------------------------

func (e *exporter) exportBlock(b *ast.BlockStmt) (*Node, error) {
	stmts := make([]*Node, 0, len(b.List))
	for _, s := range b.List {
		n, err := e.exportStmt(s)
		if err != nil {
			return nil, err
		}
		if n == nil {
			continue // *ast.EmptyStmt: normalized away (documented)
		}
		stmts = append(stmts, n)
	}
	return e.node("block", b, map[string]any{"stmts": stmts}), nil
}

func (e *exporter) exportStmt(s ast.Stmt) (*Node, error) {
	switch s := s.(type) {
	case *ast.EmptyStmt:
		return nil, nil // normalized away (documented in docs/frontend-v1.md)
	case *ast.BlockStmt:
		return e.exportBlock(s)
	case *ast.DeclStmt:
		gd, ok := s.Decl.(*ast.GenDecl)
		if !ok {
			return nil, scopeErrorf("only var/type declarations are supported in frontend/v1 scope (WS G/H)")
		}
		switch gd.Tok {
		case token.VAR:
			return e.exportVarGenDecl(gd)
		case token.TYPE:
			// Local type declarations are runtime no-ops (the type
			// table carries the types); normalized away like EmptyStmt
			// (documented in docs/frontend-v1.md). Each spec's declared
			// (possibly named) type is registered eagerly so later
			// references resolve to stable ids.
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					return nil, scopeErrorf("non-type type spec %T is not supported in frontend/v1 scope", spec)
				}
				tn, ok := e.info.Defs[ts.Name].(*types.TypeName)
				if !ok {
					return nil, fmt.Errorf("gofrontend: type name %q has no *types.TypeName object", ts.Name.Name)
				}
				if _, err := e.typeID(tn.Type()); err != nil {
					return nil, err
				}
			}
			return nil, nil
		default:
			return nil, scopeErrorf("only var/type declarations are supported in frontend/v1 scope (WS G/H)")
		}
	case *ast.AssignStmt:
		if s.Tok == token.DEFINE {
			return e.exportDefine(s)
		}
		return e.exportAssign(s)
	case *ast.ExprStmt:
		x, err := e.exportExpr(s.X)
		if err != nil {
			return nil, err
		}
		return e.node("exprstmt", s, map[string]any{"x": x}), nil
	case *ast.ReturnStmt:
		results := make([]*Node, 0, len(s.Results))
		for _, r := range s.Results {
			x, err := e.exportExpr(r)
			if err != nil {
				return nil, err
			}
			results = append(results, x)
		}
		return e.node("return", s, map[string]any{"results": results}), nil
	case *ast.IncDecStmt:
		x, err := e.exportAssignable(s.X)
		if err != nil {
			return nil, err
		}
		return e.node("incdec", s, map[string]any{"tok": s.Tok.String(), "x": x}), nil
	case *ast.IfStmt:
		return e.exportIf(s)
	case *ast.ForStmt:
		return e.exportFor(s)
	case *ast.RangeStmt:
		return e.exportRange(s)
	case *ast.SwitchStmt:
		return e.exportSwitch(s)
	case *ast.TypeSwitchStmt:
		return e.exportTypeSwitch(s)
	case *ast.BranchStmt:
		return e.exportBranch(s)
	case *ast.LabeledStmt:
		return e.exportLabeled(s)
	case *ast.DeferStmt:
		return e.exportDeferStmt(s)
	case *ast.GoStmt:
		return e.exportGoStmt(s)
	default:
		return nil, scopeErrorf("%T is not supported in frontend/v1 scope (%s)", s, stmtOwner(s))
	}
}

// exportDeferStmt exports `defer f(args)` (M5, WS F). The deferred call is
// exported as a call node; the call/args evaluation timing (evaluate now,
// invoke at return/unwind) is the factory's job.
func (e *exporter) exportDeferStmt(s *ast.DeferStmt) (*Node, error) {
	// P3-M2b (ADR-0002): `defer m.Unlock()` — the frozen buildDefer only
	// understands funclit/ident/builtin deferred calls, so a deferred sync
	// method call desugars to
	//   defer func(__sync_recv, __sync_arg0, ...) {
	//       __sync_recv.Op(__sync_arg0, ...)
	//   }(recv, args...)
	// The receiver and arguments are evaluated at defer time (Go
	// semantics); the call runs at return/unwind. Param passing (not
	// capture) so a receiver reassigned before return still unlocks the
	// deferred-time object.
	if sel, ok := unwrapSelector(s.Call.Fun); ok {
		if selection, ok := e.info.Selections[sel]; ok && selection.Kind() == types.MethodVal {
			if nt := syncPrimitiveRecv(selection.Recv()); nt != nil {
				return e.exportSyncDefer(s, sel, selection, nt)
			}
		}
	}
	call, err := e.exportCall(s.Call)
	if err != nil {
		return nil, err
	}
	return e.node("defer", s, map[string]any{"call": call}), nil
}

// exportSyncDefer desugars `defer m.Op(args)` on a sync primitive (see
// exportDeferStmt). The returned node is a "defer" wrapping a "call" of a
// synthesized funclit — all additive v1 wire, no factory change.
// exportSyncDefer desugars `defer m.Op(args)` on a sync primitive (see
// exportDeferStmt). The returned node is a "defer" wrapping a "call" of a
// synthesized funclit — all additive v1 wire, no factory change.
// exportSyncDefer desugars `defer m.Op(args)` on a sync primitive (see
// exportDeferStmt). The returned node is a "defer" wrapping a "call" of a
// synthesized funclit — all additive v1 wire, no factory change.
//
// Receiver semantics (verified against gc 1.27.1): a method with pointer
// receiver called on a value-typed receiver R uses &R — the ADDRESS — so a
// reassigned R still unlocks the current object. The desugar therefore
// CAPTURES a value-typed ident receiver in the closure body (evaluated at
// return time, like &R). A pointer-typed receiver (or a non-ident receiver,
// i.e. a temporary that cannot be reassigned) is passed as a param,
// evaluated at defer time. Args are always params (evaluated at defer
// time, Go semantics).
func (e *exporter) exportSyncDefer(s *ast.DeferStmt, sel *ast.SelectorExpr, selection *types.Selection, nt *types.Named) (*Node, error) {
	c := s.Call
	typeName := nt.Obj().Name()
	op := sel.Sel.Name
	if !syncOpsInScope[typeName][op] {
		return nil, scopeErrorf("sync.%s.%s is not in M2 scope", typeName, op)
	}
	argNodes, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	// The method's *types.Func gives the declared parameter types.
	fn, ok := selection.Obj().(*types.Func)
	if !ok {
		return nil, fmt.Errorf("gofrontend: sync method %s.%s is not a *types.Func", typeName, op)
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("gofrontend: sync method %s.%s has non-signature type", typeName, op)
	}
	recvType := e.info.Types[sel.X].Type
	if recvType == nil {
		return nil, fmt.Errorf("gofrontend: no type info for sync defer receiver")
	}
	// Value-typed ident receiver: capture (address semantics).
	// Otherwise: evaluate at defer time via a param.
	_, recvIsPtr := recvType.(*types.Pointer)
	_, recvIsIdent := sel.X.(*ast.Ident)
	captureRecv := !recvIsPtr && recvIsIdent
	var recvNode *Node
	var recvParam *Node
	var recvIdent *Node
	outerArgs := []*Node{}
	if captureRecv {
		// The original receiver expression goes in the body and is
		// captured by the closure — no param, no outer arg.
		recvNode, err = e.exportExpr(sel.X)
		if err != nil {
			return nil, err
		}
	} else {
		recvNode, err = e.exportExpr(sel.X)
		if err != nil {
			return nil, err
		}
		outerArgs = append(outerArgs, recvNode)
		v := types.NewParam(token.NoPos, nil, "__sync_recv", recvType)
		id, err := e.defVar(v, true)
		if err != nil {
			return nil, err
		}
		tid, err := e.typeID(recvType)
		if err != nil {
			return nil, err
		}
		recvParam = e.node("param", c, map[string]any{
			"name": "__sync_recv", "obj": id, "type": tid,
		})
		recvIdent = e.node("ident", c, map[string]any{
			"name": "__sync_recv", "obj": id, "type": tid,
		})
	}
	// Args are always params (evaluated at defer time).
	params := []*Node{}
	idents := []*Node{}
	if recvParam != nil {
		params = append(params, recvParam)
		idents = append(idents, recvIdent)
	}
	if sig.Params().Len() != len(argNodes) {
		return nil, fmt.Errorf("gofrontend: sync.%s.%s arity mismatch (internal error)", typeName, op)
	}
	for i := range argNodes {
		v := types.NewParam(token.NoPos, nil, fmt.Sprintf("__sync_arg%d", i), sig.Params().At(i).Type())
		id, err := e.defVar(v, true)
		if err != nil {
			return nil, err
		}
		tid, err := e.typeID(sig.Params().At(i).Type())
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf("__sync_arg%d", i)
		params = append(params, e.node("param", c, map[string]any{
			"name": name, "obj": id, "type": tid,
		}))
		idents = append(idents, e.node("ident", c, map[string]any{
			"name": name, "obj": id, "type": tid,
		}))
		outerArgs = append(outerArgs, argNodes[i])
	}
	// Body receiver: the captured original, or the param ident.
	bodyRecv := recvNode
	if recvIdent != nil {
		bodyRecv = recvIdent
	}
	// Body args: param idents (skip the receiver's if present).
	bodyArgs := idents
	if recvParam != nil {
		bodyArgs = idents[1:]
	}
	inner := e.node("syncop", c, map[string]any{
		"op": op, "synctyp": typeName,
		"recv": bodyRecv, "args": bodyArgs, "type": nil,
	})
	body := e.node("block", c, map[string]any{
		"stmts": []*Node{e.node("exprstmt", c, map[string]any{"x": inner})},
	})
	funclit := e.node("funclit", c, map[string]any{
		"params": params, "results": []*Node{}, "body": body,
	})
	call := e.node("call", c, map[string]any{
		"fun": funclit, "args": outerArgs, "type": nil,
	})
	return e.node("defer", s, map[string]any{"call": call}), nil
}

// exportGoStmt exports `go f(args)` (P2-M1, WS A). The call is exported as
// a call node; the Phase-2 factory builds a GoStatementNode that evaluates
// the function value in the spawning goroutine and hands it to the
// scheduler. Wire stays v1: "gostmt" is an additive node kind (freeze
// §2.3 — old consumers reject it as unknown, which is the correct
// forward-compatibility behavior for a kind they predate).
func (e *exporter) exportGoStmt(s *ast.GoStmt) (*Node, error) {
	// P3-M2b (ADR-0002): `go m.Op(args)` — GoStatementNode only builds
	// zero-arg spawns (P2-M1), so a direct sync method go-statement
	// desugars to go func() { <syncop> }() with the receiver/args
	// captured. For race-free programs this is indistinguishable from
	// evaluating them at go-time (the go statement happens-before the new
	// goroutine; any concurrent reassignment would be a data race).
	if sel, ok := unwrapSelector(s.Call.Fun); ok {
		if selection, ok := e.info.Selections[sel]; ok && selection.Kind() == types.MethodVal {
			if nt := syncPrimitiveRecv(selection.Recv()); nt != nil {
				return e.exportSyncGoStmt(s, sel, nt)
			}
		}
	}
	call, err := e.exportCall(s.Call)
	if err != nil {
		return nil, err
	}
	return e.node("gostmt", s, map[string]any{"call": call}), nil
}

// exportSyncGoStmt desugars `go m.Op(args)` on a sync primitive (see
// exportGoStmt): a zero-arg funclit call whose body is the syncop — all
// additive v1 wire, no factory change.
func (e *exporter) exportSyncGoStmt(s *ast.GoStmt, sel *ast.SelectorExpr, nt *types.Named) (*Node, error) {
	c := s.Call
	typeName := nt.Obj().Name()
	op := sel.Sel.Name
	if !syncOpsInScope[typeName][op] {
		return nil, scopeErrorf("sync.%s.%s is not in M2 scope", typeName, op)
	}
	inner, err := e.exportSyncOp(c, sel, nt)
	if err != nil {
		return nil, err
	}
	body := e.node("block", c, map[string]any{
		"stmts": []*Node{e.node("exprstmt", c, map[string]any{"x": inner})},
	})
	funclit := e.node("funclit", c, map[string]any{
		"params": []*Node{}, "results": []*Node{}, "body": body,
	})
	call := e.node("call", c, map[string]any{
		"fun": funclit, "args": []*Node{}, "type": nil,
	})
	return e.node("gostmt", s, map[string]any{"call": call}), nil
}

// stmtOwner attributes an unsupported statement to its workstream.
func stmtOwner(s ast.Stmt) string {
	switch s.(type) {
	case *ast.SelectStmt:
		return "WS E (Phase 2: channels)"
	case *ast.GoStmt:
		return "Phase 2 (goroutines; M5 task brief)"
	case *ast.SendStmt:
		return "WS E/F"
	default:
		return "a later workstream"
	}
}

// exportVarGenDecl handles `var` declarations (GenDecl form).
func (e *exporter) exportVarGenDecl(gd *ast.GenDecl) (*Node, error) {
	names := []string{}
	objs := []int{}
	var typ any
	values := make([]*Node, 0)
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			return nil, scopeErrorf("non-value var spec %T is not supported in frontend/v1 scope", spec)
		}
		var typeTid int
		var typeIsSync *types.Named
		hasType := false
		if vs.Type != nil {
			tid, err := e.exprType(vs.Type)
			if err != nil {
				return nil, err
			}
			typ = tid
			typeTid, hasType = tid, true
			if tv, ok := e.info.Types[vs.Type]; ok && tv.Type != nil {
				typeIsSync = syncPrimitiveNamed(tv.Type)
			}
		}
		for _, n := range vs.Names {
			v, ok := e.info.Defs[n].(*types.Var)
			if !ok {
				return nil, fmt.Errorf("gofrontend: var name %q has no *types.Var object", n.Name)
			}
			id, err := e.defVar(v, false)
			if err != nil {
				return nil, err
			}
			names = append(names, n.Name)
			objs = append(objs, id)
		}
		for _, v := range vs.Values {
			x, err := e.exportExpr(v)
			if err != nil {
				return nil, err
			}
			values = append(values, x)
		}
		// P3-M2b (ADR-0002 D3): `var m sync.Mutex` (no init) normalizes to
		// an implicit syncnew per name — gc's ready-to-use zero value. The
		// factory's struct zero would be an unusable empty struct, and the
		// factory is frozen, so the exporter supplies the init. One
		// syncnew per name: each variable is a distinct mutex.
		if hasType && typeIsSync != nil && len(vs.Values) == 0 {
			for range vs.Names {
				values = append(values, e.node("syncnew", vs, map[string]any{
					"synctyp": typeIsSync.Obj().Name(), "type": typeTid,
				}))
			}
		}
	}
	return e.node("vardecl", gd, map[string]any{
		"names": names, "objs": objs, "define": false, "type": typ, "values": values,
	}), nil
}

// exportDefine handles `:=` (short variable declarations).
func (e *exporter) exportDefine(s *ast.AssignStmt) (*Node, error) {
	names := make([]string, 0, len(s.Lhs))
	objs := make([]int, 0, len(s.Lhs))
	for _, lhs := range s.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			return nil, scopeErrorf(":= with non-identifier lhs %T is not supported in frontend/v1 scope (WS D)", lhs)
		}
		// A redeclared variable (same block, same type, at least one new
		// variable on the left) is recorded in Uses, not Defs, by go/types;
		// ObjectOf covers both cases and yields the same *types.Var.
		v, ok := e.info.ObjectOf(id).(*types.Var)
		if !ok {
			return nil, fmt.Errorf("gofrontend: := name %q does not denote a variable", id.Name)
		}
		oid, err := e.defVar(v, false)
		if err != nil {
			return nil, err
		}
		names = append(names, id.Name)
		objs = append(objs, oid)
	}
	values := make([]*Node, 0, len(s.Rhs))
	commaOk := len(s.Lhs) == 2 && len(s.Rhs) == 1 && isCommaOkRhs(s.Rhs[0])
	for _, rhs := range s.Rhs {
		var x *Node
		var err error
		if commaOk {
			x, err = e.exportCommaOk(unparen(rhs))
		} else {
			x, err = e.exportExpr(rhs)
		}
		if err != nil {
			return nil, err
		}
		values = append(values, x)
	}
	return e.node("vardecl", s, map[string]any{
		"names": names, "objs": objs, "define": true, "type": nil, "values": values,
	}), nil
}

// exportAssign handles `=` and op-assignments. The lhs may be a plain
// identifier, an index expression, or a selector (M4, WS E); op-assignments
// require a single lhs (go/types: "requires single-valued expressions").
func (e *exporter) exportAssign(s *ast.AssignStmt) (*Node, error) {
	commaOk := len(s.Lhs) == 2 && len(s.Rhs) == 1 && isCommaOkRhs(s.Rhs[0])
	lhs := make([]*Node, 0, len(s.Lhs))
	for _, l := range s.Lhs {
		x, err := e.exportAssignable(l)
		if err != nil {
			return nil, err
		}
		lhs = append(lhs, x)
	}
	rhs := make([]*Node, 0, len(s.Rhs))
	for _, r := range s.Rhs {
		var x *Node
		var err error
		if commaOk {
			x, err = e.exportCommaOk(unparen(r))
		} else {
			x, err = e.exportExpr(r)
		}
		if err != nil {
			return nil, err
		}
		rhs = append(rhs, x)
	}
	return e.node("assign", s, map[string]any{
		"tok": s.Tok.String(), "lhs": lhs, "rhs": rhs,
	}), nil
}

// exportAssignable exports an assignment lhs: identifier, index, or
// selector expression. The blank identifier becomes a "blank" node (the
// value is evaluated and discarded).
func (e *exporter) exportAssignable(l ast.Expr) (*Node, error) {
	if id, ok := l.(*ast.Ident); ok && id.Name == "_" {
		return e.node("blank", id, map[string]any{}), nil
	}
	switch l := l.(type) {
	case *ast.Ident:
		return e.exportIdent(l)
	case *ast.IndexExpr:
		return e.exportIndex(l, false)
	case *ast.SelectorExpr:
		return e.exportSelector(l)
	default:
		return nil, scopeErrorf("assignment to %T is not supported in frontend/v1 scope (WS D)", l)
	}
}

// exportCommaOk exports a two-valued map-index or type-assertion rhs
// (already unparenthesized by the caller).
func (e *exporter) exportCommaOk(r ast.Expr) (*Node, error) {
	switch r := r.(type) {
	case *ast.IndexExpr:
		return e.exportIndex(r, true)
	case *ast.TypeAssertExpr:
		return e.exportTypeAssert(r, true)
	default:
		return nil, fmt.Errorf("gofrontend: not a comma-ok expression: %T", r)
	}
}

// ---------------------------------------------------------------------------
// Control-flow statements (M4, WS E).
// ---------------------------------------------------------------------------

// exportOptStmt exports a statement that may be nil (if/for/switch init,
// for post), returning nil for the absent case.
func (e *exporter) exportOptStmt(s ast.Stmt) (*Node, error) {
	if s == nil {
		return nil, nil
	}
	return e.exportStmt(s)
}

// exportIf handles if/else (else-if chains recurse as nested "if" nodes).
func (e *exporter) exportIf(s *ast.IfStmt) (*Node, error) {
	init, err := e.exportOptStmt(s.Init)
	if err != nil {
		return nil, err
	}
	cond, err := e.exportExpr(s.Cond)
	if err != nil {
		return nil, err
	}
	then, err := e.exportBlock(s.Body)
	if err != nil {
		return nil, err
	}
	var else_ *Node
	if s.Else != nil {
		switch t := s.Else.(type) {
		case *ast.BlockStmt:
			else_, err = e.exportBlock(t)
		case *ast.IfStmt:
			else_, err = e.exportIf(t)
		default:
			return nil, fmt.Errorf("gofrontend: else branch is %T", s.Else)
		}
		if err != nil {
			return nil, err
		}
	}
	return e.node("if", s, map[string]any{
		"init": init, "cond": cond, "then": then, "else": else_,
	}), nil
}

// exportFor handles the three for forms: 3-clause, condition-only, and
// infinite (absent cond/post are null on the wire).
func (e *exporter) exportFor(s *ast.ForStmt) (*Node, error) {
	init, err := e.exportOptStmt(s.Init)
	if err != nil {
		return nil, err
	}
	var cond *Node
	if s.Cond != nil {
		cond, err = e.exportExpr(s.Cond)
		if err != nil {
			return nil, err
		}
	}
	post, err := e.exportOptStmt(s.Post)
	if err != nil {
		return nil, err
	}
	body, err := e.exportBlock(s.Body)
	if err != nil {
		return nil, err
	}
	return e.node("for", s, map[string]any{
		"init": init, "cond": cond, "post": post, "body": body,
	}), nil
}

// rangeVarId maps a range-clause Key/Value expression to an object-table id:
// nil for an absent position or the blank identifier.
func (e *exporter) rangeVarId(x ast.Expr) (any, error) {
	if x == nil {
		return nil, nil
	}
	id, ok := x.(*ast.Ident)
	if !ok {
		return nil, scopeErrorf("range variable %T is not supported in frontend/v1 scope", x)
	}
	if id.Name == "_" {
		return nil, nil
	}
	v, ok := e.info.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, fmt.Errorf("gofrontend: range variable %q does not denote a variable", id.Name)
	}
	return e.defVar(v, false)
}

// exportRange handles `for k, v := range x` and `for k, v = range x`
// (define=false). Key/value are object ids or null (absent or blank).
func (e *exporter) exportRange(s *ast.RangeStmt) (*Node, error) {
	var define bool
	switch s.Tok {
	case token.DEFINE:
		define = true
	case token.ASSIGN:
		define = false
	case token.ILLEGAL:
		// `for range x`: no key/value variables, no definition.
		define = false
	default:
		return nil, fmt.Errorf("gofrontend: range clause token %v", s.Tok)
	}
	key, err := e.rangeVarId(s.Key)
	if err != nil {
		return nil, err
	}
	value, err := e.rangeVarId(s.Value)
	if err != nil {
		return nil, err
	}
	x, err := e.exportExpr(s.X)
	if err != nil {
		return nil, err
	}
	body, err := e.exportBlock(s.Body)
	if err != nil {
		return nil, err
	}
	return e.node("range", s, map[string]any{
		"key": key, "value": value, "define": define, "x": x, "body": body,
	}), nil
}

// exportStmtList exports a statement list as a "block" node positioned at
// `at` (used for case-clause bodies, which are not *ast.BlockStmt).
func (e *exporter) exportStmtList(list []ast.Stmt, at ast.Node) (*Node, error) {
	stmts := make([]*Node, 0, len(list))
	for _, s := range list {
		n, err := e.exportStmt(s)
		if err != nil {
			return nil, err
		}
		if n == nil {
			continue // *ast.EmptyStmt: normalized away (documented)
		}
		stmts = append(stmts, n)
	}
	return e.node("block", at, map[string]any{"stmts": stmts}), nil
}

// exportSwitch handles expression switches (a nil tag is `switch { }`).
func (e *exporter) exportSwitch(s *ast.SwitchStmt) (*Node, error) {
	init, err := e.exportOptStmt(s.Init)
	if err != nil {
		return nil, err
	}
	var tag *Node
	if s.Tag != nil {
		tag, err = e.exportExpr(s.Tag)
		if err != nil {
			return nil, err
		}
	}
	clauses := make([]*Node, 0, len(s.Body.List))
	for _, stmt := range s.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			return nil, fmt.Errorf("gofrontend: switch body holds %T", stmt)
		}
		cases := make([]*Node, 0, len(cc.List))
		for _, c := range cc.List {
			x, err := e.exportExpr(c)
			if err != nil {
				return nil, err
			}
			cases = append(cases, x)
		}
		body, err := e.exportStmtList(cc.Body, cc)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, e.node("caseclause", cc, map[string]any{
			"cases": cases, "default": len(cc.List) == 0, "body": body,
		}))
	}
	return e.node("switch", s, map[string]any{
		"init": init, "tag": tag, "clauses": clauses,
	}), nil
}

// exportTypeSwitch handles `switch [v :=] x.(type)`. The guard variable
// (when declared) is one implicit *types.Var per clause
// (info.Implicits[clause]), already typed with the clause's type by
// go/types — single-type clauses get the clause type, multi-type/nil/
// default clauses the guard (interface) type. Each is registered in the
// object table before its clause body is exported, so references resolve
// through the normal exportIdent path.
func (e *exporter) exportTypeSwitch(s *ast.TypeSwitchStmt) (*Node, error) {
	init, err := e.exportOptStmt(s.Init)
	if err != nil {
		return nil, err
	}
	var tag ast.Expr
	hasGuard := false
	switch a := s.Assign.(type) {
	case *ast.ExprStmt:
		ta, ok := a.X.(*ast.TypeAssertExpr)
		if !ok {
			return nil, fmt.Errorf("gofrontend: type switch guard is %T, want x.(type)", a.X)
		}
		tag = ta.X
	case *ast.AssignStmt:
		if a.Tok != token.DEFINE || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
			return nil, fmt.Errorf("gofrontend: type switch guard must be `v := x.(type)`")
		}
		ta, ok := a.Rhs[0].(*ast.TypeAssertExpr)
		if !ok {
			return nil, fmt.Errorf("gofrontend: type switch guard rhs is %T, want x.(type)", a.Rhs[0])
		}
		if _, ok := a.Lhs[0].(*ast.Ident); !ok {
			return nil, fmt.Errorf("gofrontend: type switch guard lhs is %T, want an identifier", a.Lhs[0])
		}
		hasGuard = true
		tag = ta.X
	default:
		return nil, fmt.Errorf("gofrontend: type switch guard is %T", s.Assign)
	}
	tagNode, err := e.exportExpr(tag)
	if err != nil {
		return nil, err
	}
	clauses := make([]*Node, 0, len(s.Body.List))
	for _, stmt := range s.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			return nil, fmt.Errorf("gofrontend: type switch body holds %T", stmt)
		}
		isDefault := len(cc.List) == 0
		var typeIDs []int
		nilCase := false
		for _, te := range cc.List {
			if id, ok := te.(*ast.Ident); ok && id.Name == "nil" {
				nilCase = true
				continue
			}
			tid, err := e.exprType(te)
			if err != nil {
				return nil, err
			}
			typeIDs = append(typeIDs, tid)
		}
		if nilCase && len(typeIDs) > 0 {
			return nil, fmt.Errorf("gofrontend: `case nil` combined with types")
		}
		var bindObj any
		var bindType any
		if hasGuard {
			// go/types materializes one implicit Var per clause, typed
			// with the clause's type. Register it before exporting the
			// body so guard references resolve to this clause's id.
			iv, ok := e.info.Implicits[cc].(*types.Var)
			if !ok {
				return nil, fmt.Errorf("gofrontend: type switch clause has no implicit guard variable")
			}
			oid, err := e.defVar(iv, false)
			if err != nil {
				return nil, err
			}
			btid, err := e.typeID(iv.Type())
			if err != nil {
				return nil, err
			}
			bindObj, bindType = oid, btid
		}
		body, err := e.exportStmtList(cc.Body, cc)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, e.node("typecase", cc, map[string]any{
			"types": typeIDs, "nilcase": nilCase, "default": isDefault,
			"bindobj": bindObj, "bindtype": bindType, "body": body,
		}))
	}
	return e.node("typeswitch", s, map[string]any{
		"init": init, "tag": tagNode, "clauses": clauses,
	}), nil
}

// exportBranch handles break/continue/goto/fallthrough (label or null).
func (e *exporter) exportBranch(s *ast.BranchStmt) (*Node, error) {
	var label any
	if s.Label != nil {
		label = s.Label.Name
	}
	return e.node("branch", s, map[string]any{"op": s.Tok.String(), "label": label}), nil
}

// exportLabeled handles `L: stmt`.
func (e *exporter) exportLabeled(s *ast.LabeledStmt) (*Node, error) {
	inner, err := e.exportStmt(s.Stmt)
	if err != nil {
		return nil, err
	}
	return e.node("labeled", s, map[string]any{"label": s.Label.Name, "stmt": inner}), nil
}

// ---------------------------------------------------------------------------
// Expressions.
// ---------------------------------------------------------------------------

// exprType returns the type-table id for the type of expression x.
func (e *exporter) exprType(x ast.Expr) (int, error) {
	if tv, ok := e.info.Types[x]; ok && tv.Type != nil {
		return e.typeID(tv.Type)
	}
	// Definition-site idents (params, := lhs) may lack a Types entry.
	if id, ok := x.(*ast.Ident); ok {
		if obj := e.info.ObjectOf(id); obj != nil {
			return e.typeID(obj.Type())
		}
	}
	return 0, fmt.Errorf("gofrontend: no type info for expression %T", x)
}

// foldedConst emits a go/types constant-folded binary or paren expression
// as a basiclit.
//
// Rationale (M3, WS D): untyped constants can exceed 64 bits (1<<70),
// constant comparisons are decided on infinite-precision values
// ((1<<64) < 1 is false), and constant float arithmetic is exact-rational
// (0.1+0.2 folds to the rational 0.3). Evaluating such expressions through
// the operator nodes would silently diverge from gc, so the exporter
// propagates go/types' already-folded value; NodeFactory converts it to the
// use-site type with correctly-rounded parsing. Unary expressions are NOT
// folded: -, ^, !, + are exact on the low 64 bits / booleans, so the factory
// evaluates them without divergence. "value" is empty (no single source
// literal); "const" carries go/constant ExactString. Additive producer
// behavior — the wire format is unchanged (docs/frontend-v1.md).
func (e *exporter) foldedConst(x ast.Expr, v constant.Value) (*Node, error) {
	t, err := e.exprType(x)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{"value": "", "const": v.ExactString(), "type": t}
	if v.Kind() == constant.String {
		fields["sbytes"] = hex.EncodeToString([]byte(constant.StringVal(v)))
	}
	if v.Kind() == constant.Int || v.Kind() == constant.Float {
		f32, _ := constant.Float32Val(v)
		f64, _ := constant.Float64Val(v)
		fields["f32bits"] = int32(math.Float32bits(f32))
		fields["f64bits"] = int64(math.Float64bits(f64))
	}
	// M7: complex constants carry the real/imag parts as bit patterns.
	if v.Kind() == constant.Complex {
		realPart := constant.Real(v)
		imagPart := constant.Imag(v)
		r32, _ := constant.Float32Val(realPart)
		r64, _ := constant.Float64Val(realPart)
		i32, _ := constant.Float32Val(imagPart)
		i64, _ := constant.Float64Val(imagPart)
		fields["creal32"] = int32(math.Float32bits(r32))
		fields["creal64"] = int64(math.Float64bits(r64))
		fields["cimag32"] = int32(math.Float32bits(i32))
		fields["cimag64"] = int64(math.Float64bits(i64))
	}
	return e.node("basiclit", x, fields), nil
}

func (e *exporter) exportExpr(x ast.Expr) (*Node, error) {
	switch x := x.(type) {
	case *ast.Ident:
		return e.exportIdent(x)
	case *ast.BasicLit:
		t, err := e.exprType(x)
		if err != nil {
			return nil, err
		}
		fields := map[string]any{"value": x.Value, "type": t}
		if tv, ok := e.info.Types[x]; ok && tv.Value != nil {
			fields["const"] = tv.Value.ExactString()
			if tv.Value.Kind() == constant.String {
				// Byte-exact payload for string constants: ExactString is a
				// quoted Go literal (lossy to unquote by hand, and JSON
				// itself cannot carry invalid-UTF-8 bytes), so the exporter
				// hex-encodes the exact bytes (M3, WS C/D; additive field).
				fields["sbytes"] = hex.EncodeToString([]byte(constant.StringVal(tv.Value)))
			}
			if tv.Value.Kind() == constant.Int || tv.Value.Kind() == constant.Float {
				// Correctly-rounded float values of the constant, computed
				// with go/constant itself (the same rounding go/types uses
				// for constant→float conversions). Bits are reinterpreted
				// as signed ints for JSON (the factory reinterprets back).
				// This sidesteps ExactString's "a/b" fraction form, which
				// no correctly-rounded decimal parse could recover.
				f32, _ := constant.Float32Val(tv.Value)
				f64, _ := constant.Float64Val(tv.Value)
				fields["f32bits"] = int32(math.Float32bits(f32))
				fields["f64bits"] = int64(math.Float64bits(f64))
			}
		}
		return e.node("basiclit", x, fields), nil
	case *ast.BinaryExpr:
		if tv, ok := e.info.Types[x]; ok && tv.Value != nil {
			return e.foldedConst(x, tv.Value)
		}
		t, err := e.exprType(x)
		if err != nil {
			return nil, err
		}
		l, err := e.exportExpr(x.X)
		if err != nil {
			return nil, err
		}
		r, err := e.exportExpr(x.Y)
		if err != nil {
			return nil, err
		}
		return e.node("binary", x, map[string]any{"op": x.Op.String(), "x": l, "y": r, "type": t}), nil
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			// &T{...}: normalize to a pointer-typed composite literal
			// (M4, WS E). & on anything else stays a WS D scope error.
			cl, ok := x.X.(*ast.CompositeLit)
			if !ok {
				return nil, scopeErrorf("& on %T is not supported in frontend/v1 scope (WS D)", x.X)
			}
			t, err := e.exprType(x)
			if err != nil {
				return nil, err
			}
			return e.exportCompositeLitAs(cl, t)
		}
		// M8 closeout: constant-fold like BinaryExpr/ParenExpr. go/types
		// evaluates -0.0 to +0.0 (and all constant arithmetic exactly);
		// emitting a runtime negation would produce IEEE -0.0 instead.
		if tv, ok := e.info.Types[x]; ok && tv.Value != nil {
			return e.foldedConst(x, tv.Value)
		}
		t, err := e.exprType(x)
		if err != nil {
			return nil, err
		}
		o, err := e.exportExpr(x.X)
		if err != nil {
			return nil, err
		}
		return e.node("unary", x, map[string]any{"op": x.Op.String(), "x": o, "type": t}), nil
	case *ast.CompositeLit:
		return e.exportCompositeLit(x)
	case *ast.IndexExpr:
		// M6 (WS H): a generic function in value position (e.g. min[int]
		// passed as an argument) is an IndexExpr whose X denotes a generic
		// function recorded in info.Instances. It is a func value, not an
		// index operation: route it through the func-value path instead
		// of exportIndex (which would try to export the type argument as
		// an expression).
		if id, ok := x.X.(*ast.Ident); ok {
			if _, isInst := e.info.Instances[id]; isInst {
				if fn, ok := denotedFunc(e.info, id); ok {
					if fi, ok := e.funcByObj[fn]; ok && fi.isTemplate && fi.recv == nil {
						fields := map[string]any{"name": id.Name}
						if err := e.exportFuncValue(id, fn, fields); err != nil {
							return nil, err
						}
						return e.node("ident", x, fields), nil
					}
				}
			}
		}
		return e.exportIndex(x, false)
	case *ast.SliceExpr:
		return e.exportSlice(x)
	case *ast.SelectorExpr:
		return e.exportSelector(x)
	case *ast.TypeAssertExpr:
		return e.exportTypeAssert(x, false)
	case *ast.ParenExpr:
		if tv, ok := e.info.Types[x]; ok && tv.Value != nil {
			return e.foldedConst(x, tv.Value)
		}
		t, err := e.exprType(x)
		if err != nil {
			return nil, err
		}
		o, err := e.exportExpr(x.X)
		if err != nil {
			return nil, err
		}
		return e.node("paren", x, map[string]any{"x": o, "type": t}), nil
	case *ast.CallExpr:
		return e.exportCall(x)
	case *ast.FuncLit:
		return e.exportFuncLit(x)
	default:
		return nil, scopeErrorf("%T is not supported in frontend/v1 scope (%s)", x, exprOwner(x))
	}
}

// exprOwner attributes an unsupported expression to its workstream.
func exprOwner(x ast.Expr) string {
	switch x.(type) {
	case *ast.FuncType, *ast.StarExpr, *ast.IndexListExpr:
		return "WS D"
	case *ast.ChanType, *ast.MapType, *ast.ArrayType, *ast.StructType,
		*ast.InterfaceType:
		return "WS C/D"
	default:
		return "a later workstream"
	}
}

// exportIdent exports an identifier in expression position: a use of a
// declared variable, or a builtin function name.
func (e *exporter) exportIdent(id *ast.Ident) (*Node, error) {
	if _, ok := e.info.Defs[id]; ok {
		return nil, fmt.Errorf("gofrontend: definition-site identifier %q used as expression", id.Name)
	}
	obj, ok := e.info.Uses[id]
	if !ok {
		return nil, fmt.Errorf("gofrontend: unresolved identifier %q", id.Name)
	}
	fields := map[string]any{"name": id.Name}
	switch obj := obj.(type) {
	case *types.Var:
		oid, ok := e.objIDs[obj]
		if !ok {
			return nil, fmt.Errorf("gofrontend: variable %q used before declaration", id.Name)
		}
		t, err := e.typeID(obj.Type())
		if err != nil {
			return nil, err
		}
		fields["obj"] = oid
		fields["type"] = t
	case *types.Builtin:
		t, err := e.typeID(obj.Type())
		if err != nil {
			return nil, err
		}
		fields["builtin"] = obj.Name()
		fields["type"] = t
	case *types.Const:
		// Constants (universe true/false, package-level and local consts):
		// emitted as a folded basiclit so the factory's typed-constant
		// path handles them (M3, WS D). foldedConst supplies the binary
		// payloads (sbytes/f32bits/f64bits) the factory requires.
		return e.foldedConst(id, obj.Val())
	case *types.Nil:
		// M5 (WS F): the nil identifier in expression position. Typed as
		// the empty interface; the factory's NilNode evaluates to null,
		// which panic(nil) converts to *PanicNilError and which compares
		// equal to nil interfaces. (Full nil typing is WS D.)
		t, err := e.typeID(types.NewInterfaceType(nil, nil))
		if err != nil {
			return nil, err
		}
		return e.node("nil", id, map[string]any{"type": t}), nil
	case *types.Func:
		// M6 (WS H): reference to a named function or method (value
		// position, e.g. f := Min[int]). Emitted as an ident whose obj is
		// the function/instantiation object id; the factory builds a
		// func-value constant. Method values (bound receivers) are not
		// supported (WS H follow-up).
		if err := e.exportFuncValue(id, obj, fields); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("gofrontend: identifier %q denotes %T", id.Name, obj)
	}
	return e.node("ident", id, fields), nil
}

// exportFuncValue fills fields for a reference to a named function or
// method in value position (e.g. f := Min[int], or Min[int] passed as an
// argument). Generic functions resolve to their monomorphized
// instantiation via info.Instances.
func (e *exporter) exportFuncValue(id *ast.Ident, obj *types.Func, fields map[string]any) error {
	fi, ok := e.funcByObj[obj]
	if !ok {
		return fmt.Errorf("gofrontend: unknown function %s", obj.Name())
	}
	if fi.recv != nil {
		return scopeErrorf("method value %s (bound receiver) is not supported in frontend/v1 scope (WS H)", id.Name)
	}
	if fi.isTemplate {
		ii, ok := e.info.Instances[id]
		if !ok {
			return scopeErrorf("generic function value %s without instantiation is not supported (WS H)", id.Name)
		}
		targs := typeList(ii.TypeArgs)
		key := instKeyForFunc(fi, targs, e.subst)
		inst, ok := e.instByKey[key]
		if !ok {
			return fmt.Errorf("gofrontend: instantiation %s not enumerated (internal error)", key)
		}
		fields["obj"] = inst.instID
		fields["type"] = inst.sigID
	} else {
		t, err := e.typeID(obj.Type())
		if err != nil {
			return err
		}
		oid, err := e.defFunc(obj)
		if err != nil {
			return err
		}
		fields["obj"] = oid
		fields["type"] = t
	}
	return nil
}

// exportCall exports a call expression: either a Go conversion T(x) (the
// call target denotes a *types.TypeName), emitted as a "convert" node
// (M3, WS D — new node kind, additive per docs/frontend-v1.md §8), a call
// to runtime.Goexit (M5, WS F — the Phase-1 Goexit trigger), a call to a
// builtin function, a call of a function literal, or a call through a
// func-valued variable (M5, WS F — deferred calls and recursion). Other
// call shapes are scope errors (WS D).
func (e *exporter) exportCall(c *ast.CallExpr) (*Node, error) {
	// M5 (WS F): runtime.Goexit() — the Phase-1 Goexit trigger. Emitted as
	// a "call" node with the goexit marker; the factory builds GoexitNode.
	// M6 (WS H): method calls — possibly wrapped in an index expression
	// for explicit type arguments (s.M[T](...)).
	if sel, ok := unwrapSelector(c.Fun); ok {
		// M6 (WS H): fmt.X via the D4 import overlay.
		if pkgIdent, ok := sel.X.(*ast.Ident); ok && pkgIdent.Name == "fmt" {
			if _, ok := e.info.Uses[pkgIdent].(*types.PkgName); ok {
				return e.exportFmtCall(c, sel)
			}
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "runtime" && sel.Sel.Name == "Goexit" {
			if obj, ok := e.info.Uses[sel.Sel].(*types.Func); !ok || obj.Pkg() == nil || obj.Pkg().Name() != "runtime" {
				return nil, scopeErrorf("runtime.Goexit did not resolve to the overlay package")
			}
			if len(c.Args) != 0 {
				return nil, fmt.Errorf("gofrontend: runtime.Goexit takes no arguments")
			}
			return e.node("call", c, map[string]any{"goexit": true, "args": []*Node{}}), nil
		}
		// P2-M4 (GC interop, ADR-0002 D4): runtime.SetFinalizer/KeepAlive/GC.
		// Emitted as "call" nodes with the gcinterop marker naming the
		// intrinsic; the factory builds the corresponding CallNode which
		// delegates to graalgo.runtime.GcInterop. Marker is additive on the
		// v1 wire format (goexit-marker precedent) — no version bump.
		if pkgIdent, ok := sel.X.(*ast.Ident); ok && pkgIdent.Name == "runtime" {
			if obj, ok := e.info.Uses[sel.Sel].(*types.Func); ok && obj.Pkg() != nil && obj.Pkg().Name() == "runtime" {
				switch sel.Sel.Name {
				case "SetFinalizer", "KeepAlive", "GC":
					want := map[string]int{"SetFinalizer": 2, "KeepAlive": 1, "GC": 0}[sel.Sel.Name]
					if len(c.Args) != want {
						return nil, fmt.Errorf("gofrontend: runtime.%s takes %d arguments", sel.Sel.Name, want)
					}
					args, err := e.exportCallArgs(c)
					if err != nil {
						return nil, err
					}
					return e.node("call", c, map[string]any{
						"gcinterop": sel.Sel.Name, "args": args,
					}), nil
				}
			}
		}
		// M6 (WS H): method call — receiver-based dispatch resolved by
		// go/types to a (possibly instantiated) method.
		// P3-M2b (sync bindings, ADR-0002 D2): method calls on the
		// synthetic sync primitives lower to additive "syncop" wire nodes,
		// bypassing exportMethodCall's WS D no-implicit-& rule.
		if selection, ok := e.info.Selections[sel]; ok && selection.Kind() == types.MethodVal {
			if nt := syncPrimitiveRecv(selection.Recv()); nt != nil {
				return e.exportSyncOp(c, sel, nt)
			}
		}
		selection, ok := e.info.Selections[sel]
		if !ok {
			return nil, scopeErrorf("selector call %s.%s is not supported in frontend/v1 scope (WS D)",
				sel.X, sel.Sel.Name)
		}
		return e.exportMethodCall(c, sel, selection)
	}
	// M5 (WS F): immediately-called function literal, e.g. the canonical
	// deferred shape `defer func() { ... }()`.
	if lit, ok := c.Fun.(*ast.FuncLit); ok {
		fun, err := e.exportFuncLit(lit)
		if err != nil {
			return nil, err
		}
		if tv, ok := e.info.Types[lit]; ok && tv.Type != nil {
			if sig, ok := tv.Type.(*types.Signature); ok {
				if err := checkVariadicCall(c, sig); err != nil {
					return nil, err
				}
			}
		}
		args, err := e.exportCallArgs(c)
		if err != nil {
			return nil, err
		}
		typ, err := e.callType(c)
		if err != nil {
			return nil, err
		}
		return e.node("call", c, map[string]any{
			"fun": fun, "args": args, "type": typ,
		}), nil
	}
	funIdent := callTargetIdent(c.Fun)
	if funIdent == nil {
		// M7: conversion with a composite type target, e.g. []byte("hi").
		// The target is an *ast.ArrayType/*ast.MapType, not an identifier.
		if tv, ok := e.info.Types[c.Fun]; ok && tv.Type != nil {
			if len(c.Args) != 1 {
				return nil, fmt.Errorf("gofrontend: conversion has %d arguments (want 1)", len(c.Args))
			}
			x, err := e.exportExpr(c.Args[0])
			if err != nil {
				return nil, err
			}
			to, err := e.typeID(tv.Type)
			if err != nil {
				return nil, err
			}
			// M7: "from" records the source type id.
			var from any
			if atv, ok := e.info.Types[c.Args[0]]; ok && atv.Type != nil {
				fid, err := e.typeID(atv.Type)
				if err != nil {
					return nil, err
				}
				from = fid
			}
			fields := map[string]any{"x": x, "to": to, "type": to}
			if from != nil {
				fields["from"] = from
			}
			return e.node("convert", c, fields), nil
		}
		return nil, scopeErrorf("non-identifier call target %T is not supported in frontend/v1 scope (WS D)", c.Fun)
	}
	if tn, ok := e.info.Uses[funIdent].(*types.TypeName); ok {
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("gofrontend: conversion to %s has %d arguments (want 1)",
				tn.Name(), len(c.Args))
		}
		x, err := e.exportExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
		to, err := e.typeID(tn.Type())
		if err != nil {
			return nil, err
		}
		// M7: "from" records the source type id (needed for string<->
		// slice conversions to get the element kind).
		var from any
		if tv, ok := e.info.Types[c.Args[0]]; ok && tv.Type != nil {
			fid, err := e.typeID(tv.Type)
			if err != nil {
				return nil, err
			}
			from = fid
		}
		// "type" records the conversion target: NodeFactory stamps it on
		// the built node (M4: interface boxing needs the target type id).
		fields := map[string]any{"x": x, "to": to, "type": to}
		if from != nil {
			fields["from"] = from
		}
		return e.node("convert", c, fields), nil
	}
	if v, ok := e.info.Uses[funIdent].(*types.Var); ok {
		// M5 (WS F): call through a func-valued variable (recursion,
		// `defer f()`). The variable's signature type is already in the
		// type table; the factory builds a func-value call node.
		if _, ok := v.Type().(*types.Signature); !ok {
			return nil, scopeErrorf("call of non-function variable %s is not supported in frontend/v1 scope (WS D)",
				funIdent.Name)
		}
		if sig, ok := v.Type().(*types.Signature); ok {
			if err := checkVariadicCall(c, sig); err != nil {
				return nil, err
			}
		}
		fun, err := e.exportIdent(funIdent)
		if err != nil {
			return nil, err
		}
		args, err := e.exportCallArgs(c)
		if err != nil {
			return nil, err
		}
		typ, err := e.callType(c)
		if err != nil {
			return nil, err
		}
		return e.node("call", c, map[string]any{
			"fun": fun, "args": args, "type": typ,
		}), nil
	}
	if fn, ok := e.info.Uses[funIdent].(*types.Func); ok {
		// M6 (WS H): call to a named top-level function (or a
		// monomorphized generic instantiation). The call target resolves
		// by object id; the factory builds a direct func-value call.
		return e.exportFuncCall(c, funIdent, fn)
	}
	b, ok := e.info.Uses[funIdent].(*types.Builtin)
	if !ok {
		return nil, scopeErrorf("calls to non-builtin functions are not supported in frontend/v1 scope (WS D)")
	}
	// M8: fail fast on builtins outside the v1 surface (fuzzer find: clear/
	// min/max used to slip through and die as "internal error building AST"
	// in NodeFactory). The surface is freeze-stub.md "Builtin nodes".
	switch b.Name() {
	case "len", "cap", "make", "new", "append", "copy", "delete",
		"complex", "real", "imag", "print", "println", "panic", "recover":
	default:
		return nil, scopeErrorf("call to builtin %q is not supported in frontend/v1 scope (WS G intrinsic surface)", b.Name())
	}
	fun, err := e.exportIdent(funIdent)
	if err != nil {
		return nil, err
	}
	// M7: new(T) and make(T, ...) take a type as the first argument.
	// The type is recorded in the result type id (via callType); only
	// the value arguments are exported as expressions.
	// Non-nil so empty args marshal as [] rather than null.
	args := []*Node{}
	switch b.Name() {
	case "new":
		// new(T): no value arguments; the pointer type is the result.
		// P3-M2b (ADR-0002 D3): new(sync.X) constructs the primitive.
		if len(c.Args) == 1 {
			if tv, ok := e.info.Types[c.Args[0]]; ok && tv.Type != nil {
				if nt := syncPrimitiveNamed(tv.Type); nt != nil {
					ntyp, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					return e.node("syncnew", c, map[string]any{
						"synctyp": nt.Obj().Name(), "type": ntyp,
					}), nil
				}
			}
		}
	case "make":
		// make(T, len[, cap]): skip the type, export len/cap.
		if len(c.Args) < 1 {
			return nil, scopeErrorf("make without a type argument")
		}
		for _, a := range c.Args[1:] {
			x, err := e.exportExpr(a)
			if err != nil {
				return nil, err
			}
			args = append(args, x)
		}
	default:
		var err error
		args, err = e.exportCallArgs(c)
		if err != nil {
			return nil, err
		}
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{
		"fun": fun, "args": args, "builtin": b.Name(), "type": typ,
	}
	// M7: mark append(s, x...) spread; the factory expands the string
	// into individual bytes.
	if b.Name() == "append" && c.Ellipsis.IsValid() {
		fields["ellipsis"] = true
	}
	return e.node("call", c, fields), nil
}

// exportFmtCall exports a call to one of the seven D4 fmt stub functions
// (M6, WS H — the Phase-1 fmt import overlay, PLAN.md D4). Any other fmt.X
// is a scope error naming Phase 3.
func (e *exporter) exportFmtCall(c *ast.CallExpr, sel *ast.SelectorExpr) (*Node, error) {
	name := sel.Sel.Name
	if !fmtOverlayFuncs[name] {
		return nil, scopeErrorf("fmt.%s is not supported in frontend/v1 scope (Phase 3)", name)
	}
	if obj, ok := e.info.Uses[sel.Sel].(*types.Func); !ok || obj.Pkg() == nil || obj.Pkg().Name() != "fmt" {
		return nil, scopeErrorf("fmt.%s did not resolve to the overlay package", name)
	}
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	// The Print variants return (int, error); Phase 1 supports them in
	// statement position only (results discarded) — the wire type is null.
	// The Sprint variants and Errorf return a single value.
	var typ any
	switch name {
	case "Print", "Printf", "Println":
		typ = nil
	default:
		typ, err = e.callType(c)
		if err != nil {
			return nil, err
		}
	}
	return e.node("fmtcall", c, map[string]any{"func": name, "args": args, "type": typ}), nil
}

// exportFuncCall exports a call to a named top-level function (M6, WS H).
// Generic functions resolve to their monomorphized instantiation via
// info.Instances; the call target is an "ident" node whose obj is the
// function/instantiation object id.
func (e *exporter) exportFuncCall(c *ast.CallExpr, funIdent *ast.Ident, fn *types.Func) (*Node, error) {
	fi, ok := e.funcByObj[fn]
	if !ok {
		return nil, fmt.Errorf("gofrontend: call to unknown function %s", fn.Name())
	}
	if sig, ok := fn.Type().(*types.Signature); ok {
		if err := checkVariadicCall(c, sig); err != nil {
			return nil, err
		}
	}
	var objID, sigID int
	if fi.isTemplate {
		ii, ok := e.info.Instances[funIdent]
		if !ok {
			return nil, fmt.Errorf("gofrontend: no instantiation recorded for call to generic function %s", fn.Name())
		}
		targs := typeList(ii.TypeArgs)
		key := instKeyForFunc(fi, targs, e.subst)
		inst, ok := e.instByKey[key]
		if !ok {
			return nil, fmt.Errorf("gofrontend: instantiation %s not enumerated (internal error)", key)
		}
		objID = inst.instID
		sigID = inst.sigID
	} else {
		objID = fi.funcID
		var err error
		sigID, err = e.typeID(fn.Type())
		if err != nil {
			return nil, err
		}
	}
	fun := e.node("ident", funIdent, map[string]any{
		"name": fn.Name(), "obj": objID, "type": sigID,
	})
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	return e.node("call", c, map[string]any{
		"fun": fun, "args": args, "type": typ,
	}), nil
}

// exportMethodCall exports a method call x.M(args) (M6, WS H). The receiver
// x is emitted as a "methodref" fun target carrying the resolved
// function/instantiation object id; the factory evaluates the receiver
// first and passes it as the first argument (params[0] of the method's
// funcdecl). Receiver-based dispatch only: the receiver type must match the
// declared receiver type after substitution — implicit &/* adjustments are
// a scope error (WS D).
func (e *exporter) exportMethodCall(c *ast.CallExpr, sel *ast.SelectorExpr, selection *types.Selection) (*Node, error) {
	if selection.Kind() != types.MethodVal {
		return nil, scopeErrorf("selector call %s.%s is not supported in frontend/v1 scope (WS D)",
			sel.X, sel.Sel.Name)
	}
	var mtargs []types.Type
	if ii, ok := e.info.Instances[sel.Sel]; ok {
		mtargs = typeList(ii.TypeArgs)
	}
	mc, err := e.resolveMethodCall(sel, selection, mtargs, e.subst)
	if err != nil {
		return nil, err
	}
	// Remediation (R1 P1-1, R2 P1-2): same variadic fail-fast as direct
	// calls; the spread form passes the slice directly and works.
	if err := checkVariadicCall(c, mc.decl.sig); err != nil {
		return nil, err
	}
	declRecv := mc.decl.sig.Recv().Type()
	xt, ok := e.info.Types[sel.X]
	if !ok || xt.Type == nil {
		return nil, fmt.Errorf("gofrontend: no type info for method receiver")
	}
	if substString(declRecv, mc.subst) != substString(xt.Type, e.subst) {
		return nil, scopeErrorf("method call %s needs an implicit address-of or deref on the receiver (WS D)", sel.Sel.Name)
	}
	var objID, sigID int
	if mc.needsInst {
		inst, ok := e.instByKey[mc.key]
		if !ok {
			return nil, fmt.Errorf("gofrontend: method instantiation %s not enumerated (internal error)", mc.key)
		}
		objID = inst.instID
		sigID = inst.sigID
	} else {
		objID = mc.decl.funcID
		// P1-1 (M6 remediation): wire the method's signature type id on the
		// methodref node (receiver as params[0], matching the funcdecl's
		// "sig") so the factory can adapt concrete arguments passed to
		// interface-typed parameters — the same call-site boxing direct
		// calls get. Without it the root fell back to inferring the
		// descriptor from the Java carrier type and a later type switch
		// silently took the wrong branch. The signature is concrete here
		// (!isTemplate), so the active substitution is the identity on it.
		var err error
		sigID, err = e.freshSigID(mc.decl.recv, mc.decl.sig.Params(), mc.decl.sig.Results(),
			mc.decl.sig.Variadic())
		if err != nil {
			return nil, err
		}
	}
	recv, err := e.exportExpr(sel.X)
	if err != nil {
		return nil, err
	}
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	fun := e.node("methodref", sel, map[string]any{"obj": objID, "recv": recv, "type": sigID})
	return e.node("call", c, map[string]any{
		"fun": fun, "args": args, "type": typ,
	}), nil
}

// exportSyncOp exports a method call on a synthetic sync primitive
// (P3-M2b, WS D, ADR-0002) as an additive "syncop" wire node. The receiver
// evaluates to the Java primitive object itself (ADR-0002 D1: the Go value
// IS the object), so the pointer/value receiver distinction is a no-op at
// the value level — this deliberately bypasses exportMethodCall's WS D
// no-implicit-& rule, which would reject m.Lock() on an addressable value
// despite every real sync method having a pointer receiver. Out-of-scope
// methods (TryLock/TryRLock/RLocker/WaitGroup.Go) fail fast here with a
// named M2 scope error (ADR-0002 D6).
func (e *exporter) exportSyncOp(c *ast.CallExpr, sel *ast.SelectorExpr, nt *types.Named) (*Node, error) {
	typeName := nt.Obj().Name()
	op := sel.Sel.Name
	if !syncOpsInScope[typeName][op] {
		return nil, scopeErrorf("sync.%s.%s is not in M2 scope", typeName, op)
	}
	recv, err := e.exportExpr(sel.X)
	if err != nil {
		return nil, err
	}
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	return e.node("syncop", c, map[string]any{
		"op": op, "synctyp": typeName, "recv": recv, "args": args, "type": typ,
	}), nil
}

// exportCallArgs exports a call's argument expressions.
func (e *exporter) exportCallArgs(c *ast.CallExpr) ([]*Node, error) {
	args := make([]*Node, 0, len(c.Args))
	for _, a := range c.Args {
		x, err := e.exportExpr(a)
		if err != nil {
			return nil, err
		}
		args = append(args, x)
	}
	return args, nil
}

// callType returns the type-table id of a call's result type (nil when the
// call has no result, e.g. a builtin like panic or a void function).
// go/types reports a no-result call as an empty *types.Tuple.
func (e *exporter) callType(c *ast.CallExpr) (any, error) {
	var typ any
	if tv, ok := e.info.Types[c]; ok && tv.Type != nil {
		if t, ok := tv.Type.(*types.Tuple); ok {
			if t.Len() == 0 {
				return nil, nil
			}
			return nil, scopeErrorf("multi-valued call result is not supported in frontend/v1 scope (WS D)")
		}
		tid, err := e.typeID(tv.Type)
		if err != nil {
			return nil, err
		}
		typ = tid
	}
	return typ, nil
}

// exportFuncLit exports a function literal (M5, WS F). Params and results
// are emitted as kind:"param" nodes (the funcdecl param schema:
// name/obj/type), from the go/types signature tuple so unnamed
// parameters/results are covered too.
func (e *exporter) exportFuncLit(l *ast.FuncLit) (*Node, error) {
	tv, ok := e.info.Types[l.Type]
	if !ok || tv.Type == nil {
		return nil, fmt.Errorf("gofrontend: no type info for function literal")
	}
	sig, ok := tv.Type.(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("gofrontend: function literal has non-signature type %s", tv.Type)
	}
	params, err := e.exportSignatureVars(l.Type, sig.Params())
	if err != nil {
		return nil, err
	}
	results, err := e.exportSignatureVars(l.Type, sig.Results())
	if err != nil {
		return nil, err
	}
	if l.Body == nil {
		return nil, scopeErrorf("function literal without body")
	}
	body, err := e.exportBlock(l.Body)
	if err != nil {
		return nil, err
	}
	return e.node("funclit", l, map[string]any{
		"params": params, "results": results, "body": body,
	}), nil
}

// exportSignatureVars emits one kind:"param" node per *types.Var in a
// signature tuple (defining each in the object table as a param). Positions
// are best-effort (the enclosing func type); per-var positions don't exist
// for unnamed parameters.
func (e *exporter) exportSignatureVars(pos ast.Node, t *types.Tuple) ([]*Node, error) {
	out := make([]*Node, 0, t.Len())
	for i := 0; i < t.Len(); i++ {
		p, err := e.exportParamVar(pos, t.At(i))
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Composite-type expressions (M4, WS E).
// ---------------------------------------------------------------------------

// exportCompositeLit exports a composite literal. The literal's type comes
// from go/types (info.Types), so syntactically elided element literals
// (`[][]int{{1,2}}`, `[]*Point{{1,2}}`) arrive with their full explicit
// type — the wire never carries elision.
func (e *exporter) exportCompositeLit(l *ast.CompositeLit) (*Node, error) {
	t, err := e.exprType(l)
	if err != nil {
		return nil, err
	}
	return e.exportCompositeLitAs(l, t)
}

// exportCompositeLitAs is exportCompositeLit with an explicit type id
// (used for the &T{...} normalization, where the pointer type comes from
// the enclosing UnaryExpr).
func (e *exporter) exportCompositeLitAs(l *ast.CompositeLit, t int) (*Node, error) {
	// P3-M2b (ADR-0002 D3): a composite literal of a sync primitive
	// constructs the Java object directly — gc's ready-to-use zero value.
	// (Elements are impossible: the fields are unexported, so go/types
	// rejects any; the guard is defense in depth.)
	if tv, ok := e.info.Types[l]; ok && tv.Type != nil {
		if nt := syncPrimitiveNamed(tv.Type); nt != nil {
			if len(l.Elts) > 0 {
				return nil, scopeErrorf("sync.%s literal with elements is not in M2 scope", nt.Obj().Name())
			}
			return e.node("syncnew", l, map[string]any{
				"synctyp": nt.Obj().Name(), "type": t,
			}), nil
		}
	}
	keyed := false
	var keys []*Node
	var fieldKeys []string
	// Non-nil so an empty literal marshals as [] rather than null
	// (the factory requires values to be an array).
	values := []*Node{}
	for _, el := range l.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			keyed = true
			v, err := e.exportExpr(kv.Value)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
			// Struct literal keys are field names (not expressions);
			// map/array/slice literal keys are index expressions.
			if id, ok := kv.Key.(*ast.Ident); ok && e.isStructLitKey(l, id) {
				fieldKeys = append(fieldKeys, id.Name)
			} else {
				k, err := e.exportExpr(kv.Key)
				if err != nil {
					return nil, err
				}
				keys = append(keys, k)
			}
		} else {
			v, err := e.exportExpr(el)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
		}
	}
	if keyed && len(keys)+len(fieldKeys) != len(values) {
		return nil, fmt.Errorf("gofrontend: keyed literal key/value count mismatch")
	}
	fields := map[string]any{"type": t, "keyed": keyed, "values": values}
	if len(fieldKeys) > 0 {
		fields["fieldkeys"] = fieldKeys
	}
	if len(keys) > 0 {
		fields["keys"] = keys
	}
	return e.node("complit", l, fields), nil
}

// isStructLitKey reports whether id is a struct field name in a keyed
// composite literal (as opposed to a map index expression).
func (e *exporter) isStructLitKey(l *ast.CompositeLit, id *ast.Ident) bool {
	if tv, ok := e.info.Types[l]; ok && tv.Type != nil {
		if _, ok := tv.Type.Underlying().(*types.Struct); ok {
			return true
		}
	}
	return false
}

// exportIndex exports x[index]. commaOk marks the two-valued map form
// (set by exportAssign/exportDefine from the assignment shape); the
// value type is the map's element type (go/types records a tuple for the
// two-valued form).
func (e *exporter) exportIndex(x *ast.IndexExpr, commaOk bool) (*Node, error) {
	var t int
	var err error
	if commaOk {
		mt, ok := e.info.Types[x.X].Type.Underlying().(*types.Map)
		if !ok {
			return nil, fmt.Errorf("gofrontend: comma-ok index on non-map type %v", e.info.Types[x.X].Type)
		}
		if t, err = e.typeID(mt.Elem()); err != nil {
			return nil, err
		}
	} else {
		if t, err = e.exprType(x); err != nil {
			return nil, err
		}
	}
	xe, err := e.exportExpr(x.X)
	if err != nil {
		return nil, err
	}
	ie, err := e.exportExpr(x.Index)
	if err != nil {
		return nil, err
	}
	return e.node("index", x, map[string]any{
		"x": xe, "index": ie, "type": t, "commaOk": commaOk,
	}), nil
}

// exportSlice exports x[lo:hi] and x[lo:hi:max] (absent bounds are null).
func (e *exporter) exportSlice(x *ast.SliceExpr) (*Node, error) {
	t, err := e.exprType(x)
	if err != nil {
		return nil, err
	}
	xe, err := e.exportExpr(x.X)
	if err != nil {
		return nil, err
	}
	var lo, hi, max *Node
	if x.Low != nil {
		if lo, err = e.exportExpr(x.Low); err != nil {
			return nil, err
		}
	}
	if x.High != nil {
		if hi, err = e.exportExpr(x.High); err != nil {
			return nil, err
		}
	}
	if x.Max != nil {
		if max, err = e.exportExpr(x.Max); err != nil {
			return nil, err
		}
	}
	return e.node("sliceexpr", x, map[string]any{
		"x": xe, "lo": lo, "hi": hi, "max": max, "type": t,
	}), nil
}

// exportSelector exports x.Field. selpath is go/types' field-index path
// through (possibly embedded) structs, so the factory needs no promotion
// logic of its own.
func (e *exporter) exportSelector(x *ast.SelectorExpr) (*Node, error) {
	t, err := e.exprType(x)
	if err != nil {
		return nil, err
	}
	xe, err := e.exportExpr(x.X)
	if err != nil {
		return nil, err
	}
	sel, ok := e.info.Selections[x]
	if !ok {
		return nil, scopeErrorf("selector %q is not a field selection (WS D)", x.Sel.Name)
	}
	// M6 (WS H): a selector denoting a method in value position is a
	// method value (bound receiver) — not supported (WS H follow-up).
	// Method CALLS are handled by exportMethodCall.
	if _, ok := sel.Obj().(*types.Func); ok {
		return nil, scopeErrorf("method value %s (bound receiver) is not supported in frontend/v1 scope (WS H)", x.Sel.Name)
	}
	idx := sel.Index()
	path := make([]any, len(idx))
	for i, v := range idx {
		path[i] = v
	}
	return e.node("selector", x, map[string]any{
		"x": xe, "field": x.Sel.Name, "selpath": path, "type": t,
	}), nil
}

// exportTypeAssert exports x.(T). commaOk marks the two-valued form; the
// value type is the asserted type (go/types records a tuple for the
// two-valued form).
func (e *exporter) exportTypeAssert(x *ast.TypeAssertExpr, commaOk bool) (*Node, error) {
	if x.Type == nil {
		return nil, scopeErrorf("x.(type) outside a type switch is not supported in frontend/v1 scope")
	}
	var t int
	var err error
	if commaOk {
		if t, err = e.exprType(x.Type); err != nil {
			return nil, err
		}
	} else {
		if t, err = e.exprType(x); err != nil {
			return nil, err
		}
	}
	xe, err := e.exportExpr(x.X)
	if err != nil {
		return nil, err
	}
	return e.node("typeassert", x, map[string]any{
		"x": xe, "type": t, "commaOk": commaOk,
	}), nil
}

// unparen strips parenthesized wrappers (used for comma-ok shape detection;
// the exporter normalizes the paren away in that position).
func unparen(x ast.Expr) ast.Expr {
	for {
		p, ok := x.(*ast.ParenExpr)
		if !ok {
			return x
		}
		x = p.X
	}
}

// isCommaOkRhs reports whether r is a two-valued map-index or type-assertion
// expression (the `v, ok := m[k]` / `v, ok := x.(T)` shapes).
func isCommaOkRhs(r ast.Expr) bool {
	switch unparen(r).(type) {
	case *ast.IndexExpr, *ast.TypeAssertExpr:
		return true
	}
	return false
}
