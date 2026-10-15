package vet

import (
	"go/ast"
	"go/importer"
	"go/token"
	"go/types"
	"strconv"
)

func init() {
	register(&Analyzer{
		Name: "cgocall",
		Doc:  "detect some violations of the cgo pointer passing rules",
		Run:  runCgoCall,
	})
}

// runCgoCall implements the cgocall analyzer: it looks for code that uses
// cgo to call C code passing values whose types are almost always invalid
// according to the cgo pointer sharing rules. Specifically, it warns about
// attempts to pass a Go chan, map, func, or slice to C, either directly or
// via a pointer, array, or struct.
func runCgoCall(pass *Pass) []Diagnostic {
	// Only cgo files (those importing "C") can contain C calls.
	var cgoFiles []*ast.File
	for _, f := range pass.Files {
		for _, imp := range f.Imports {
			if path, err := strconv.Unquote(imp.Path.Value); err == nil && path == "C" {
				cgoFiles = append(cgoFiles, f)
				break
			}
		}
	}
	if cgoFiles == nil {
		return nil
	}
	// The main type check cannot resolve import "C", so it bails out
	// with no type information for cgo files. Re-typecheck with
	// FakeImportC so the "C" import resolves to a synthetic package and
	// the rest of each file gets usable type information.
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	conf := types.Config{
		Importer:                 importer.Default(),
		FakeImportC:              true,
		DisableUnusedImportCheck: true,
		Error:                    func(error) {}, // ignore errors; best effort
	}
	conf.Check("cgopkg", pass.Fset, pass.Files, info)
	var out []Diagnostic
	for _, f := range cgoFiles {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			// Is this a C.f() call?
			var name string
			fun := call.Fun
			for {
				paren, ok := fun.(*ast.ParenExpr)
				if !ok {
					break
				}
				fun = paren.X
			}
			if sel, ok := fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "C" {
					name = sel.Sel.Name
				}
			}
			if name == "" {
				return true
			}
			// A call to C.CBytes passes a pointer but is always safe.
			if name == "CBytes" {
				return true
			}
			for _, arg := range call.Args {
				if !cgoTypeOK(cgoBaseType(info, arg), make(map[types.Type]bool)) {
					out = append(out, Diagnostic{
						pass.position(arg),
						"possibly passing Go type with embedded pointer to C",
					})
					break
				}
				// Check for passing the address of a bad type.
				a := arg
				if conv, ok := a.(*ast.CallExpr); ok && len(conv.Args) == 1 &&
					cgoIsUnsafePointer(info, conv.Fun) {
					a = conv.Args[0]
				}
				if u, ok := a.(*ast.UnaryExpr); ok && u.Op == token.AND {
					if !cgoTypeOK(cgoBaseType(info, u.X), make(map[types.Type]bool)) {
						out = append(out, Diagnostic{
							pass.position(arg),
							"possibly passing Go type with embedded pointer to C",
						})
						break
					}
				}
			}
			return true
		})
	}
	return out
}

// cgoBaseType looks through type conversions involving unsafe.Pointer to
// find the real type of an argument:
//
//	unsafe.Pointer(x) => x
//	*(*unsafe.Pointer)(unsafe.Pointer(&x)) => x
func cgoBaseType(info *types.Info, arg ast.Expr) types.Type {
	switch arg := arg.(type) {
	case *ast.CallExpr:
		if len(arg.Args) == 1 && cgoIsUnsafePointer(info, arg.Fun) {
			return cgoBaseType(info, arg.Args[0])
		}
	case *ast.StarExpr:
		call, ok := arg.X.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			break
		}
		// Here arg is *f(v).
		t := cgoExprType(info, call.Fun)
		if t == nil {
			break
		}
		ptr, ok := t.Underlying().(*types.Pointer)
		if !ok {
			break
		}
		// Here arg is *(*p)(v)
		elem, ok := ptr.Elem().Underlying().(*types.Basic)
		if !ok || elem.Kind() != types.UnsafePointer {
			break
		}
		// Here arg is *(*unsafe.Pointer)(v)
		call, ok = call.Args[0].(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			break
		}
		// Here arg is *(*unsafe.Pointer)(f(v))
		if !cgoIsUnsafePointer(info, call.Fun) {
			break
		}
		// Here arg is *(*unsafe.Pointer)(unsafe.Pointer(v))
		u, ok := call.Args[0].(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			break
		}
		// Here arg is *(*unsafe.Pointer)(unsafe.Pointer(&v))
		return cgoBaseType(info, u.X)
	}
	return cgoExprType(info, arg)
}

// cgoExprType returns the type of e, consulting Types first and falling
// back to Uses for identifiers that denote declared objects.
func cgoExprType(info *types.Info, e ast.Expr) types.Type {
	if tv, ok := info.Types[e]; ok && tv.Type != nil {
		return tv.Type
	}
	if id, ok := e.(*ast.Ident); ok {
		if obj, ok := info.Uses[id]; ok {
			return obj.Type()
		}
	}
	return nil
}

// cgoTypeOK reports whether the type of arg is OK to pass to a C function
// using cgo. This is not true for Go types with embedded pointers.
// m avoids infinite recursion on recursive types.
func cgoTypeOK(t types.Type, m map[types.Type]bool) bool {
	if t == nil || m[t] {
		return true
	}
	m[t] = true
	switch t := t.Underlying().(type) {
	case *types.Chan, *types.Map, *types.Signature, *types.Slice:
		return false
	case *types.Pointer:
		return cgoTypeOK(t.Elem(), m)
	case *types.Array:
		return cgoTypeOK(t.Elem(), m)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if !cgoTypeOK(t.Field(i).Type(), m) {
				return false
			}
		}
	}
	return true
}

// cgoIsUnsafePointer reports whether e denotes unsafe.Pointer.
func cgoIsUnsafePointer(info *types.Info, e ast.Expr) bool {
	t := cgoExprType(info, e)
	return t != nil && t.Underlying() == types.Typ[types.UnsafePointer]
}
