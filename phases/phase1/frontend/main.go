// Command gofrontend: Go frontend service for graalgo.
//
// gofrontend type-checks a single-file Go program with the vendored go/*
// packages (see VENDORING.md) and emits the frontend/v1 typed-AST JSON
// document (see ../docs/frontend-v1.md) to stdout.
//
// Usage: gofrontend <file.go>
// Exit codes: 0 success, 1 usage error, 2 frontend error (details on stderr).
package main

import (
	"fmt"
	"os"
)

const toolVersion = "0.7.0"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: gofrontend <file.go>\n")
		os.Exit(1)
	}
	out, err := ExportFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "gofrontend: %v\n", err)
		os.Exit(2)
	}
	fmt.Println(out)
}
