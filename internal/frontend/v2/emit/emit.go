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
// An unrepresentable type is an error, never a misdescription: the caller
// aborts emission, so no document with a lied-about type can escape.
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

// describeType describes one type for the type table. Every types.Type
// the checker can produce gets its own kind here; an unrepresentable type
// is an error, never a silent misdescription (the old default arm emitted
// kind "basic" with class "unknown", which the backend can only crash on).
func (e *Emitter) describeType(t types.Type) (map[string]any, error) {
	switch tt := t.(type) {
	case *types.Basic:
		class := basicClass(tt)
		if class == "unknown" {
			// The only basic type with no class is untyped nil: there
			// is no nil kind on the wire and no backend mapping for
			// it. Failing here moves the crash from backend load
			// (GoKind.fromBasic throws on "unknown") to emit time,
			// with the type named.
			return nil, fmt.Errorf("emit: unsupported basic type %q (class \"unknown\"): no backend mapping", tt.Name())
		}
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
		return map[string]any{
			"kind":   "pointer",
			"detail": map[string]any{"elem": elem},
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
			pt, err := e.typeID(p.Type())
			if err != nil {
				return nil, err
			}
			params = append(params, map[string]any{
				"name": p.Name(),
				"type": pt,
			})
		}
		results := []any{}
		for i := 0; i < tt.Results().Len(); i++ {
			r := tt.Results().At(i)
			rt, err := e.typeID(r.Type())
			if err != nil {
				return nil, err
			}
			results = append(results, map[string]any{
				"name": r.Name(),
				"type": rt,
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
		underlying, err := e.typeID(tt.Underlying())
		if err != nil {
			return nil, err
		}
		methods, err := e.describeMethods(tt)
		if err != nil {
			return nil, err
		}
		typeparams, err := e.describeTypeParams(tt.TypeParams())
		if err != nil {
			return nil, err
		}
		typeargs, err := e.describeTypeArgs(tt.TypeArgs())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind": "named",
			"detail": map[string]any{
				"name":       namedName(tt),
				"underlying": underlying,
				"methods":    methods,
				"typeparams": typeparams,
				"typeargs":   typeargs,
			},
		}, nil
	case *types.Alias:
		// Aliases are transparent in Go: `type MyInt = int` makes MyInt
		// *be* int, and `any` *is* interface{}. The entry keeps the
		// alias name and points at the right-hand side, so the backend
		// resolves it exactly like the aliased type. (Without this
		// case aliases fell through to basic/unknown — including `any`,
		// so no program using `any` could load in the backend.)
		rhs, err := e.typeID(tt.Rhs())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind": "alias",
			"detail": map[string]any{
				"name": typeObjectName(tt.Obj()),
				"rhs":  rhs,
			},
		}, nil
	case *types.TypeParam:
		// A type parameter has no runtime value of its own: at runtime
		// it behaves like its constraint (the Java side's
		// GoKind.TYPEPARAM documents the same rule). The entry carries
		// the constraint so the backend can resolve the kind.
		c := tt.Constraint()
		if c == nil {
			return nil, fmt.Errorf("emit: type parameter %s has no constraint", tt.Obj().Name())
		}
		cid, err := e.typeID(c)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind": "typeparam",
			"detail": map[string]any{
				"name":       tt.Obj().Name(),
				"constraint": cid,
				"index":      tt.Index(),
			},
		}, nil
	case *types.Union:
		// A union only occurs inside a constraint interface's type set:
		// the checker rejects a union as a value type, so one can
		// never be a variable's type. It has no runtime value, so the
		// entry records its terms faithfully and the backend rejects
		// any attempt to resolve it to a value kind.
		terms := []any{}
		for i := 0; i < tt.Len(); i++ {
			term := tt.Term(i)
			tid, err := e.typeID(term.Type())
			if err != nil {
				return nil, err
			}
			terms = append(terms, map[string]any{
				"type":  tid,
				"tilde": term.Tilde(),
			})
		}
		return map[string]any{
			"kind":   "union",
			"detail": map[string]any{"terms": terms},
		}, nil
	case *types.Struct:
		fields := []any{}
		for i := 0; i < tt.NumFields(); i++ {
			f := tt.Field(i)
			ft, err := e.typeID(f.Type())
			if err != nil {
				return nil, err
			}
			// No "offset": no v2 target layout is defined (the JVM
			// layout is the backend's), so a hardcoded 0 would be a
			// lie. The field's position in this list is its index;
			// the backend reads fields by name (see NodeFactoryV2).
			fields = append(fields, map[string]any{
				"name":     f.Name(),
				"type":     ft,
				"tag":      tt.Tag(i),
				"embedded": f.Embedded(),
			})
		}
		return map[string]any{
			"kind":   "struct",
			"detail": map[string]any{"fields": fields},
		}, nil
	case *types.Interface:
		methods := []any{}
		for i := 0; i < tt.NumExplicitMethods(); i++ {
			m := tt.ExplicitMethod(i)
			sig, err := e.typeID(m.Type())
			if err != nil {
				return nil, err
			}
			methods = append(methods, map[string]any{
				"name": m.Name(),
				"type": sig,
			})
		}
		embedded := []any{}
		for i := 0; i < tt.NumEmbeddeds(); i++ {
			et, err := e.typeID(tt.EmbeddedType(i))
			if err != nil {
				return nil, err
			}
			embedded = append(embedded, et)
		}
		return map[string]any{
			"kind": "interface",
			"detail": map[string]any{
				"empty":    tt.Empty(),
				"methods":  methods,
				"embedded": embedded,
			},
		}, nil
	case *types.Tuple:
		// go/types gives a call expression the tuple of its results:
		// the empty tuple for a void call, the single result type for
		// a one-result call (never a tuple), the tuple for a
		// multi-result call. The entry records the elements
		// faithfully; the backend maps the empty tuple to INTERFACE
		// (no value — the result is discarded) and rejects a
		// non-empty tuple at kind resolution (no single GoKind
		// exists for it).
		elems := []any{}
		for i := 0; i < tt.Len(); i++ {
			et, err := e.typeID(tt.At(i).Type())
			if err != nil {
				return nil, err
			}
			elems = append(elems, et)
		}
		return map[string]any{
			"kind":   "tuple",
			"detail": map[string]any{"elems": elems},
		}, nil
	default:
		// Every other types.Type fails loudly instead of emitting
		// kind "basic" with class "unknown": a silent misdescription
		// the backend can only crash on.
		return nil, fmt.Errorf("emit: unsupported type %T %s", t, t.String())
	}
}

// describeTypeParams describes a type parameter list as
// [{name, constraint}] — one entry per declared parameter, in order.
// A nil or empty list describes as an empty list, never nil, so the
// shape is stable. Used for Named origins/instantiations (TypeParams
// returns the origin's parameters for an instantiation too).
func (e *Emitter) describeTypeParams(list *types.TypeParamList) ([]any, error) {
	out := []any{}
	for i := 0; i < list.Len(); i++ {
		tp := list.At(i)
		c := tp.Constraint()
		if c == nil {
			return nil, fmt.Errorf("emit: type parameter %s has no constraint", tp.Obj().Name())
		}
		cid, err := e.typeID(c)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"name":       tp.Obj().Name(),
			"constraint": cid,
		})
	}
	return out, nil
}

// describeTypeArgs describes a type argument list as [type ids], in
// order. A nil or empty list describes as an empty list. Used for Named
// instantiations: List[int] and List[string] differ here (and in their
// substituted underlying types).
func (e *Emitter) describeTypeArgs(list *types.TypeList) ([]any, error) {
	out := []any{}
	for i := 0; i < list.Len(); i++ {
		id, err := e.typeID(list.At(i))
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// describeMethods describes the explicitly declared methods of a named
// type as [{name, type}] — the method's signature id, with the receiver
// omitted (it is the owning type). Method(i) gives substituted
// signatures on instantiated types, so List[int].Get returns int.
func (e *Emitter) describeMethods(n *types.Named) ([]any, error) {
	out := []any{}
	for i := 0; i < n.NumMethods(); i++ {
		m := n.Method(i)
		sig, err := e.typeID(m.Type())
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"name": m.Name(),
			"type": sig,
		})
	}
	return out, nil
}

// isNoTypeID reports whether t has no type-table entry: untyped nil
// (a nil literal's type is contextual — *int in `var p *int = nil`,
// interface{} in F(nil)) and invalid (the type go/types assigns to
// builtin function values such as unsafe.Sizeof, which have no
// signature). Both are absences of a type, not types: the object table
// already says kind "nil" for a nil literal, and a builtin selector is
// built by the backend without reading the type id. Callers that
// describe an expression's type therefore record no type id for them
// instead of misdescribing them — describeType's "unknown" rejection
// stays as the backstop for any other unrepresentable basic.
func isNoTypeID(t types.Type) bool {
	b, ok := t.(*types.Basic)
	if !ok {
		return false
	}
	return b.Kind() == types.UntypedNil || b.Kind() == types.Invalid
}

func basicClass(b *types.Basic) string {
	// unsafe.Pointer carries no Info bits (Info 0), so detect it by kind
	// before consulting Info: it is the only basic type GoKind.fromBasic
	// accepts under class "pointer".
	if b.Kind() == types.UnsafePointer {
		return "pointer"
	}
	// Test the Info bits directly. Do NOT mask with IsNumeric first: the
	// mask strips IsBoolean and IsUnsigned, which made the bool and uint
	// arms dead (bool came out "unknown", every unsigned int "int").
	// IsUnsigned must be tested before IsInteger because unsigned types
	// carry both bits. Untyped kinds land in their base class (untyped
	// bool -> "bool", untyped int -> "int"), which is what
	// GoKind.fromBasic accepts; only untyped nil (no other bit set) falls
	// through to "unknown". describeType rejects that class loudly;
	// basicClass itself stays a pure classifier.
	info := b.Info()
	switch {
	case info&types.IsBoolean != 0:
		return "bool"
	case info&types.IsUnsigned != 0:
		return "uint"
	case info&types.IsInteger != 0:
		return "int"
	case info&types.IsFloat != 0:
		return "float"
	case info&types.IsComplex != 0:
		return "complex"
	case info&types.IsString != 0:
		return "string"
	}
	return "unknown"
}

// typeObjectName renders a type object's name the way named types do:
// package-qualified when the object belongs to a package, bare otherwise
// (universe objects like `any` have no package).
func typeObjectName(obj types.Object) string {
	if obj == nil {
		return ""
	}
	if pkg := obj.Pkg(); pkg != nil && pkg.Path() != "" {
		return pkg.Path() + "." + obj.Name()
	}
	return obj.Name()
}

func namedName(n *types.Named) string {
	if obj := n.Obj(); obj != nil {
		return typeObjectName(obj)
	}
	return n.String()
}
