package vet

import (
	"go/ast"
	"go/types"
)

func init() {
	register(&Analyzer{
		Name: "composites",
		Doc:  "check for unkeyed composite literals",
		Run:  runComposites,
	})
}

// Unkeyed composite literals like T{1, 2} break when fields are added
// or reordered. go vet flags them for imported types (and optionally
// local types with -compositeswhitelist=false; we flag imported only,
// matching the default).
func runComposites(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || len(lit.Elts) == 0 {
				return true
			}
			// Skip if any element is keyed.
			for _, e := range lit.Elts {
				if _, ok := e.(*ast.KeyValueExpr); ok {
					return true
				}
			}
			// Unkeyed: check if the type is from another package.
			tv, ok := pass.Info.Types[lit]
			if !ok || tv.Type == nil {
				return true
			}
			t := tv.Type
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem()
			}
			named, ok := t.(*types.Named)
			if !ok || named.Obj().Pkg() == nil {
				return true
			}
			// Only flag imported types (default go vet behavior).
			if pass.Pkg != nil && named.Obj().Pkg() == pass.Pkg {
				return true
			}
			// Skip struct types with no fields or all blank? No — flag it.
			if _, ok := named.Underlying().(*types.Struct); ok {
				out = append(out, Diagnostic{
					pass.position(lit),
					"composite literal uses unkeyed fields",
				})
			}
			return true
		})
	}
	return out
}
