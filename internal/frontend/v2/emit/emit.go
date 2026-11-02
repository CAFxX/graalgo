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
	// (assigning object ids), then emit bodies and global declarations.
	// A global initializer may reference a later global or function, so
	// declaring everything first is what makes those references resolve.
	//
	// Deterministic emission order (V2-12): pkg.Files is a Go map, so
	// ranging over it directly would make function order and type/object
	// ids vary run to run. Files are visited sorted by name and
	// declarations sorted by source position, so the same package always
	// produces the same document — a prerequisite for reproducible builds
	// and snapshot testing.
	decls := sortedTopDecls(pkg)

	for _, d := range decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			obj := e.info.Info.Defs[decl.Name]
			if obj == nil {
				return nil, fmt.Errorf("emit: no object for func %s", decl.Name.Name)
			}
			if _, err := e.declareObject(obj); err != nil {
				return nil, err
			}
		case *ast.GenDecl:
			if err := e.declareTopGenDecl(decl); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("emit: unsupported top-level decl %T", d)
		}
	}

	var funcs []any
	globals := []any{}
	for _, d := range decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			fn, err := e.emitFuncDecl(decl)
			if err != nil {
				return nil, err
			}
			funcs = append(funcs, fn)
		case *ast.GenDecl:
			gs, err := e.emitTopGenDecl(decl)
			if err != nil {
				return nil, err
			}
			globals = append(globals, gs...)
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
		Globals: globals,
	}, nil
}

// sortedTopDecls returns every top-level declaration in the package in
// deterministic order: files sorted by name, then declarations sorted by
// source position (so interleaved funcs and GenDecls keep source order).
// Sorting is structural — by filename and token position — never keyed on
// any package, symbol, or test name.
func sortedTopDecls(pkg *ast.Package) []ast.Decl {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	var decls []ast.Decl
	for _, name := range names {
		file := pkg.Files[name]
		ds := append([]ast.Decl(nil), file.Decls...)
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

// declareParam assigns an object id for a function-scope variable
// (parameter, result, or receiver).
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
		// Roll back the reservation so a failed declaration leaves no
		// nil hole in the object table.
		delete(e.objIDs, obj)
		e.objects = e.objects[:len(e.objects)-1]
		return 0, err
	}
	e.objects[id] = desc
	return id, nil
}

// describeObject describes one go/types object. Every object kind
// go/types can produce has an explicit case; anything else is an error,
// never a mislabelled "var" — a constant, type name, or package name
// described as a variable would silently corrupt downstream dispatch.
func (e *Emitter) describeObject(obj types.Object, isParam bool) (map[string]any, error) {
	// Not every object kind has a type: package names and labels do not.
	// typeID(nil) would panic, so only resolve the type when one exists.
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
		}, nil
	case *types.Func:
		return map[string]any{
			"kind": "func",
			"name": o.Name(),
			"type": typ,
		}, nil
	case *types.Const:
		m := map[string]any{
			"kind": "const",
			"name": o.Name(),
			"type": typ,
		}
		if o.Val() != nil {
			m["value"] = o.Val().ExactString()
		}
		return m, nil
	case *types.TypeName:
		return map[string]any{
			"kind": "type",
			"name": o.Name(),
			"type": typ,
		}, nil
	case *types.PkgName:
		m := map[string]any{
			"kind": "package",
			"name": o.Name(),
			"type": typ,
		}
		if o.Imported() != nil {
			m["path"] = o.Imported().Path()
		}
		return m, nil
	case *types.Label:
		return map[string]any{
			"kind": "label",
			"name": o.Name(),
			"type": typ,
		}, nil
	case *types.Nil:
		return map[string]any{
			"kind": "nil",
			"name": o.Name(),
			"type": typ,
		}, nil
	case *types.Builtin:
		return map[string]any{
			"kind": "builtin",
			"name": o.Name(),
			"type": typ,
		}, nil
	default:
		// Defensive only: go1.27's types.Object carries unexported
		// methods (order/setType/setOrder/setParent/sameId), so no
		// implementation outside go/types can reach this branch — but
		// if a future toolchain ever opens the interface, an unknown
		// kind must fail here rather than be mislabelled "var".
		return nil, fmt.Errorf("emit: unsupported object %T (%s)", obj, obj.Name())
	}
}

// declareMethodObject declares a method *types.Func object for a type
// table entry. describeType has no error return (type ids are assigned
// lazily throughout emission), so this helper is the one place where a
// declaration failure inside type description surfaces. A *types.Func
// always describes successfully — the Func case has no failure mode — so
// an error here means an internal invariant broke, not that user input
// was rejected; failing loudly beats emitting a dangling method entry.
func (e *Emitter) declareMethodObject(obj *types.Func) int {
	id, err := e.declareObject(obj)
	if err != nil {
		panic(fmt.Sprintf("emit: internal error describing method %s: %v", obj.Name(), err))
	}
	return id
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
		// The method set is what a backend dispatches on, so it must be
		// populated, not empty. "methods" is the value method set;
		// "ptrMethods" is the pointer-only remainder: methods callable
		// via *T but not via T, identified precisely by a pointer
		// receiver (value-receiver methods appear in both sets, so
		// they are listed once, under "methods").
		methods := []any{}
		ms := types.NewMethodSet(tt)
		for i := 0; i < ms.Len(); i++ {
			m := ms.At(i)
			fn, ok := m.Obj().(*types.Func)
			if !ok {
				continue
			}
			methods = append(methods, map[string]any{
				"name": fn.Name(),
				"obj":  e.declareMethodObject(fn),
				"sig":  e.typeID(fn.Type()),
			})
		}
		ptrMethods := []any{}
		pms := types.NewMethodSet(types.NewPointer(tt))
		for i := 0; i < pms.Len(); i++ {
			m := pms.At(i)
			fn, ok := m.Obj().(*types.Func)
			if !ok {
				continue
			}
			sig, ok := fn.Type().(*types.Signature)
			if !ok || sig.Recv() == nil {
				continue
			}
			if _, isPtr := sig.Recv().Type().(*types.Pointer); !isPtr {
				continue
			}
			ptrMethods = append(ptrMethods, map[string]any{
				"name": fn.Name(),
				"obj":  e.declareMethodObject(fn),
				"sig":  e.typeID(fn.Type()),
			})
		}
		return map[string]any{
			"kind": "named",
			"detail": map[string]any{
				"name":       namedName(tt),
				"underlying": e.typeID(tt.Underlying()),
				"methods":    methods,
				"ptrMethods": ptrMethods,
			},
		}
	case *types.Struct:
		fields := []any{}
		for i := 0; i < tt.NumFields(); i++ {
			f := tt.Field(i)
			fields = append(fields, map[string]any{
				"name":     f.Name(),
				"type":     e.typeID(f.Type()),
				"tag":      tt.Tag(i),
				"embedded": f.Embedded(),
				"offset":   0,
			})
		}
		return map[string]any{
			"kind":   "struct",
			"detail": map[string]any{"fields": fields},
		}
	case *types.Interface:
		// Explicit methods only under "methods"; embedded interfaces
		// (and type-set elements) under "embedded", so a backend can
		// tell what this interface declares from what it inherits.
		// "complete" is tt.IsMethodSet(): true exactly when the
		// method list is the whole interface. It is false for any
		// interface carrying type-set restrictions — a union term
		// (~int | ~string), or an embedded constraint interface —
		// even after successful checking: probed constraint
		// interfaces report complete:false from a fully checked
		// package, so false never means "still under construction".
		// Backend rule for complete:false: the "methods" list is NOT
		// the full contract — consult "embedded" (unions and embedded
		// constraints) to decide which types satisfy the interface;
		// method-set-only dispatch is wrong for it.
		methods := []any{}
		for i := 0; i < tt.NumExplicitMethods(); i++ {
			m := tt.ExplicitMethod(i)
			methods = append(methods, map[string]any{
				"name": m.Name(),
				"obj":  e.declareMethodObject(m),
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
				"complete": tt.IsMethodSet(),
			},
		}
	case *types.Union:
		// A type set, never a scalar: each term carries its type and
		// whether it admits underlying types (tilde). Labelling a
		// union "basic" would let a backend dispatch on kind and
		// treat ~int | ~string as a single scalar type.
		terms := []any{}
		for i := 0; i < tt.Len(); i++ {
			term := tt.Term(i)
			terms = append(terms, map[string]any{
				"type":  e.typeID(term.Type()),
				"tilde": term.Tilde(),
			})
		}
		return map[string]any{
			"kind":   "union",
			"detail": map[string]any{"terms": terms},
		}
	case *types.TypeParam:
		// A generic type parameter (e.g. the T in struct{ V T } or a
		// constraint bound). Its constraint is itself a type (usually
		// an interface) in this table.
		var constraint any
		if tt.Constraint() != nil {
			constraint = e.typeID(tt.Constraint())
		}
		name := ""
		if tt.Obj() != nil {
			name = tt.Obj().Name()
		}
		return map[string]any{
			"kind": "typeparam",
			"detail": map[string]any{
				"name":       name,
				"constraint": constraint,
				"index":      tt.Index(),
			},
		}
	case *types.Alias:
		// An alias (type A = int) is not its RHS: it has its own
		// identity (its object name) and a right-hand-side type.
		// go1.27 materialises aliases as *types.Alias; describing one
		// as "basic" would erase the alias and misdirect any consumer
		// resolving by name.
		var rhs any
		if tt.Rhs() != nil {
			rhs = e.typeID(tt.Rhs())
		}
		args := []any{}
		if tt.TypeArgs() != nil {
			for i := 0; i < tt.TypeArgs().Len(); i++ {
				args = append(args, e.typeID(tt.TypeArgs().At(i)))
			}
		}
		return map[string]any{
			"kind": "alias",
			"detail": map[string]any{
				"name": tt.Obj().Name(),
				"rhs":  rhs,
				"args": args,
			},
		}
	default:
		// No type may be mislabelled "basic": every go/types type kind
		// has an explicit case above, so reaching here means a future
		// toolchain added a kind. Emit it as "unknown" — a distinct
		// kind a backend must reject explicitly — never as a scalar
		// it is not.
		return map[string]any{
			"kind":   "unknown",
			"detail": map[string]any{"name": t.String(), "class": "unknown"},
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
