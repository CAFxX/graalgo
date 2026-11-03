package p3frontend

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFlattenDepInitOrder verifies REVIEW-2026-11-03-00 N-P1-2: a
// dependency package's func init() must survive Flatten with gc's
// package-at-a-time initialization order, not the old loud rejection
// and not a naive single-package merge (which would run ALL var
// initializers before ANY init).
//
// The test is a gc differential on the flattened program itself: the
// merged source run under gc must print exactly the two-package gc
// order (dep vars, dep inits, main vars, main inits, main body).
// It is capable of failing three ways: the pre-fix code errors in
// MergeDir; a naive deletion of the rejection emits the dep init as a
// real init (wrong order: MAIN-VAR-INIT before DEP-INIT); and a
// broken runner (missing/uncalled) drops DEP-INIT entirely.
func TestFlattenDepInitOrder(t *testing.T) {
	depGo := `package dep

import "fmt"

var DepVar = initDepVar()

func initDepVar() string {
	fmt.Println("DEP-VAR-INIT")
	return "dep"
}

func init() {
	fmt.Println("DEP-INIT")
}

func Hello() string {
	return "hello-" + DepVar
}
`
	mainGo := `package main

import (
	"example.com/initprobe/dep"
	"fmt"
)

var MainVar = initMainVar()

func initMainVar() string {
	fmt.Println("MAIN-VAR-INIT")
	return "main"
}

func init() {
	fmt.Println("MAIN-INIT")
}

func main() {
	fmt.Println("MAIN-BODY", dep.Hello())
}
`
	t.Run("basic", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"go.mod":     "module example.com/initprobe\n\ngo 1.27\n",
			"dep/dep.go": depGo,
			"main.go":    mainGo,
		})
		merged := mergeDirOrFail(t, dir)
		assertFlattenedInitShape(t, merged, 1)
		assertGCRunOrder(t, merged, []string{
			"DEP-VAR-INIT", "DEP-INIT", "MAIN-VAR-INIT", "MAIN-INIT",
			"MAIN-BODY hello-dep",
		})
	})

	// A dep func literally named runInit must not collide with the
	// synthetic per-package runner: the allocator appends "_" until
	// the name is free, and the merged program must still compile
	// and print the gc order.
	t.Run("runnerNameCollision", func(t *testing.T) {
		dir := t.TempDir()
		collidingDep := strings.Replace(depGo, "func Hello() string {",
			`func runInit() string { return "not-the-runner" }

func Hello() string {`, 1)
		writeFiles(t, dir, map[string]string{
			"go.mod":     "module example.com/initprobe\n\ngo 1.27\n",
			"dep/dep.go": collidingDep,
			"main.go":    mainGo,
		})
		merged := mergeDirOrFail(t, dir)
		assertFlattenedInitShape(t, merged, 1)
		assertGCRunOrder(t, merged, []string{
			"DEP-VAR-INIT", "DEP-INIT", "MAIN-VAR-INIT", "MAIN-INIT",
			"MAIN-BODY hello-dep",
		})
	})

	// Two dep files, one init each: both inits run, in file order,
	// between the dep vars and the main vars.
	t.Run("twoDepFiles", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"go.mod":   "module example.com/initprobe\n\ngo 1.27\n",
			"dep/a.go": "package dep\n\nimport \"fmt\"\n\nvar DepVar = initDepVar()\n\nfunc initDepVar() string {\n\tfmt.Println(\"DEP-VAR-INIT\")\n\treturn \"dep\"\n}\n\nfunc init() {\n\tfmt.Println(\"DEP-INIT-A\")\n}\n\nfunc Hello() string {\n\treturn \"hello-\" + DepVar\n}\n",
			"dep/b.go": "package dep\n\nimport \"fmt\"\n\nfunc init() {\n\tfmt.Println(\"DEP-INIT-B\")\n}\n",
			"main.go":  mainGo,
		})
		merged := mergeDirOrFail(t, dir)
		assertFlattenedInitShape(t, merged, 2)
		assertGCRunOrder(t, merged, []string{
			"DEP-VAR-INIT", "DEP-INIT-A", "DEP-INIT-B", "MAIN-VAR-INIT",
			"MAIN-INIT", "MAIN-BODY hello-dep",
		})
	})
}

func mergeDirOrFail(t *testing.T, dir string) string {
	t.Helper()
	merged, err := MergeDir(dir)
	if err != nil {
		t.Fatalf("MergeDir: %v", err)
	}
	return merged
}

// assertFlattenedInitShape checks the merged source's top-level shape:
// exactly one real func init (main's), one blank-var runner call per
// dep-init set, and the ordering DepVar < blank-runner < MainVar that
// is what reproduces package-at-a-time order under gc's
// declaration-order tie-break. depInits is the expected number of
// renamed dep inits (i.e. runner call statements).
func assertFlattenedInitShape(t *testing.T, merged string, depInits int) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "merged.go", merged, 0)
	if err != nil {
		t.Fatalf("merged source does not parse: %v\n%s", err, merged)
	}
	var (
		initCount   int
		runnerCalls int
		declOrder   []string
	)
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "init" {
				initCount++
			}
			declOrder = append(declOrder, "func:"+d.Name.Name)
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				vs := spec.(*ast.ValueSpec)
				for _, name := range vs.Names {
					declOrder = append(declOrder, "var:"+name.Name)
				}
				if len(vs.Names) == 1 && vs.Names[0].Name == "_" &&
					len(vs.Values) == 1 {
					if call, ok := vs.Values[0].(*ast.CallExpr); ok {
						if id, ok := call.Fun.(*ast.Ident); ok &&
							strings.Contains(id.Name, "runInit") {
							runnerCalls++
						}
					}
				}
			}
		}
	}
	if initCount != 1 {
		t.Fatalf("merged source has %d real func init, want exactly 1 (main's):\n%s", initCount, merged)
	}
	if runnerCalls != 1 {
		t.Fatalf("merged source has %d blank-var runner calls, want 1:\n%s", runnerCalls, merged)
	}
	// The runner body must call exactly depInits renamed init funcs.
	runnerBodyCalls := 0
	for _, decl := range f.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if !ok || !strings.Contains(d.Name.Name, "runInit") {
			continue
		}
		for _, stmt := range d.Body.List {
			if es, ok := stmt.(*ast.ExprStmt); ok {
				if _, ok := es.X.(*ast.CallExpr); ok {
					runnerBodyCalls++
				}
			}
		}
	}
	if runnerBodyCalls != depInits {
		t.Fatalf("runner calls %d renamed inits, want %d:\n%s", runnerBodyCalls, depInits, merged)
	}
	// Declaration order: some renamed dep var, then the blank var,
	// then MainVar. (Renamed dep vars carry the p3i_ prefix.)
	idxBlank, idxMain := -1, -1
	idxDep := -1
	for i, d := range declOrder {
		switch {
		case d == "var:_" && idxBlank < 0:
			idxBlank = i
		case d == "var:MainVar":
			idxMain = i
		case strings.HasPrefix(d, "var:p3i_") && strings.HasSuffix(d, "DepVar"):
			idxDep = i
		}
	}
	if idxDep < 0 || idxBlank < 0 || idxMain < 0 {
		t.Fatalf("decl order %v lacks dep var / blank var / MainVar:\n%s", declOrder, merged)
	}
	if !(idxDep < idxBlank && idxBlank < idxMain) {
		t.Fatalf("decl order %v: want dep var < blank runner < MainVar:\n%s", declOrder, merged)
	}
}

// assertGCRunOrder runs the merged source under gc and checks the
// exact marker order.
func assertGCRunOrder(t *testing.T, merged string, want []string) {
	t.Helper()
	dir := t.TempDir()
	prog := filepath.Join(dir, "merged.go")
	if err := os.WriteFile(prog, []byte(merged), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, goTool(t), "run", prog)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run merged source: %v\n%s", err, merged)
	}
	got := strings.Split(strings.TrimSpace(string(out)), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("gc order mismatch:\n got: %q\nwant: %q\nmerged:\n%s", got, want, merged)
	}
}
