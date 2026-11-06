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
	// sizes is the target size model (gc/amd64, pinned by the checker)
	// used for struct field offsets.
	sizes types.Sizes
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
	sizes := cr.Sizes
	if sizes == nil {
		// Defensive: checker.Check always pins Sizes, but a hand-built
		// Result must not silently compute offsets under a different
		// model (or the zero model). Fail to the pinned target.
		sizes = types.SizesFor("gc", "amd64")
	}
	e := &Emitter{
		fset:    cr.Fset,
		info:    cr,
		sizes:   sizes,
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
	decls := sortedTopDecls(pkg)

	// Pass 1: declare all top-level funcs and globals (assigning object
	// ids) so bodies and initializers can reference them.
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			obj := e.info.Info.Defs[d.Name]
			if obj == nil {
				return nil, fmt.Errorf("emit: no object for func %s", d.Name.Name)
			}
			if _, err := e.declareObject(obj); err != nil {
				return nil, err
			}
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					return nil, fmt.Errorf("emit: unsupported var spec %T", spec)
				}
				for _, name := range vs.Names {
					if name.Name == "_" {
						continue
					}
					obj := e.info.Info.Defs[name]
					if obj == nil {
						return nil, fmt.Errorf("emit: no object for global %s", name.Name)
					}
					if _, err := e.declareObject(obj); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	var funcs []any
	for _, d := range decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		fn, err := e.emitFuncDecl(fd)
		if err != nil {
			return nil, err
		}
		funcs = append(funcs, fn)
	}

	globals, err := e.emitGlobals(decls)
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

// sortedTopDecls returns every top-level declaration in the package in
// deterministic order: files sorted by name, then declarations sorted by
// source position. Non-function declarations are no longer skipped: the
// emitter needs top-level var declarations for Globals, and methods are
// FuncDecls like any other function. Sorting is structural — by filename
// and token position — never keyed on any package, symbol, or test name.
func sortedTopDecls(pkg *ast.Package) []ast.Decl {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	var decls []ast.Decl
	for _, name := range names {
		file := pkg.Files[name]
		ds := make([]ast.Decl, len(file.Decls))
		copy(ds, file.Decls)
		// Positions from one *ast.File share the same FileSet base, so
		// token.Pos comparison is a total source-position order within the
		// file. go/parser already produces Decls in source order; this sort
		// states the contract explicitly instead of relying on that
		// implementation detail.
		sort.SliceStable(ds, func(i, j int) bool { return ds[i].Pos() < ds[j].Pos() })
		decls = append(decls, ds...)
	}
	return decls
}

// emitGlobals emits one vardecl node per top-level var ValueSpec, in
// deterministic (file, position) order. Package-level vars are real
// definitions: a backend reading Globals must see them, with their
// initializers, or references to them would dangle (objects 26/27 with
// no definition anywhere, as the review found). Top-level const and type
// declarations need no Globals entry: consts are fully described in the
// object table (kind const, with value) and named types in the type
// table, both on demand when referenced.
func (e *Emitter) emitGlobals(decls []ast.Decl) ([]any, error) {
	globals := []any{}
	for _, d := range decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				return nil, fmt.Errorf("emit: unsupported var spec %T", spec)
			}
			n, err := e.emitValueSpec(vs)
			if err != nil {
				return nil, err
			}
			globals = append(globals, n)
		}
	}
	return globals, nil
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
func (e *Emitter) declareObject(obj types.Object) (int, error) {
	return e.declareObjectParam(obj, false)
}

// declareParam assigns an object id for a function parameter.
func (e *Emitter) declareParam(obj types.Object) (int, error) {
	return e.declareObjectParam(obj, true)
}

func (e *Emitter) declareObjectParam(obj types.Object, isParam bool) (int, error) {
	if id, ok := e.objIDs[obj]; ok {
		return id, nil
	}
	id := len(e.objects)
	e.objIDs[obj] = id
	// Placeholder; filled by describeObject below. Registered before
	// description so recursive references work.
	e.objects = append(e.objects, nil)
	desc, err := e.describeObject(obj, isParam)
	if err != nil {
		return 0, err
	}
	e.objects[id] = desc
	return id, nil
}

// describeObject describes one object for the object table. Every object
// kind the checker can produce gets its own kind here: a const, a type
// name, or a builtin emitted as "var" is a wrong program, not a missing
// feature (math.Pi, true, and the conversion type int(u) all came out as
// vars). An object kind with no case fails loudly instead of silently
// mis-describing.
func (e *Emitter) describeObject(obj types.Object, isParam bool) (map[string]any, error) {
	switch o := obj.(type) {
	case *types.Var:
		tid, err := e.typeID(o.Type())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":  "var",
			"name":  o.Name(),
			"type":  tid,
			"param": isParam,
		}, nil
	case *types.Func:
		tid, err := e.typeID(o.Type())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind": "func",
			"name": o.Name(),
			"type": tid,
		}, nil
	case *types.Const:
		tid, err := e.typeID(o.Type())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":  "const",
			"name":  o.Name(),
			"type":  tid,
			"value": o.Val().ExactString(),
		}, nil
	case *types.TypeName:
		tid, err := e.typeID(o.Type())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind": "typename",
			"name": o.Name(),
			"type": tid,
		}, nil
	case *types.Builtin:
		// Builtins have no meaningful single type (len, unsafe.Sizeof);
		// the name is the whole description.
		return map[string]any{
			"kind": "builtin",
			"name": o.Name(),
		}, nil
	case *types.Nil:
		return map[string]any{
			"kind": "nil",
			"name": o.Name(),
		}, nil
	default:
		return nil, fmt.Errorf("emit: unsupported object %T %s", obj, obj.Name())
	}
}

// typeID assigns a type-table id, registering before describing (recursion).
// Describing a type can fail (method tables declare objects), so the error
// propagates: a type that cannot be described is a compile error, never a
// silently degraded entry.
func (e *Emitter) typeID(t types.Type) (int, error) {
	if id, ok := e.typeIDs[t]; ok {
		return id, nil
	}
	id := len(e.types)
	e.typeIDs[t] = id
	e.types = append(e.types, nil) // placeholder for recursion
	desc, err := e.describeType(t)
	if err != nil {
		return 0, err
	}
	e.types[id] = desc
	return id, nil
}

// describeType describes one type for the type table. Method sets are
// populated, never left empty as a placeholder: a named, pointer, struct,
// or interface type carries the method set of exactly that type (see
// methods.go and docs/arch/frontend-v2-method-dispatch.md).
func (e *Emitter) describeType(t types.Type) (map[string]any, error) {
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
		}, nil
	case *types.Pointer:
		elem, err := e.typeID(tt.Elem())
		if err != nil {
			return nil, err
		}
		methods, err := e.methodTableEntries(tt)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":   "pointer",
			"detail": map[string]any{"elem": elem, "methods": methods},
		}, nil
	case *types.Slice:
		elem, err := e.typeID(tt.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":   "slice",
			"detail": map[string]any{"elem": elem},
		}, nil
	case *types.Array:
		elem, err := e.typeID(tt.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":   "array",
			"detail": map[string]any{"len": tt.Len(), "elem": elem},
		}, nil
	case *types.Map:
		key, err := e.typeID(tt.Key())
		if err != nil {
			return nil, err
		}
		elem, err := e.typeID(tt.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind": "map",
			"detail": map[string]any{
				"key":  key,
				"elem": elem,
			},
		}, nil
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
		elem, err := e.typeID(tt.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":   "chan",
			"detail": map[string]any{"dir": dir, "elem": elem},
		}, nil
	case *types.Signature:
		params := []any{}
		for i := 0; i < tt.Params().Len(); i++ {
			p := tt.Params().At(i)
			tid, err := e.typeID(p.Type())
			if err != nil {
				return nil, err
			}
			params = append(params, map[string]any{
				"name": p.Name(),
				"type": tid,
			})
		}
		results := []any{}
		for i := 0; i < tt.Results().Len(); i++ {
			r := tt.Results().At(i)
			tid, err := e.typeID(r.Type())
			if err != nil {
				return nil, err
			}
			results = append(results, map[string]any{
				"name": r.Name(),
				"type": tid,
			})
		}
		return map[string]any{
			"kind": "signature",
			"detail": map[string]any{
				"params":   params,
				"results":  results,
				"variadic": tt.Variadic(),
			},
		}, nil
	case *types.Named:
		under, err := e.typeID(tt.Underlying())
		if err != nil {
			return nil, err
		}
		methods, err := e.methodTableEntries(tt)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind": "named",
			"detail": map[string]any{
				"name":       namedName(tt),
				"underlying": under,
				"methods":    methods,
			},
		}, nil
	case *types.Struct:
		fields := []any{}
		offsets := e.structOffsets(tt)
		for i := 0; i < tt.NumFields(); i++ {
			f := tt.Field(i)
			tid, err := e.typeID(f.Type())
			if err != nil {
				return nil, err
			}
			fields = append(fields, map[string]any{
				"name":     f.Name(),
				"type":     tid,
				"tag":      tt.Tag(i),
				"embedded": f.Embedded(),
				"offset":   offsets[i],
			})
		}
		methods, err := e.methodTableEntries(tt)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":   "struct",
			"detail": map[string]any{"fields": fields, "methods": methods},
		}, nil
	case *types.Interface:
		methods := []any{}
		for i := 0; i < tt.NumMethods(); i++ {
			m := tt.Method(i)
			msig, ok := m.Type().(*types.Signature)
			if !ok {
				return nil, fmt.Errorf("emit: interface method %s has non-signature type %T", m.Name(), m.Type())
			}
			sigID, err := e.typeID(m.Type())
			if err != nil {
				return nil, err
			}
			methods = append(methods, map[string]any{
				"name":    m.Name(),
				"sig":     sigID,
				"implkey": implKeyOf(m, msig),
			})
		}
		embedded := []any{}
		for i := 0; i < tt.NumEmbeddeds(); i++ {
			tid, err := e.typeID(tt.EmbeddedType(i))
			if err != nil {
				return nil, err
			}
			embedded = append(embedded, tid)
		}
		return map[string]any{
			"kind": "interface",
			"detail": map[string]any{
				"empty":    tt.Empty(),
				"methods":  methods,
				"embedded": embedded,
			},
		}, nil
	default:
		return map[string]any{
			"kind":   "basic",
			"detail": map[string]any{"name": t.String(), "class": "unknown", "untyped": false},
		}, nil
	}
}

// structOffsets returns the gc/amd64 byte offset of each field of tt, or
// -1 ("unknown") for every field when the offsets cannot be computed:
// a field type containing a type parameter (go/types panics on Sizeof
// there; offsets of a generic struct are instantiation-dependent anyway)
// or a value-nesting depth over maxStructDepth (Sizeof is exponential in
// nesting depth). -1 is never mistaken for 0: the factory indexes fields
// by position and never trusts an offset for layout.
func (e *Emitter) structOffsets(tt *types.Struct) []int64 {
	offsets := make([]int64, tt.NumFields())
	vars := make([]*types.Var, 0, tt.NumFields())
	for i := 0; i < tt.NumFields(); i++ {
		vars = append(vars, tt.Field(i))
	}
	generic := false
	for _, v := range vars {
		if sizesWouldPanic(v.Type()) {
			generic = true
			break
		}
	}
	if generic || structValueDepth(tt, 0) > maxStructDepth {
		for i := range offsets {
			offsets[i] = -1
		}
		return offsets
	}
	return e.sizes.Offsetsof(vars)
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
