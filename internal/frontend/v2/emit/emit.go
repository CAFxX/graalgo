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
	var funcs []any
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			obj := e.info.Info.Defs[fd.Name]
			if obj == nil {
				return nil, fmt.Errorf("emit: no object for func %s", fd.Name.Name)
			}
			e.declareObject(obj)
		}
	}

	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			fn, err := e.emitFuncDecl(fd)
			if err != nil {
				return nil, err
			}
			funcs = append(funcs, fn)
		}
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
		Globals: []any{},
	}, nil
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

// sizes is the target data model used for struct field offsets.
//
// The backend (GraalVM/Truffle on a 64-bit JVM) models Go values with the
// gc/amd64 layout — the same model the v1 frontend uses
// (internal/frontend/export.go: `types.SizesFor("gc", "amd64")`) for the
// identical "offset" field consumed by the backend's reflect support
// (internal/backend/interop/.../p3reflect/ReflectTypes.java reads
// `f.longInt("offset", path)`). Both frontends must describe one backend
// memory model, so v2 emits offsets under the same sizes. (The v2 checker
// leaves types.Config.Sizes unset, so go/types defaults to the host's gc
// sizes — gc/amd64 on every supported build host — consistent with the
// explicit choice here.)
var sizes types.Sizes = types.SizesFor("gc", "amd64")

// unknownOffset marks a struct field whose byte offset cannot be computed
// statically: some field type mentions a type parameter (go/types panics
// on those — see sizesWouldPanic), so the layout is
// instantiation-dependent. The backend must index such fields by
// position, never by byte offset — the same convention as v1.
const unknownOffset = -1

// structOffsets returns one byte offset per field under the target sizes,
// or unknownOffset for every field when any field type would make
// go/types panic (type parameters). Structural, never name-keyed.
func structOffsets(vars []*types.Var) []int64 {
	offsets := make([]int64, len(vars))
	for _, v := range vars {
		if sizesWouldPanic(v.Type()) {
			for i := range offsets {
				offsets[i] = unknownOffset
			}
			return offsets
		}
	}
	return sizes.Offsetsof(vars)
}

// sizesWouldPanic reports whether asking go/types for the size/alignment of
// t would panic: gcSizes asserts on bare type parameters, which are reached
// by descending through array element types, struct field types, and named
// type underlyings. (Slice/map/pointer/chan positions are fine — gcSizes
// returns WordSize without descending; signatures are word-sized too and
// are never descended into.)
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

// methodEntries describes the method set for dispatch: one entry per
// method, in go/types method-set order (sorted by name), carrying the
// method name and the type-table id of its signature. Names + signatures
// are the dispatch data; there are no wire-only implementation keys and
// no backend special cases.
func (e *Emitter) methodEntries(ms *types.MethodSet) []any {
	entries := make([]any, 0, ms.Len())
	for i := 0; i < ms.Len(); i++ {
		// go/types guarantees *types.Func for method-set selections;
		// the assertion fails loud if that ever stops holding.
		mobj := ms.At(i).Obj().(*types.Func)
		entries = append(entries, map[string]any{
			"name": mobj.Name(),
			"sig":  e.typeID(mobj.Type()),
		})
	}
	return entries
}

func (e *Emitter) describeType(t types.Type) map[string]any {
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
				// Full method set (declared + promoted through embedded
				// fields): this is the dispatch table's key set.
				"methods": e.methodEntries(types.NewMethodSet(tt)),
			},
		}
	case *types.Struct:
		fields := []any{}
		vars := make([]*types.Var, tt.NumFields())
		for i := 0; i < tt.NumFields(); i++ {
			vars[i] = tt.Field(i)
		}
		offsets := structOffsets(vars)
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
	case *types.Interface:
		methods := []any{}
		for i := 0; i < tt.NumMethods(); i++ {
			m := tt.Method(i)
			methods = append(methods, map[string]any{
				"name": m.Name(),
				"sig":  e.typeID(m.Type()),
			})
		}
		// Embedded interfaces travel as type-table references; the
		// backend unions their method sets with the explicit methods
		// above — the same model as v1's "embedded" list.
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
