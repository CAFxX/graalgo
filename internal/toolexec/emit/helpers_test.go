package emit

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"
	"testing"

	"graalgo-toolexec/ir"
)

// srcImporter type-checks in-memory packages from source and falls back to
// gc export data (go list) for everything else.
type srcImporter struct {
	t     *testing.T
	fset  *token.FileSet
	srcs  map[string]map[string]string // import path -> file name -> source
	pkgs  map[string]*types.Package
	infos map[string]*types.Info
	files map[string][]*ast.File
	std   types.Importer
}

func (s *srcImporter) Import(path string) (*types.Package, error) {
	if p := s.pkgs[path]; p != nil {
		return p, nil
	}
	if _, ok := s.srcs[path]; !ok {
		if path == "unsafe" {
			return types.Unsafe, nil
		}
		if s.std == nil {
			s.std = importer.ForCompiler(s.fset, "gc", nil)
		}
		return s.std.Import(path)
	}
	return s.check(path), nil
}

func (s *srcImporter) check(path string) *types.Package {
	s.t.Helper()
	var names []string
	for n := range s.srcs[path] {
		names = append(names, n)
	}
	sort.Strings(names)
	var files []*ast.File
	for _, n := range names {
		f, err := parser.ParseFile(s.fset, n, s.srcs[path][n], parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			s.t.Fatal(err)
		}
		files = append(files, f)
	}
	info := NewInfo()
	conf := types.Config{GoVersion: "go1.27", Sizes: types.SizesFor("gc", "amd64"), Importer: s}
	pkg, err := conf.Check(path, s.fset, files, info)
	if err != nil {
		s.t.Fatal(err)
	}
	s.pkgs[path], s.infos[path], s.files[path] = pkg, info, files
	return pkg
}

// emitSrcCfg emits package "p" from one source file, with cfg tweaks.
func emitSrcCfg(t *testing.T, src string, tweak func(*Config)) *ir.File {
	t.Helper()
	s := &srcImporter{t: t, fset: token.NewFileSet(), srcs: map[string]map[string]string{"p": {"p.go": src}},
		pkgs: map[string]*types.Package{}, infos: map[string]*types.Info{}, files: map[string][]*ast.File{}}
	pkg := s.check("p")
	cfg := Config{Fset: s.fset, Pkg: pkg, Info: s.infos["p"], Files: s.files["p"], Lang: "go1.27", GOOS: "linux", GOARCH: "amd64"}
	if tweak != nil {
		tweak(&cfg)
	}
	f, _, err := Emit(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Every emitted file must survive the codec.
	data, err := ir.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	g, err := ir.DecodeValidate(data)
	if err != nil {
		t.Fatal(err)
	}
	for i := range g.Funcs {
		if _, err := g.Body(i); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func emitSrc(t *testing.T, src string) *ir.File { return emitSrcCfg(t, src, nil) }

// walkNodes calls fn for every Stmt and Expr node reachable from v.
func walkNodes(v reflect.Value, fn func(any)) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return
		}
		if v.Kind() == reflect.Interface {
			fn(v.Elem().Interface())
		}
		walkNodes(v.Elem(), fn)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				walkNodes(v.Field(i), fn)
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		for i := range v.Len() {
			walkNodes(v.Index(i), fn)
		}
	}
}

// funcBody returns the body of the FUNCINDEX entry named name.
func funcBody(t *testing.T, f *ir.File, name string) *ir.FuncBody {
	t.Helper()
	for i, fn := range f.Funcs {
		if fn.Name == name {
			return f.Bodies[i]
		}
	}
	t.Fatalf("no function %q", name)
	return nil
}

// nodesOf returns every node of type T in body.
func nodesOf[T any](body any) []T {
	var out []T
	walkNodes(reflect.ValueOf(body), func(n any) {
		if x, ok := n.(T); ok {
			out = append(out, x)
		}
	})
	return out
}

func typeKey(f *ir.File, id ir.TypeID) string {
	if id == 0 {
		return ""
	}
	return f.Types[id-1].Key
}

// line returns the 1-based source line of position id in body.
func posLine(body *ir.FuncBody, id ir.PosID) int {
	line := 0
	for i := 0; i < int(id); i++ {
		line += int(body.Positions[i].Line)
	}
	return line
}

func hasKey(f *ir.File, key string) bool {
	for _, te := range f.Types {
		if te.Key == key {
			return true
		}
	}
	return false
}

func keysContaining(f *ir.File, sub string) []string {
	var out []string
	for _, te := range f.Types {
		if strings.Contains(te.Key, sub) {
			out = append(out, te.Key)
		}
	}
	return out
}

func reflectOf(x any) reflect.Value { return reflect.ValueOf(x) }
