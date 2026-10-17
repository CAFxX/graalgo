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
	register(&Analyzer{
		Name: "tests",
		Doc:  "check for common mistaken usages of tests and examples",
		Run:  runTests,
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

// Common test mistakes:
// - Test function with wrong signature (must be func TestXxx(*testing.T))
// - TestMain with wrong signature (must be func TestMain(*testing.M))
// - Example without // Output: comment (not an error, skip)
func runTests(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		// Only check _test.go files.
		pos := pass.Fset.Position(f.Package)
		if len(pos.Filename) < 8 || pos.Filename[len(pos.Filename)-8:] != "_test.go" {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			// TestMain is checked separately below: it must not be
			// swept up by the Test* loop (it takes *testing.M, not
			// *testing.T).
			if fn.Name.Name == "TestMain" {
				out = append(out, checkTestMain(pass, fn)...)
				continue
			}
			if name := fn.Name.Name; len(name) >= 4 && name[:4] == "Test" {
				out = append(out, checkTestFunc(pass, fn)...)
			}
		}
	}
	return out
}

// checkTestFunc verifies a TestXxx function has the signature
// func TestXxx(*testing.T).
func checkTestFunc(pass *Pass, fn *ast.FuncDecl) []Diagnostic {
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return []Diagnostic{{
			pass.position(fn),
			"Test functions must take one *testing.T parameter",
		}}
	}
	param := fn.Type.Params.List[0]
	tv, ok := pass.Info.Types[param.Type]
	if !ok || tv.Type == nil {
		// The parameter type did not resolve (its import failed, which
		// Run already reported as a diagnostic). Treat it as unknown
		// rather than as a violation: do not flag what cannot be
		// verified (C2/C3).
		return nil
	}
	if !isPointerToNamed(tv.Type, "testing", "T") {
		return []Diagnostic{{
			pass.position(fn),
			"Test functions must take *testing.T",
		}}
	}
	return nil
}

// checkTestMain verifies the signature func TestMain(m *testing.M) with
// no results. As with checkTestFunc, an unresolvable parameter type is
// treated as unknown rather than as a violation.
func checkTestMain(pass *Pass, fn *ast.FuncDecl) []Diagnostic {
	var out []Diagnostic
	paramBad := true
	if fn.Type.Params != nil && len(fn.Type.Params.List) == 1 {
		tv, ok := pass.Info.Types[fn.Type.Params.List[0].Type]
		switch {
		case !ok || tv.Type == nil:
			// Unresolvable: cannot verify; the failed import is
			// already reported by Run. Do not flag.
			paramBad = false
		case isPointerToNamed(tv.Type, "testing", "M"):
			paramBad = false
		}
	}
	if paramBad {
		out = append(out, Diagnostic{
			pass.position(fn),
			"TestMain must take one *testing.M parameter",
		})
	}
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		out = append(out, Diagnostic{
			pass.position(fn),
			"TestMain must not return results",
		})
	}
	return out
}
