// Package parser implements stage 1 of the frontend/v2 pipeline: Go source
// text → untyped AST.
//
// This is a thin, honest wrapper around the standard library's go/parser.
// It does not invent syntax, does not synthesize packages, and does not
// apply build constraints beyond what go/build already provides. Its job
// is to turn a list of .go files into an *ast.Package with accurate
// source positions.
package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
)

// Result is the output of stage 1.
type Result struct {
	Fset *token.FileSet
	Pkg  *ast.Package
	// Files are the absolute paths that were parsed, in order.
	Files []string
}

// ParseFiles parses the named Go source files, which must all declare the
// same package. Each file is parsed independently with go/parser; the
// resulting *ast.Files are assembled into one *ast.Package.
func ParseFiles(filenames []string) (*Result, error) {
	if len(filenames) == 0 {
		return nil, fmt.Errorf("parser: no input files")
	}
	fset := token.NewFileSet()
	sources := make(map[string]string, len(filenames))
	absFiles := make([]string, 0, len(filenames))
	for _, fn := range filenames {
		abs, err := filepath.Abs(fn)
		if err != nil {
			return nil, fmt.Errorf("parser: %v", err)
		}
		src, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("parser: %v", err)
		}
		sources[abs] = string(src)
		absFiles = append(absFiles, abs)
	}
	pkg, err := parseSources(fset, sources)
	if err != nil {
		return nil, err
	}
	return &Result{Fset: fset, Pkg: pkg, Files: absFiles}, nil
}

// ParseSources parses multiple in-memory sources as one package.
// Each entry maps a filename to its source text.
func ParseSources(fset *token.FileSet, sources map[string]string) (*ast.Package, error) {
	return parseSources(fset, sources)
}

func parseSources(fset *token.FileSet, sources map[string]string) (*ast.Package, error) {
	files := make(map[string]*ast.File, len(sources))
	var pkgName string
	for name, src := range sources {
		// Syntax errors are fatal here. Semantic problems are stage 2's job.
		// SkipObjectResolution: we do our own resolution in the checker via
		// go/types, which is authoritative. The parser's best-effort
		// resolution would only add noise.
		f, err := parser.ParseFile(fset, name, src,
			parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parser: %s: %v", name, err)
		}
		if pkgName == "" {
			pkgName = f.Name.Name
		} else if f.Name.Name != pkgName {
			return nil, fmt.Errorf("parser: %s declares package %s, want %s",
				name, f.Name.Name, pkgName)
		}
		files[name] = f
	}
	return &ast.Package{
		Name:  pkgName,
		Files: files,
	}, nil
}
