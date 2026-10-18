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
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math"
	"math/big"
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
	decls := sortedFuncDecls(pkg)

	var funcs []any
	for _, fd := range decls {
		obj := e.info.Info.Defs[fd.Name]
		if obj == nil {
			return nil, fmt.Errorf("emit: no object for func %s", fd.Name.Name)
		}
		if _, err := e.declareObject(obj); err != nil {
			return nil, err
		}
	}

	for _, fd := range decls {
		fn, err := e.emitFuncDecl(fd)
		if err != nil {
			return nil, err
		}
		funcs = append(funcs, fn)
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

// sortedFuncDecls returns every top-level function declaration in the
// package in deterministic order: files sorted by name, then declarations
// sorted by source position. Non-function declarations are skipped, as in
// the original emission loop. Sorting is structural — by filename and
// token position — never keyed on any package, symbol, or test name.
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

// describeObject describes a types.Object for the object table.
//
// The "kind" is the object's true go/types kind — never a fallback:
//   - *types.Var → "var" (with "param" for function parameters)
//   - *types.Func → "func"
//   - *types.Const → "const" (with the constant's "value"; see describeConst)
//   - *types.TypeName → "typename"
//   - *types.Nil → "nil"
//
// The backend switches on this kind: constants become ConstNodes (they are
// immutable values, not frame slots), nil becomes a NilNode, and type names
// only ever appear as the target of "conversion" nodes — never as callable
// "var" objects. Object kinds the emitter cannot meaningfully describe
// (*types.PkgName, *types.Label, *types.Builtin) are loud errors: reaching
// here with one means an emitter bug (e.g. a bare package name used as a
// value, which the checker should already have rejected).
func (e *Emitter) describeObject(obj types.Object, isParam bool) (map[string]any, error) {
	// Reject undescribable object kinds BEFORE touching obj.Type():
	// *types.PkgName.Type(), *types.Label.Type() and *types.Builtin.Type()
	// are nil, so describing them as anything would be a lie built on a
	// panic. Reaching here with one means an emitter bug.
	switch obj.(type) {
	case *types.Var, *types.Func, *types.Const, *types.TypeName, *types.Nil:
	default:
		return nil, fmt.Errorf("emit: unsupported object kind %T for %q", obj, obj.Name())
	}
	typ := e.typeID(obj.Type())
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
		val, err := describeConst(o.Val())
		if err != nil {
			return nil, fmt.Errorf("emit: const %s: %v", o.Name(), err)
		}
		return map[string]any{
			"kind":  "const",
			"name":  o.Name(),
			"type":  typ,
			"value": val,
		}, nil
	case *types.TypeName:
		return map[string]any{
			"kind": "typename",
			"name": o.Name(),
			"type": typ,
		}, nil
	case *types.Nil:
		return map[string]any{
			"kind": "nil",
			"name": o.Name(),
			"type": typ,
		}, nil
	default:
		// Unreachable: the guard above rejects everything else.
		return nil, fmt.Errorf("emit: unsupported object kind %T for %q", obj, obj.Name())
	}
}

// describeConst describes a go/constant.Value for a "const" object entry.
// The encoding mirrors the v1 exporter's constant payloads
// (internal/frontend/export.go foldedConst): integer constants carry the
// low 64 bits ("bits") plus the exact decimal ("exact", for untyped
// constants wider than 64 bits); float constants carry the correctly-rounded
// IEEE bit patterns for both widths ("f32bits"/"f64bits", computed with
// go/constant itself — the same rounding go/types uses); string constants
// carry the exact bytes hex-encoded ("sbytes", since ExactString is a
// quoted Go literal that is lossy to unquote by hand); complex constants
// carry the real/imag parts as bit patterns ("creal32"/"creal64"/
// "cimag32"/"cimag64"). All integers are int64 on the Go side; the v2
// binary encoder zigzag-encodes every integer width, so the Java side
// recovers the exact bit patterns.
func describeConst(v constant.Value) (map[string]any, error) {
	switch v.Kind() {
	case constant.Int:
		return map[string]any{
			"ckind": "int",
			"bits":  constIntBits(v),
			"exact": v.ExactString(),
		}, nil
	case constant.Float:
		f32, _ := constant.Float32Val(v)
		f64, _ := constant.Float64Val(v)
		return map[string]any{
			"ckind":   "float",
			"f32bits": int64(int32(math.Float32bits(f32))),
			"f64bits": int64(math.Float64bits(f64)),
			"exact":   v.ExactString(),
		}, nil
	case constant.Bool:
		return map[string]any{
			"ckind": "bool",
			"value": constant.BoolVal(v),
		}, nil
	case constant.String:
		return map[string]any{
			"ckind":  "string",
			"sbytes": hex.EncodeToString([]byte(constant.StringVal(v))),
		}, nil
	case constant.Complex:
		real, imag := constant.Real(v), constant.Imag(v)
		r32, _ := constant.Float32Val(real)
		r64, _ := constant.Float64Val(real)
		i32, _ := constant.Float32Val(imag)
		i64, _ := constant.Float64Val(imag)
		return map[string]any{
			"ckind":   "complex",
			"creal32": int64(int32(math.Float32bits(r32))),
			"creal64": int64(math.Float64bits(r64)),
			"cimag32": int64(int32(math.Float32bits(i32))),
			"cimag64": int64(math.Float64bits(i64)),
		}, nil
	default:
		return nil, fmt.Errorf("unknown constant kind %v", v.Kind())
	}
}

// constIntBits returns the low 64 bits of an integer constant, two's
// complement. go/types guarantees typed constants fit their type, so the
// exact path covers them; untyped constants wider than 64 bits (e.g. 1<<70)
// are observed through the "exact" decimal instead — the low bits here are
// only a carrier for ConstNode.ofUntypedInt.
func constIntBits(v constant.Value) int64 {
	if i, ok := constant.Int64Val(v); ok {
		return i
	}
	if u, ok := constant.Uint64Val(v); ok {
		return int64(u)
	}
	bi, ok := new(big.Int).SetString(v.ExactString(), 10)
	if !ok {
		return 0 // unreachable: Kind() == Int implies a decimal ExactString
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	return new(big.Int).And(bi, mask).Int64()
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
		return map[string]any{
			"kind": "named",
			"detail": map[string]any{
				"name":       namedName(tt),
				"underlying": e.typeID(tt.Underlying()),
				"methods":    []any{},
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
		return map[string]any{
			"kind": "interface",
			"detail": map[string]any{
				"empty":    tt.Empty(),
				"methods":  []any{},
				"embedded": []any{},
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
	// NOTE: the cases test individual info bits — NOT b.Info() &
	// types.IsNumeric. Masking with IsNumeric first makes the IsBoolean
	// and IsUnsigned cases dead code: IsBoolean is not part of IsNumeric,
	// so typed bool fell through to "unknown", and uints (whose masked
	// info equals IsInteger) were misclassified as "int", which
	// GoKind.fromBasic then rejects by name. Every class returned here
	// must be one GoKind.fromBasic accepts ("int", "uint", "float",
	// "bool", "string", "complex", "pointer", "untyped", "unknown").
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
	if info&types.IsUntyped != 0 {
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
