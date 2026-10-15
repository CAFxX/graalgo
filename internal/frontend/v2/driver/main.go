// Command gofrontend-v2: the frontend/v2 pipeline driver.
//
// Usage: gofrontend-v2 <file.go> [file.go...]
// Runs the clean parser → checker → emitter pipeline with no synthetic
// packages, no special cases, and no desugaring workarounds, and prints
// a summary of the resulting in-memory program. There is no JSON wire
// format: the emitter hands the program to the backend directly.
package main

import (
	"fmt"
	"go/token"
	"os"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/parser"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <file.go> [file.go...]\n", os.Args[0])
		os.Exit(1)
	}

	// Stage 1: parse.
	pres, err := parser.ParseFiles(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "gofrontend-v2: %v\n", err)
		os.Exit(2)
	}

	// Stage 2: type-check.
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{
		GoVersion: "go1.27",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gofrontend-v2: %v\n", err)
		os.Exit(2)
	}

	// Stage 3: emit.
	doc, err := emit.Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gofrontend-v2: %v\n", err)
		os.Exit(2)
	}
	_ = token.NoPos
	fmt.Printf("package %s: %d funcs, %d types, %d objects\n",
		doc.Package["name"], len(doc.Funcs), len(doc.Types), len(doc.Objects))
}
