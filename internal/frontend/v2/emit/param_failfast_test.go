// L11 regression test: emitFuncDecl must fail loudly when a parameter's
// type-system definition is missing, never silently emit a function with
// fewer parameters than its signature.
//
// In a well-typed package go/types always records every parameter in
// Info.Defs, so the nil branch is defensive. But if it ever triggers
// (corrupt checker result, go/types bug), the old code silently continued,
// producing a funcdecl whose params list was shorter than the signature
// the type system declared — a corrupt function that the Java backend
// would then misbind or reject far from the cause.
package emit

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
)

// buildParamlessInfoEmitter constructs an Emitter whose type info records
// the function itself but deliberately omits the definition of parameter
// x — the corrupt-state case that must fail loudly.
func buildParamlessInfoEmitter() (*Emitter, *ast.FuncDecl) {
	fset := token.NewFileSet()
	x := ast.NewIdent("x")
	fd := &ast.FuncDecl{
		Name: ast.NewIdent("f"),
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: []*ast.Field{
					{Names: []*ast.Ident{x}, Type: ast.NewIdent("int")},
				},
			},
		},
		Body: &ast.BlockStmt{},
	}
	sig := types.NewSignatureType(
		nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "x", types.Typ[types.Int])),
		nil, false,
	)
	fn := types.NewFunc(token.NoPos, nil, "f", sig)
	info := &types.Info{Defs: map[*ast.Ident]types.Object{
		fd.Name: fn, // function recorded; param x deliberately missing
	}}
	cr := &checker.Result{Info: info, Fset: fset, Pkg: types.NewPackage("main", "main")}
	e := &Emitter{
		fset:    fset,
		info:    cr,
		typeIDs: make(map[types.Type]int),
		objIDs:  make(map[types.Object]int),
	}
	return e, fd
}

func TestEmitFuncDeclMissingParamDefFailsLoudly(t *testing.T) {
	e, fd := buildParamlessInfoEmitter()
	doc, err := e.emitFuncDecl(fd)
	if err == nil {
		params := doc["fields"].(map[string]any)["params"].([]any)
		t.Fatalf("expected loud failure for param with missing Defs entry, "+
			"got success with %d emitted params (silent param drop)", len(params))
	}
	if !strings.Contains(err.Error(), "no object for param") {
		t.Fatalf("error %q must name the missing param definition", err.Error())
	}
	if !strings.Contains(err.Error(), "x") || !strings.Contains(err.Error(), "f") {
		t.Fatalf("error %q must identify the param and the function", err.Error())
	}
}
