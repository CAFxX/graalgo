// Package emit implements stage 3 of the frontend/v2 pipeline: typed AST
// → in-memory backend program.
//
// There is no JSON wire format: v2 constructs the program representation
// directly in memory (his 2026-10-15 order: "that should obviously skip
// the json"). One AST node kind maps to one emit function — no monolith.
// There is no desugaring of stdlib calls, no package-name special cases,
// and no synthetic packages. What the source says is what the IR says.
package emit

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"runtime"
	"sort"

	"graalgo-frontend-v2/checker"
)

// Document is the top-level in-memory v2 program representation. It is
// never serialized: there is no JSON wire format in v2.
type Document struct {
	Frontend  string
	Generator map[string]any
	Package   map[string]any
	Types     []any
	Objects   []any
	Funcs     []any
	Globals   []any
}

// Emitter holds the state for one emission.
type Emitter struct {
	fset *token.FileSet
	info *checker.Result
	// files maps absolute path → index in package.files.
	files []string
	// Type table: go/types.Type → id.
	typeIDs map[types.Type]int
	types   []any
	// Object table: types.Object → id.
	objIDs  map[types.Object]int
	objects []any
	// err is the first unhandled-kind failure recorded while describing
	// objects or types. describeObject/describeType cannot return an
	// error through their many call sites (typeID is used inline in
	// node fields), so the failure is sticky: Emit returns it instead
	// of the document, so no consumer ever sees the placeholder entry
	// recorded alongside it. This is what makes "loudly reject" hold
	// for the type/object tables, not just for AST nodes.
	err error
}

// failf records the first unhandled-kind failure; later ones are dropped
// so the reported error is deterministic.
func (e *Emitter) failf(format string, args ...any) {
	if e.err == nil {
		e.err = fmt.Errorf(format, args...)
	}
}

// Emit runs stage 3 and returns the in-memory program document. The
// document is consumed directly by the backend via the Go→Java API;
// it is never serialized to JSON.
func Emit(cr *checker.Result, pkg *ast.Package, files []string) (*Document, error) {
	e := &Emitter{
		fset:    cr.Fset,
		info:    cr,
		files:   files,
		typeIDs: make(map[types.Type]int),
		objIDs:  make(map[types.Object]int),
	}
	return e.emitDocument(pkg)
}

func (e *Emitter) emitDocument(pkg *ast.Package) (*Document, error) {
	// Emit order: types and objects are populated lazily as nodes are
	// emitted (ids registered before description, so recursion works).
	// We do two passes: first declare all top-level funcs and globals
	// (assigning object ids), then emit bodies.
	//
	// Deterministic emission order (V2-12): pkg.Files is a Go map, so
	// ranging over it directly would make function order and type/object
	// ids vary run to run. Files are visited sorted by name and
	// declarations sorted by source position, so the same package always
	// produces the same document — a prerequisite for reproducible builds
	// and snapshot testing.
	decls := sortedFuncDecls(pkg)
	genDecls := sortedTopLevelGenDecls(pkg)

	var funcs []any
	for _, fd := range decls {
		obj := e.info.Info.Defs[fd.Name]
		if obj == nil {
			return nil, fmt.Errorf("emit: no object for func %s", fd.Name.Name)
		}
		e.declareObject(obj)
	}

	// Declare every top-level non-function name up front, so object ids
	// are assigned in deterministic source order before any body or
	// initializer is emitted. Package-level vars are emitted as Globals
	// below; const and type names are recorded in the object table with
	// their honest kinds (const/typename) instead of being silently
	// skipped, which previously lost them entirely. Imports are the
	// checker's job and carry no object to record.
	for _, gd := range genDecls {
		if gd.Tok == token.IMPORT {
			continue
		}
		for _, spec := range gd.Specs {
			var names []*ast.Ident
			switch sp := spec.(type) {
			case *ast.ValueSpec:
				names = sp.Names
			case *ast.TypeSpec:
				names = []*ast.Ident{sp.Name}
			case *ast.ImportSpec:
				continue
			default:
				return nil, fmt.Errorf("emit: unsupported top-level spec %T", spec)
			}
			for _, name := range names {
				if obj := e.info.Info.Defs[name]; obj != nil {
					e.declareObject(obj)
				}
			}
		}
	}
	if e.err != nil {
		return nil, e.err
	}

	for _, fd := range decls {
		fn, err := e.emitFuncDecl(fd)
		if err != nil {
			return nil, err
		}
		funcs = append(funcs, fn)
	}

	globals, err := e.emitGlobals(genDecls)
	if err != nil {
		return nil, err
	}
	if e.err != nil {
		return nil, e.err
	}

	baseNames := make([]string, len(e.files))
	for i, f := range e.files {
		baseNames[i] = fileBase(f)
	}
	return &Document{
		Frontend: "v2",
		Generator: map[string]any{
			"tool":    "gofrontend-v2",
			"version": "0.1.0",
			"goroot":  "go1.27.1",
		},
		Package: map[string]any{
			"name":  pkg.Name,
			"files": baseNames,
			"lang":  "go1.27",
		},
		Types:   e.types,
		Objects: e.objects,
		Funcs:   funcs,
		Globals: globals,
	}, nil
}

// emitGlobals emits one "global" node per package-level VAR ValueSpec,
// in deterministic source order. Each entry carries the declared names,
// their object ids, one type id per name (names in one spec may have
// different types, so a single shared type would be a fabrication),
// and the emitted initializer expressions, if any. A spec with no
// initializer (e.g. `var Name string`) has an empty values list —
// the variable exists, it is just zero-initialized.
func (e *Emitter) emitGlobals(genDecls []*ast.GenDecl) ([]any, error) {
	globals := []any{}
	for _, gd := range genDecls {
		if gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				return nil, fmt.Errorf("emit: unsupported var spec %T", spec)
			}
			names := []string{}
			objs := []any{}
			typs := []any{}
			for _, name := range vs.Names {
				names = append(names, name.Name)
				obj := e.info.Info.Defs[name]
				if obj == nil {
					// Blank identifier: no object exists.
					objs = append(objs, -1)
					if vs.Type != nil {
						if tv, ok := e.info.Info.Types[vs.Type]; ok && tv.Type != nil {
							typs = append(typs, e.typeID(tv.Type))
							continue
						}
					}
					typs = append(typs, nil)
					continue
				}
				objs = append(objs, e.declareObject(obj))
				typs = append(typs, e.typeID(obj.Type()))
			}
			values := []any{}
			for _, v := range vs.Values {
				vn, err := e.emitExpr(v)
				if err != nil {
					return nil, err
				}
				values = append(values, vn)
			}
			globals = append(globals, e.node("global", vs, map[string]any{
				"names":  names,
				"objs":   objs,
				"types":  typs,
				"values": values,
			}))
		}
	}
	return globals, nil
}

// sortedFuncDecls returns every top-level function declaration in the
// package in deterministic order: files sorted by name, then declarations
// sorted by source position. Non-function declarations are not returned
// here; they are handled separately by sortedTopLevelGenDecls.
// Sorting is structural — by filename and token position — never keyed
// on any package, symbol, or test name.
func sortedFuncDecls(pkg *ast.Package) []*ast.FuncDecl {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	var decls []*ast.FuncDecl
	for _, name := range names {
		file := pkg.Files[name]
		var fds []*ast.FuncDecl
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			fds = append(fds, fd)
		}
		// Positions from one *ast.File share the same FileSet base, so
		// token.Pos comparison is a total source-position order within the
		// file. go/parser already produces Decls in source order; this sort
		// states the contract explicitly instead of relying on that
		// implementation detail.
		sort.SliceStable(fds, func(i, j int) bool { return fds[i].Pos() < fds[j].Pos() })
		decls = append(decls, fds...)
	}
	return decls
}

// sortedTopLevelGenDecls returns every top-level GenDecl (var, const,
// type, import) in the same deterministic order as sortedFuncDecls:
// files sorted by name, declarations sorted by source position.
func sortedTopLevelGenDecls(pkg *ast.Package) []*ast.GenDecl {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	var decls []*ast.GenDecl
	for _, name := range names {
		file := pkg.Files[name]
		var gds []*ast.GenDecl
		for _, d := range file.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			gds = append(gds, gd)
		}
		sort.SliceStable(gds, func(i, j int) bool { return gds[i].Pos() < gds[j].Pos() })
		decls = append(decls, gds...)
	}
	return decls
}

func fileBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// pos renders a source position per §3 of the spec.
func (e *Emitter) pos(n ast.Node) map[string]any {
	p := e.fset.Position(n.Pos())
	end := e.fset.Position(n.End())
	fileIdx := 0
	for i, f := range e.files {
		if f == p.Filename {
			fileIdx = i
			break
		}
	}
	// col is rune-based (spec §3).
	return map[string]any{
		"file": fileIdx,
		"line": p.Line,
		"col":  runeCol(p),
		"off":  p.Offset,
		"len":  end.Offset - p.Offset,
	}
}

func runeCol(p token.Position) int {
	// token.Position.Column is byte-based; the spec wants rune-based.
	// Without the source line we approximate with the byte column.
	// TODO(v2): track source lines for exact rune columns.
	return p.Column
}

// declareObject assigns an object id if not already assigned.
func (e *Emitter) declareObject(obj types.Object) int {
	if obj == nil {
		e.failf("emit: nil object")
		return -1
	}
	return e.declareObjectParam(obj, false)
}

// declareParam assigns an object id for a function parameter.
func (e *Emitter) declareParam(obj types.Object) int {
	return e.declareObjectParam(obj, true)
}

func (e *Emitter) declareObjectParam(obj types.Object, isParam bool) int {
	if id, ok := e.objIDs[obj]; ok {
		return id
	}
	id := len(e.objects)
	e.objIDs[obj] = id
	// Placeholder; filled by describeObject below. Registered before
	// description so recursive references work.
	e.objects = append(e.objects, nil)
	e.objects[id] = e.describeObject(obj, isParam)
	return id
}

func (e *Emitter) describeObject(obj types.Object, isParam bool) map[string]any {
	// Nil and builtin objects have no type; asking typeID for it
	// would record a spurious failure.
	var typ any
	if obj.Type() != nil {
		typ = e.typeID(obj.Type())
	}
	switch o := obj.(type) {
	case *types.Var:
		return map[string]any{
			"kind":  "var",
			"name":  o.Name(),
			"type":  typ,
			"param": isParam,
		}
	case *types.Func:
		return map[string]any{
			"kind": "func",
			"name": o.Name(),
			"type": typ,
		}
	case *types.Const:
		// Constant value as its exact string, so the backend never
		// has to guess the value from the type alone.
		val := ""
		if o.Val() != nil {
			val = o.Val().ExactString()
		}
		return map[string]any{
			"kind": "const",
			"name": o.Name(),
			"type": typ,
			"val":  val,
		}
	case *types.TypeName:
		return map[string]any{
			"kind": "typename",
			"name": o.Name(),
			"type": typ,
		}
	case *types.Nil:
		return map[string]any{
			"kind": "nil",
			"name": o.Name(),
			"type": typ,
		}
	case *types.Builtin:
		return map[string]any{
			"kind": "builtin",
			"name": o.Name(),
			"type": typ,
		}
	default:
		// Loudly reject: mislabelling an object (the old default
		// called everything "var") makes the backend trust wrong
		// metadata. The placeholder below is never returned to a
		// caller — Emit converts the sticky error into a failure.
		e.failf("emit: unsupported object kind %T (%s)", obj, obj.Name())
		return map[string]any{
			"kind": "unsupported",
			"name": obj.Name(),
			"type": typ,
		}
	}
}

// typeID assigns a type-table id, registering before describing (recursion).
func (e *Emitter) typeID(t types.Type) int {
	if t == nil {
		e.failf("emit: nil type")
		return -1
	}
	if id, ok := e.typeIDs[t]; ok {
		return id
	}
	id := len(e.types)
	e.typeIDs[t] = id
	e.types = append(e.types, nil) // placeholder for recursion
	e.types[id] = e.describeType(t)
	return id
}

func (e *Emitter) describeType(t types.Type) map[string]any {
	if t == nil {
		e.failf("emit: nil type")
		return map[string]any{
			"kind":   "unsupported",
			"detail": map[string]any{"name": "<nil>"},
		}
	}
	switch tt := t.(type) {
	case *types.Basic:
		class := basicClass(tt)
		return map[string]any{
			"kind": "basic",
			"detail": map[string]any{
				"name":    tt.Name(),
				"class":   class,
				"untyped": tt.Info()&types.IsUntyped != 0,
			},
		}
	case *types.Pointer:
		return map[string]any{
			"kind":   "pointer",
			"detail": map[string]any{"elem": e.typeID(tt.Elem())},
		}
	case *types.Slice:
		return map[string]any{
			"kind":   "slice",
			"detail": map[string]any{"elem": e.typeID(tt.Elem())},
		}
	case *types.Array:
		return map[string]any{
			"kind":   "array",
			"detail": map[string]any{"len": tt.Len(), "elem": e.typeID(tt.Elem())},
		}
	case *types.Map:
		return map[string]any{
			"kind": "map",
			"detail": map[string]any{
				"key":  e.typeID(tt.Key()),
				"elem": e.typeID(tt.Elem()),
			},
		}
	case *types.Chan:
		var dir string
		switch tt.Dir() {
		case types.SendRecv:
			dir = "both"
		case types.SendOnly:
			dir = "send"
		case types.RecvOnly:
			dir = "recv"
		}
		return map[string]any{
			"kind":   "chan",
			"detail": map[string]any{"dir": dir, "elem": e.typeID(tt.Elem())},
		}
	case *types.Signature:
		params := []any{}
		for i := 0; i < tt.Params().Len(); i++ {
			p := tt.Params().At(i)
			params = append(params, map[string]any{
				"name": p.Name(),
				"type": e.typeID(p.Type()),
			})
		}
		results := []any{}
		for i := 0; i < tt.Results().Len(); i++ {
			r := tt.Results().At(i)
			results = append(results, map[string]any{
				"name": r.Name(),
				"type": e.typeID(r.Type()),
			})
		}
		return map[string]any{
			"kind": "signature",
			"detail": map[string]any{
				"params":   params,
				"results":  results,
				"variadic": tt.Variadic(),
			},
		}
	case *types.Named:
		return map[string]any{
			"kind": "named",
			"detail": map[string]any{
				"name":       namedName(tt),
				"underlying": e.typeID(tt.Underlying()),
				"methods":    []any{},
			},
		}
	case *types.Struct:
		// Real field offsets from the go/types size model for the
		// gc toolchain on the host architecture — the same model
		// the checker itself uses. A hardcoded 0 used to claim
		// every field lives at the struct base.
		sizes := types.SizesFor("gc", runtime.GOARCH)
		if sizes == nil {
			e.failf("emit: no go/types size model for gc/%s", runtime.GOARCH)
			return map[string]any{
				"kind":   "struct",
				"detail": map[string]any{"fields": []any{}},
			}
		}
		vars := make([]*types.Var, tt.NumFields())
		for i := 0; i < tt.NumFields(); i++ {
			vars[i] = tt.Field(i)
		}
		offsets := sizes.Offsetsof(vars)
		fields := []any{}
		for i := 0; i < tt.NumFields(); i++ {
			f := tt.Field(i)
			fields = append(fields, map[string]any{
				"name":     f.Name(),
				"type":     e.typeID(f.Type()),
				"tag":      tt.Tag(i),
				"embedded": f.Embedded(),
				"offset":   offsets[i],
			})
		}
		return map[string]any{
			"kind":   "struct",
			"detail": map[string]any{"fields": fields},
		}
	case *types.Tuple:
		// go/types gives multi-value and no-value expressions a
		// tuple type (e.g. a call to println has the empty tuple).
		// Describe it as a tuple of element types, not as a basic.
		elems := []any{}
		for i := 0; i < tt.Len(); i++ {
			elems = append(elems, e.typeID(tt.At(i).Type()))
		}
		return map[string]any{
			"kind":   "tuple",
			"detail": map[string]any{"elems": elems},
		}
	case *types.Alias:
		// An alias is not a new type: it names its right-hand side.
		// Record both the alias name and the aliased type, so the
		// backend never has to re-derive the aliasing.
		if tt.Rhs() == nil || tt.Obj() == nil {
			e.failf("emit: alias type %s is unresolved", tt.String())
			return map[string]any{
				"kind":   "alias",
				"detail": map[string]any{"name": tt.String()},
			}
		}
		name := tt.Obj().Name()
		if pkg := tt.Obj().Pkg(); pkg != nil && pkg.Path() != "" {
			name = pkg.Path() + "." + name
		}
		return map[string]any{
			"kind": "alias",
			"detail": map[string]any{
				"name": name,
				"rhs":  e.typeID(tt.Rhs()),
			},
		}
	case *types.Interface:
		return map[string]any{
			"kind": "interface",
			"detail": map[string]any{
				"empty":    tt.Empty(),
				"methods":  []any{},
				"embedded": []any{},
			},
		}
	default:
		// Loudly reject: the old default encoded every unhandled
		// type (type parameters, unions, tuples, ...) as
		// basic/unknown, i.e. plausible-looking wrong metadata.
		// The placeholder below is never returned to a caller —
		// Emit converts the sticky error into a failure.
		e.failf("emit: unsupported type %T (%s)", t, t.String())
		return map[string]any{
			"kind":   "unsupported",
			"detail": map[string]any{"name": t.String()},
		}
	}
}

func basicClass(b *types.Basic) string {
	if b.Kind() == types.UnsafePointer {
		return "pointer"
	}
	switch b.Info() & types.IsNumeric {
	case types.IsBoolean:
		return "bool"
	case types.IsInteger:
		return "int"
	case types.IsUnsigned:
		return "uint"
	case types.IsFloat:
		return "float"
	case types.IsComplex:
		return "complex"
	}
	if b.Info()&types.IsString != 0 {
		return "string"
	}
	if b.Info()&types.IsUntyped != 0 {
		return "untyped"
	}
	return "unknown"
}

func namedName(n *types.Named) string {
	if obj := n.Obj(); obj != nil {
		if pkg := obj.Pkg(); pkg != nil && pkg.Path() != "" {
			return pkg.Path() + "." + obj.Name()
		}
		return obj.Name()
	}
	return n.String()
}
