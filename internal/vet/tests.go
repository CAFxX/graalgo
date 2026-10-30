package vet

import (
	"go/ast"
)

func init() {
	register(&Analyzer{
		Name: "tests",
		Doc:  "check for common mistaken usages of tests and examples",
		Run:  runTests,
	})
}

// Common test mistakes:
// - Test function with wrong signature (must be func TestXxx(*testing.T))
// - Example without // Output: comment (not an error, skip)
// - TestMain with wrong signature
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
			name := fn.Name.Name
			// TestMain has its own signature (*testing.M) and is
			// checked by the loop below; the "Test" prefix must not
			// route it into the *testing.T check.
			if name == "TestMain" {
				continue
			}
			if len(name) < 4 || name[:4] != "Test" {
				continue
			}
			// TestXxx must take *testing.T.
			if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
				out = append(out, Diagnostic{
					pass.position(fn),
					"Test functions must take one *testing.T parameter",
				})
				continue
			}
			param := fn.Type.Params.List[0]
			tv, ok := pass.Info.Types[param.Type]
			if !ok || tv.Type == nil || tv.Type.String() != "*testing.T" {
				out = append(out, Diagnostic{
					pass.position(fn),
					"Test functions must take *testing.T",
				})
			}
		}
		// Check TestMain signature.
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "TestMain" {
				continue
			}
			if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
				out = append(out, Diagnostic{
					pass.position(fn),
					"TestMain must take one *testing.M parameter",
				})
				continue
			}
			param := fn.Type.Params.List[0]
			tv, ok := pass.Info.Types[param.Type]
			if !ok || tv.Type == nil || tv.Type.String() != "*testing.M" {
				out = append(out, Diagnostic{
					pass.position(fn),
					"TestMain must take one *testing.M parameter",
				})
			}
		}
	}
	return out
}
