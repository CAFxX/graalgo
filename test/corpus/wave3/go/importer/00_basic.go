package main

// L11 (REVIEW-2026-11-18-05): go/importer — zero corpus imports before
// this program. Importing a package from source: the source importer
// type-checks go/token from $GOROOT/src, scope lookup of known and
// unknown names, and the error path on a missing package.

import (
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"sort"
)

func main() {
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)
	pkg, err := imp.Import("go/token")
	if err != nil {
		fmt.Printf("import-err=%v\n", err != nil)
		return
	}
	fmt.Printf("pkg-path=%s name=%s complete=%v\n", pkg.Path(), pkg.Name(), pkg.Complete())
	names := pkg.Scope().Names()
	sort.Strings(names)
	fmt.Printf("scope-names=%d\n", len(names))
	if len(names) > 8 {
		names = names[:8]
	}
	fmt.Printf("scope-first=%q\n", names)
	// Known members.
	for _, name := range []string{"NoPos", "FileSet", "Position", "ADD", "EQL"} {
		obj := pkg.Scope().Lookup(name)
		fmt.Printf("lookup %s found=%v\n", name, obj != nil)
		if obj != nil {
			fmt.Printf("lookup %s type=%s\n", name, obj.Type())
		}
	}
	fmt.Printf("lookup missing=%v\n", pkg.Scope().Lookup("NosuchName") == nil)
	// importer.Default is the gc importer.
	def := importer.Default()
	fmt.Printf("default-non-nil=%v\n", def != nil)
	// Missing package: error path.
	_, err = imp.Import("nosuch/package")
	fmt.Printf("missing-err=%v\n", err != nil)
	// The imported package is a *types.Package; check a type object.
	pos := pkg.Scope().Lookup("NoPos")
	if c, ok := pos.(*types.Const); ok {
		fmt.Printf("nopos-const-val=%v\n", c.Val())
	}
}
