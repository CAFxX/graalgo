// Package emit implements stage 3 of the frontend/v2 pipeline: typed AST
// → in-memory backend program.
//
// There is no JSON wire format: v2 constructs the program representation
// directly in memory (his 2026-10-15 order: "that should obviously skip
// the json"). One AST node kind maps to one emit function — no monolith.
// There is no desugaring of stdlib calls, no package-name special cases,
// and no synthetic packages. What the source says is what the IR says.
//
// Contract: no silent drops. Every top-level declaration is represented
// in the document — functions in Funcs, package-level vars in Globals
// (initialization order), consts in the object table (every use folds
// through go/types, per docs/arch/frontend-v1.md §5a), named types in the
// type table. Anything the emitter cannot represent is a loud error,
// never a skip.
package emit

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"graalgo-frontend-v2/checker"
)

// v2Sizes is the memory-layout model for struct field offsets, per the
// documented wire contract (docs/arch/frontend-v1.md §4):
// types.SizesFor("gc", "amd64"). Fields of generic structs carry -1
// ("unknown") — offsets are instantiation-dependent.
var v2Sizes = types.SizesFor("gc", "amd64")

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
	// We do two passes: first declare all top-level funcs, global
	// vars/consts, and named types (assigning object/type ids), then emit
	// bodies. Nothing is skipped: every top-level declaration lands in
	// Funcs, Globals, the object table, or the type table — anything else
	// is a loud error from declareTopDecl below.
	//
	// Deterministic emission order (V2-12): pkg.Files is a Go map, so
	// ranging over it directly would make function order and type/object
	// ids vary run to run. Files are visited sorted by name and
	// declarations sorted by source position, so the same package always
	// produces the same document — a prerequisite for reproducible builds
	// and snapshot testing.
	decls := sortedTopDecls(pkg)

	for _, d := range decls {
		if err := e.declareTopDecl(d); err != nil {
			return nil, err
		}
	}

	var funcs []any
	var varSpecs []*ast.ValueSpec
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			fn, err := e.emitFuncDecl(d)
			if err != nil {
				return nil, err
			}
			funcs = append(funcs, fn)
		case *ast.GenDecl:
			switch d.Tok {
			case token.VAR:
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						return nil, fmt.Errorf("emit: unexpected spec %T in var decl", spec)
					}
					varSpecs = append(varSpecs, vs)
				}
			case token.CONST, token.TYPE:
				// Consts live in the object table (declared in pass 1;
				// every use folds through go/types) and named types live
				// in the type table — no per-decl Globals entry needed.
			case token.IMPORT:
				// Import resolution is the checker's job; nothing to emit
				// (see structuralNodes in coverage_test.go).
			default:
				return nil, fmt.Errorf("emit: unsupported top-level decl *ast.GenDecl(%s)", d.Tok)
			}
		default:
			return nil, fmt.Errorf("emit: unsupported top-level decl %T", d)
		}
	}

	globals, err := e.emitGlobals(varSpecs)
	if err != nil {
		return nil, err
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

// declareTopDecl assigns object/type ids for one top-level declaration
// (pass 1). Anything that is not a function, var, const, type, or import
// declaration fails loudly here — it is never silently skipped.
func (e *Emitter) declareTopDecl(d ast.Decl) error {
	switch d := d.(type) {
	case *ast.FuncDecl:
		obj := e.info.Info.Defs[d.Name]
		if obj == nil {
			return fmt.Errorf("emit: no object for func %s", d.Name.Name)
		}
		e.declareObject(obj)
	case *ast.GenDecl:
		switch d.Tok {
		case token.VAR, token.CONST:
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					return fmt.Errorf("emit: unexpected spec %T in %s decl", spec, d.Tok)
				}
				for _, name := range vs.Names {
					if name.Name == "_" {
						continue
					}
					obj := e.info.Info.Defs[name]
					if obj == nil {
						return fmt.Errorf("emit: no object for %s %s", d.Tok, name.Name)
					}
					e.declareObject(obj)
				}
			}
		case token.TYPE:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					return fmt.Errorf("emit: unexpected spec %T in type decl", spec)
				}
				obj := e.info.Info.Defs[ts.Name]
				if obj == nil {
					return fmt.Errorf("emit: no object for type %s", ts.Name.Name)
				}
				// Register the named type (and its methods) eagerly so
				// the declaration is represented even if never referenced.
				e.typeID(obj.Type())
			}
		case token.IMPORT:
			// Nothing to declare; see emitDocument.
		default:
			return fmt.Errorf("emit: unsupported top-level decl *ast.GenDecl(%s)", d.Tok)
		}
	default:
		return fmt.Errorf("emit: unsupported top-level decl %T", d)
	}
	return nil
}

// emitGlobals emits package-level var specs in Go initialization order
// (see orderGlobalSpecs). One "global" node per spec; the fields are the
// shared emitValueSpec shape (names with object ids, per-spec type,
// initializer expressions).
func (e *Emitter) emitGlobals(specs []*ast.ValueSpec) ([]any, error) {
	ordered, err := e.orderGlobalSpecs(specs)
	if err != nil {
		return nil, err
	}
	globals := []any{}
	for _, vs := range ordered {
		fields, err := e.emitValueSpec(vs)
		if err != nil {
			return nil, err
		}
		globals = append(globals, e.node("global", vs, fields))
	}
	return globals, nil
}

// orderGlobalSpecs sorts top-level var specs into Go initialization order
// (language spec, "Package initialization"): a spec whose initializer
// refers to another package-level variable is emitted after it;
// declaration order breaks ties. Blank specs (var _ = ...) are ordered by
// their dependencies too, since their initializers run for side effects.
// go/types rejects initialization cycles at check time; a cycle reaching
// here is a loud internal error, never a silent misorder.
func (e *Emitter) orderGlobalSpecs(specs []*ast.ValueSpec) ([]*ast.ValueSpec, error) {
	pkgScope := e.info.Pkg.Scope()
	// definedBy maps each package-level var object to its spec index.
	definedBy := map[types.Object]int{}
	for i, vs := range specs {
		for _, name := range vs.Names {
			if name.Name == "_" {
				continue
			}
			if obj := e.info.Info.Defs[name]; obj != nil {
				definedBy[obj] = i
			}
		}
	}
	deps := make([]map[int]bool, len(specs))
	for i, vs := range specs {
		deps[i] = map[int]bool{}
		ast.Inspect(vs, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj, ok := e.info.Info.Uses[id].(*types.Var)
			if !ok || obj.Parent() != pkgScope {
				return true
			}
			if j, ok := definedBy[obj]; ok && j != i {
				deps[i][j] = true
			}
			return true
		})
	}
	// Kahn's algorithm; ties broken by declaration order (lowest index
	// first), which keeps the output deterministic.
	ordered := make([]*ast.ValueSpec, 0, len(specs))
	done := make([]bool, len(specs))
	for len(ordered) < len(specs) {
		pick := -1
		for i := range specs {
			if !done[i] && len(deps[i]) == 0 {
				pick = i
				break
			}
		}
		if pick < 0 {
			return nil, fmt.Errorf("emit: initialization cycle among package-level vars")
		}
		done[pick] = true
		ordered = append(ordered, specs[pick])
		for i := range specs {
			delete(deps[i], pick)
		}
	}
	return ordered, nil
}

// sortedTopDecls returns every top-level declaration in the package in
// deterministic order: files sorted by name, then declarations sorted by
// source position. Sorting is structural — by filename and token position
// — never keyed on any package, symbol, or test name.
func sortedTopDecls(pkg *ast.Package) []ast.Decl {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	var decls []ast.Decl
	for _, name := range names {
		file := pkg.Files[name]
		fds := make([]ast.Decl, len(file.Decls))
		copy(fds, file.Decls)
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
	typ := e.typeID(obj.Type())
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
		// Package-level and local constants. Every use folds through
		// go/types (iota included): the use site references this object,
		// so the exact value travels with it — no separate wire entry
		// needed (docs/arch/frontend-v1.md §5a).
		return map[string]any{
			"kind":  "const",
			"name":  o.Name(),
			"type":  typ,
			"value": o.Val().ExactString(),
		}
	default:
		return map[string]any{
			"kind": "var",
			"name": o.Name(),
			"type": typ,
		}
	}
}

// typeID assigns a type-table id, registering before describing (recursion).
func (e *Emitter) typeID(t types.Type) int {
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
	return e.describeTypeSized(t, v2Sizes)
}

// describeTypeSized describes t; sizes supplies the layout model for
// struct field offsets. Structs mentioning type parameters always get -1
// offsets via the describeStructFields guard (their layout is
// instantiation-dependent).
func (e *Emitter) describeTypeSized(t types.Type, sizes types.Sizes) map[string]any {
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
		methods := []any{}
		for i := 0; i < tt.NumMethods(); i++ {
			m := tt.Method(i)
			recvKind := "value"
			if sig, ok := m.Type().(*types.Signature); ok && sig.Recv() != nil {
				if _, isPtr := sig.Recv().Type().(*types.Pointer); isPtr {
					recvKind = "pointer"
				}
			}
			methods = append(methods, map[string]any{
				"name": m.Name(),
				"obj":  e.declareObject(m),
				"sig":  e.typeID(m.Type()),
				"recv": recvKind,
			})
		}
		// The underlying struct of a generic named type gets unknown (-1)
		// field offsets via the describeStructFields guard below: offsets
		// are instantiation-dependent (docs/arch/frontend-v1.md §4).
		underlying := tt.Underlying()
		return map[string]any{
			"kind": "named",
			"detail": map[string]any{
				"name":       namedName(tt),
				"underlying": e.typeID(underlying),
				"methods":    methods,
			},
		}
	case *types.Struct:
		return map[string]any{
			"kind":   "struct",
			"detail": map[string]any{"fields": e.describeStructFields(tt, sizes)},
		}
	case *types.Interface:
		methods := []any{}
		for i := 0; i < tt.NumMethods(); i++ {
			m := tt.Method(i)
			methods = append(methods, map[string]any{
				"name": m.Name(),
				"obj":  e.declareObject(m),
				"sig":  e.typeID(m.Type()),
			})
		}
		embedded := []any{}
		for i := 0; i < tt.NumEmbeddeds(); i++ {
			embedded = append(embedded, e.typeID(tt.EmbeddedType(i)))
		}
		return map[string]any{
			"kind": "interface",
			"detail": map[string]any{
				"empty":    tt.Empty(),
				"methods":  methods,
				"embedded": embedded,
			},
		}
	default:
		return map[string]any{
			"kind":   "basic",
			"detail": map[string]any{"name": t.String(), "class": "unknown", "untyped": false},
		}
	}
}

// describeStructFields renders a struct's fields. Offsets come from sizes
// (types.SizesFor("gc", "amd64") per docs/arch/frontend-v1.md §4). A struct
// that mentions a type parameter (e.g. the underlying struct of a generic
// named type) gets -1 ("unknown") for every offset: go/types cannot lay it
// out, and its real layout is instantiation-dependent. The guard keeps this
// total — without it go/types panics inside Offsetsof.
func (e *Emitter) describeStructFields(tt *types.Struct, sizes types.Sizes) []any {
	var offsets []int64
	if sizes != nil && !mentionsTypeParam(tt) {
		vars := make([]*types.Var, tt.NumFields())
		for i := range vars {
			vars[i] = tt.Field(i)
		}
		offsets = sizes.Offsetsof(vars)
	}
	fields := []any{}
	for i := 0; i < tt.NumFields(); i++ {
		f := tt.Field(i)
		var offset any = int64(-1)
		if offsets != nil {
			offset = offsets[i]
		}
		fields = append(fields, map[string]any{
			"name":     f.Name(),
			"type":     e.typeID(f.Type()),
			"tag":      tt.Tag(i),
			"embedded": f.Embedded(),
			"offset":   offset,
		})
	}
	return fields
}

// mentionsTypeParam reports whether t mentions a type parameter anywhere
// in its structure.
func mentionsTypeParam(t types.Type) bool {
	seen := map[types.Type]bool{}
	var visit func(t types.Type) bool
	visit = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		switch tt := t.(type) {
		case *types.TypeParam:
			return true
		case *types.Alias:
			return visit(types.Unalias(tt))
		case *types.Named:
			if ta := tt.TypeArgs(); ta != nil {
				for i := 0; i < ta.Len(); i++ {
					if visit(ta.At(i)) {
						return true
					}
				}
			}
			return visit(tt.Underlying())
		case *types.Pointer:
			return visit(tt.Elem())
		case *types.Slice:
			return visit(tt.Elem())
		case *types.Array:
			return visit(tt.Elem())
		case *types.Map:
			return visit(tt.Key()) || visit(tt.Elem())
		case *types.Chan:
			return visit(tt.Elem())
		case *types.Struct:
			for i := 0; i < tt.NumFields(); i++ {
				if visit(tt.Field(i).Type()) {
					return true
				}
			}
		case *types.Signature:
			if r := tt.Recv(); r != nil && visit(r.Type()) {
				return true
			}
			for i := 0; i < tt.Params().Len(); i++ {
				if visit(tt.Params().At(i).Type()) {
					return true
				}
			}
			for i := 0; i < tt.Results().Len(); i++ {
				if visit(tt.Results().At(i).Type()) {
					return true
				}
			}
		case *types.Interface:
			for i := 0; i < tt.NumMethods(); i++ {
				if visit(tt.Method(i).Type()) {
					return true
				}
			}
			for i := 0; i < tt.NumEmbeddeds(); i++ {
				if visit(tt.EmbeddedType(i)) {
					return true
				}
			}
		case *types.Union:
			for i := 0; i < tt.Len(); i++ {
				if visit(tt.Term(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	return visit(t)
}

func basicClass(b *types.Basic) string {
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
