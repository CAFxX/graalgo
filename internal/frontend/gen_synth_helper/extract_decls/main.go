// Command extract_decls lists top-level function declarations of a Go source
// file, one per line, as:
//
//	FUNC|<name>|<recv>|<start>|<end>
//
// <start>/<end> are 0-based byte offsets of the `func` keyword through the
// closing brace of the body. It backs gen_synth_archive_mime.py, which feeds
// it a temp file holding "package p\n" + source and subtracts that 10-byte
// prefix itself. Replaces the ad-hoc /tmp/extract_decls binary that was never
// checked in (P1-R1-2); built on demand by run_gen_synth.py.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: extract_decls <file.go>\n")
		os.Exit(2)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, os.Args[1], nil, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "extract_decls: parse error: %v\n", err)
		os.Exit(1)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		recv := ""
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			// Raw receiver type as written, e.g. "*Reader" or "Writer".
			recv = recvString(fset, fn.Recv.List[0].Type)
		}
		start := fset.Position(fn.Pos()).Offset
		end := fset.Position(fn.End()).Offset
		fmt.Printf("FUNC|%s|%s|%d|%d\n", fn.Name.Name, recv, start, end)
	}
}

// recvString renders a receiver type expression without spaces, preserving a
// leading '*' so the caller can detect pointer receivers.
func recvString(fset *token.FileSet, e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return "*" + recvString(fset, t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr: // generic receiver: Name[T]
		return recvString(fset, t.X)
	case *ast.IndexListExpr: // generic receiver: Name[K, V]
		return recvString(fset, t.X)
	default:
		// Fallback: slice of source text.
		start := fset.Position(e.Pos()).Offset
		end := fset.Position(e.End()).Offset
		src, err := os.ReadFile(fset.Position(e.Pos()).Filename)
		if err != nil || start < 0 || end > len(src) || start > end {
			return ""
		}
		out := ""
		for _, c := range string(src[start:end]) {
			if c != ' ' && c != '\t' && c != '\n' {
				out += string(c)
			}
		}
		return out
	}
}
