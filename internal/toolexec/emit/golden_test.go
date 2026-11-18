package emit

import (
	"bytes"
	"flag"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"graalgo-toolexec/ir"
)

var update = flag.Bool("update", false, "rewrite the golden vectors in ../ir/testdata")

const vectors = "../ir/testdata"

// goldenImporter type-checks the golden packages from source: dep.go is
// package example.com/dep, imported by all.go.
type goldenImporter struct {
	t    *testing.T
	fset *token.FileSet
	pkgs map[string]*types.Package
}

func (g *goldenImporter) Import(path string) (*types.Package, error) {
	switch path {
	case "unsafe":
		return types.Unsafe, nil
	case "embed":
		return importer.ForCompiler(g.fset, "gc", nil).Import(path)
	case "example.com/dep":
		if p := g.pkgs[path]; p != nil {
			return p, nil
		}
		f, _, _ := g.check(filepath.Join(vectors, "src/dep.go"), path)
		return f, nil
	}
	return nil, os.ErrNotExist
}

func (g *goldenImporter) check(file, path string) (*types.Package, *types.Info, *ast.File) {
	f, err := parser.ParseFile(g.fset, file, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		g.t.Fatal(err)
	}
	info := NewInfo()
	conf := types.Config{GoVersion: "go1.27", Sizes: types.SizesFor("gc", "amd64"), Importer: g}
	pkg, err := conf.Check(path, g.fset, []*ast.File{f}, info)
	if err != nil {
		g.t.Fatal(err)
	}
	g.pkgs[path] = pkg
	return pkg, info, f
}

// emitGolden emits golden package name ("all" or "dep").
func emitGolden(t *testing.T, name string) *ir.File {
	t.Helper()
	g := &goldenImporter{t: t, fset: token.NewFileSet(), pkgs: map[string]*types.Package{}}
	pkg, info, f := g.check(filepath.Join(vectors, "src", name+".go"), "example.com/"+name)
	embed := &EmbedConfig{Patterns: map[string][]string{"embedded.txt": {"embedded.txt"}},
		Files: map[string]string{"embedded.txt": filepath.Join(vectors, "src", "embedded.txt")}}
	file, _, err := Emit(Config{Fset: g.fset, Pkg: pkg, Info: info, Files: []*ast.File{f}, Lang: "go1.27",
		GOOS: "linux", GOARCH: "amd64", FileName: filepath.Base, Embed: embed})
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func emitSource(t *testing.T, _ string) *ir.File { return emitGolden(t, "all") }

func encode(t *testing.T, f *ir.File) []byte {
	t.Helper()
	data, err := ir.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The golden vector: what the emitter produces for testdata/src/all.go,
// byte for byte. The Java tests decode the same file (make test-gir).
func TestGolden(t *testing.T) {
	for _, name := range []string{"all", "dep"} {
		data := encode(t, emitGolden(t, name))
		golden := filepath.Join(vectors, name+".gir")
		if *update {
			if err := os.WriteFile(golden, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%v (run go test ./emit -run TestGolden -update)", err)
		}
		if !bytes.Equal(data, want) {
			t.Fatalf("emitter output differs from %s (%d vs %d bytes); if intended, rerun with -update", golden, len(data), len(want))
		}
		// Deterministic.
		if again := encode(t, emitGolden(t, name)); !bytes.Equal(again, data) {
			t.Fatalf("%s: emitting the same package twice gives different bytes", name)
		}
	}
}

// Go round trip: decode + validate + re-encode reproduces the input.
func TestGoRoundTrip(t *testing.T) {
	data := encode(t, emitSource(t, filepath.Join(vectors, "src/all.go")))
	f, err := ir.DecodeValidate(data)
	if err != nil {
		t.Fatal(err)
	}
	again := encode(t, f)
	if !bytes.Equal(again, data) {
		t.Fatalf("re-encoding a decoded file changes it (%d vs %d bytes)", len(again), len(data))
	}
}

// collect walks a decoded value and records the dynamic type of every
// union member and every Builtin used.
func collect(v reflect.Value, seen map[string]bool) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return
		}
		if v.Kind() == reflect.Interface {
			seen[v.Elem().Type().String()] = true
		}
		collect(v.Elem(), seen)
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(ir.BuiltinCallExpr{}) {
			seen["builtin:"+ir.Builtin(v.FieldByName("Builtin").Uint()).String()] = true
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				collect(v.Field(i), seen)
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			collect(v.Index(i), seen)
		}
	}
}

// The golden vector exercises every member of every union and every
// builtin, so that both decoders are tested on every node kind.
func TestGoldenCoversSchema(t *testing.T) {
	f := emitSource(t, filepath.Join(vectors, "src/all.go"))
	seen := map[string]bool{}
	collect(reflect.ValueOf(f).Elem(), seen)
	var missing []string
	for kind, names := range map[string]map[int]string{"Expr": ir.ExprKindNames, "Stmt": ir.StmtKindNames, "Type": ir.TypeKindNames} {
		for _, n := range names {
			if !seen["*ir."+n+kind] {
				missing = append(missing, n+kind)
			}
		}
	}
	for b := ir.Builtin(1); b.Valid(); b++ {
		if !seen["builtin:"+b.String()] {
			missing = append(missing, "builtin "+b.String())
		}
	}
	slices.Sort(missing)
	if len(missing) > 0 {
		t.Errorf("golden vector lacks: %s", strings.Join(missing, ", "))
	}
}

// Spot checks of the lowering itself, on the golden package.
func TestLoweringSpotChecks(t *testing.T) {
	f := emitSource(t, filepath.Join(vectors, "src/all.go"))
	byName := map[string]int{}
	for i, fn := range f.Funcs {
		byName[fn.Name] = i
	}
	for _, want := range []string{"seed", "Point.Sum", "(*Point).Scale", "(*List).Push", "Max", "init.0", "init·vars", "runtimeNano"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("no FUNCINDEX entry %q (have %v)", want, slices.Sorted(mapsKeys(byName)))
		}
	}
	if fn := f.Funcs[byName["runtimeNano"]]; fn.Flags&1 == 0 || fn.Linkname != "runtime.nanotime" || f.Bodies[byName["runtimeNano"]] != nil {
		t.Errorf("bodyless linkname function: %+v", fn)
	}
	if fn := f.Funcs[byName["Max"]]; fn.Flags&2 == 0 || len(fn.Tparams) != 1 {
		t.Errorf("generic function: flags %d tparams %v", fn.Flags, fn.Tparams)
	}
	// Init order: b depends on a, so a's initializer comes first.
	init := f.Bodies[f.Init.VarInit]
	var order []string
	for _, s := range init.Body {
		as := s.(*ir.AssignStmt)
		if g, ok := as.Lhs[0].(*ir.GlobalExpr); ok {
			order = append(order, f.Objects[g.Obj].Name)
		}
	}
	if strings.Join(order, ",") != "embedded,a,b,pair,ch,recvCh" {
		t.Errorf("initializer order = %v", order)
	}
	// Closures: n is captured by inc and, transitively, by the inner literal.
	cl := f.Bodies[byName["Closures"]]
	var lit *ir.FuncLitExpr
	for _, s := range cl.Body {
		if d, ok := s.(*ir.DefineStmt); ok {
			if l, ok := d.Rhs[0].(*ir.FuncLitExpr); ok {
				lit = l
			}
		}
	}
	if lit == nil || len(lit.Captures) != 1 || cl.Locals[lit.Captures[0]].Name != "n" || cl.Locals[lit.Captures[0]].Flags&8 == 0 || lit.Body.Locals[0].Name != "n" {
		t.Fatalf("capture of n: %+v", lit)
	}
	// Implicit conversions are explicit: var sh Shape = *p boxes into the
	// interface, and boxed's return g() converts element-wise.
	ctl := f.Bodies[byName["Control"]]
	found := false
	var walk func(st []ir.Stmt)
	walk = func(st []ir.Stmt) {
		for _, s := range st {
			if v, ok := s.(*ir.VarStmt); ok && ctl.Locals[v.Locals[0]].Name == "sh" {
				c, ok := v.Values[0].(*ir.ConvertExpr)
				found = ok && c.Implicit && f.Types[c.Type-1].Key == "example.com/all.Shape"
			}
		}
	}
	walk(ctl.Body)
	if !found {
		t.Error("var sh Shape = *p: no implicit conversion to Shape")
	}
	ret := f.Bodies[byName["boxed"]].Body[0].(*ir.ReturnStmt)
	if tc, ok := ret.Results[0].(*ir.TupleConvertExpr); !ok || f.Types[tc.Type-1].Key != "(interface {}, example.com/all.Shape)" {
		t.Errorf("return pt() in boxed: %#v", ret.Results[0])
	}
	// Typedefs carry method sets with promotion paths.
	for _, td := range f.TypeDefs {
		if f.Types[td.Type-1].Key != "example.com/all.Outer" {
			continue
		}
		var bump *ir.MsetEntry
		for i := range td.PtrMset {
			if td.PtrMset[i].Name == "Bump" {
				bump = &td.PtrMset[i]
			}
		}
		if bump == nil || !slices.Equal(bump.Path, []uint64{0}) || f.SymRefs[bump.Method].Name != "(*Inner).Bump" {
			t.Errorf("Outer's promoted Bump: %+v", bump)
		}
		if len(td.Mset) != 0 {
			t.Errorf("Outer's value method set should be empty, got %+v", td.Mset)
		}
		if td.Size != 24 || !slices.Equal(td.Offsets, []int64{0, 8}) {
			t.Errorf("Outer layout: size %d offsets %v", td.Size, td.Offsets)
		}
	}
}

func mapsKeys(m map[string]int) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
