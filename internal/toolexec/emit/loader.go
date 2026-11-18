package emit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Package is a loaded, type-checked package ready for Emit.
type Package struct {
	ImportPath string
	Config     Config
}

type listedPackage struct {
	ImportPath string
	Name       string
	Dir        string
	GoFiles    []string
	ImportMap  map[string]string
	Export     string
	Standard   bool
	Error      *struct{ Err string }

	EmbedPatterns []string
	EmbedFiles    []string
}

// Load lists patterns with `go list -deps -export` (GOFLAGS -tags=purego,
// CGO_ENABLED=0: graalgo's build configuration) and type-checks every
// listed package that has Go files against the gc export data of its
// dependencies, exactly as graalgo-tool's compile step does (design §2.2
// step 4). Packages that fail to load are returned in errs, not fatal.
func Load(patterns ...string) (pkgs []*Package, errs []error, err error) {
	args := append([]string{"list", "-e", "-deps", "-export", "-tags=purego",
		"-json=ImportPath,Name,Dir,GoFiles,ImportMap,Export,Standard,Error,EmbedPatterns,EmbedFiles"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("go list: %v\n%s", err, stderr.Bytes())
	}
	var listed []*listedPackage
	exports := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		lp := &listedPackage{}
		if err := dec.Decode(lp); err != nil {
			return nil, nil, err
		}
		listed = append(listed, lp)
		exports[lp.ImportPath] = lp.Export
	}
	for _, lp := range listed {
		if lp.Error != nil {
			errs = append(errs, fmt.Errorf("%s: %s", lp.ImportPath, lp.Error.Err))
			continue
		}
		if len(lp.GoFiles) == 0 || lp.ImportPath == "unsafe" {
			continue
		}
		p, err := check(lp, exports)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", lp.ImportPath, err))
			continue
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, errs, nil
}

// NewInfo returns a types.Info with every map Emit needs.
func NewInfo() *types.Info {
	return &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		Instances:    map[*ast.Ident]types.Instance{},
		Scopes:       map[ast.Node]*types.Scope{},
		FileVersions: map[*ast.File]string{},
	}
}

func check(lp *listedPackage, exports map[string]string) (*Package, error) {
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range lp.GoFiles {
		f, err := parser.ParseFile(fset, filepath.Join(lp.Dir, name), nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	lookup := func(path string) (io.ReadCloser, error) {
		if m, ok := lp.ImportMap[path]; ok {
			path = m
		}
		file := exports[path]
		if file == "" {
			return nil, fmt.Errorf("no export data for %q", path)
		}
		return os.Open(file)
	}
	lang := "go1.27"
	conf := types.Config{
		Importer:  importer.ForCompiler(fset, "gc", lookup),
		GoVersion: lang,
		Sizes:     types.SizesFor("gc", runtime.GOARCH),
	}
	info := NewInfo()
	pkg, err := conf.Check(lp.ImportPath, fset, files, info)
	if err != nil {
		return nil, err
	}
	root := runtime.GOROOT()
	var embed *EmbedConfig
	if len(lp.EmbedPatterns) > 0 {
		embed = listEmbedConfig(lp)
	}
	return &Package{ImportPath: lp.ImportPath, Config: Config{
		Fset: fset, Pkg: pkg, Info: info, Files: files, Lang: lang, Std: lp.Standard,
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Embed: embed,
		FileName: func(name string) string {
			// What `go build -trimpath` records for std: $GOROOT/src/x -> x.
			if rel, err := filepath.Rel(filepath.Join(root, "src"), name); err == nil && lp.Standard {
				return rel
			}
			return name
		},
	}}, nil
}

// listEmbedConfig reconstructs the -embedcfg cmd/go would pass from go
// list's EmbedPatterns and EmbedFiles: a file belongs to a pattern when
// the pattern matches the file or one of its parent directories (cmd/go
// has already applied the hidden-file and "all:" rules to EmbedFiles).
func listEmbedConfig(lp *listedPackage) *EmbedConfig {
	cfg := &EmbedConfig{Patterns: map[string][]string{}, Files: map[string]string{}}
	for _, pat := range lp.EmbedPatterns {
		glob := strings.TrimPrefix(pat, "all:")
		for _, file := range lp.EmbedFiles {
			for p := file; p != "." && p != "/"; p = path.Dir(p) {
				if ok, _ := path.Match(glob, p); ok {
					cfg.Patterns[pat] = append(cfg.Patterns[pat], file)
					break
				}
			}
		}
	}
	for _, file := range lp.EmbedFiles {
		cfg.Files[file] = filepath.Join(lp.Dir, filepath.FromSlash(file))
	}
	return cfg
}
