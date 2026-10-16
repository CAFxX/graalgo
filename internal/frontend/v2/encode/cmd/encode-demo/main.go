// Command encode-demo: encodes a Go file's v2 Document to binary.
//
// Usage: encode-demo <file.go> <output.bin>
package main

import (
	"fmt"
	"os"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/encode"
	"graalgo-frontend-v2/parser"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <file.go> <output.bin>\n", os.Args[0])
		os.Exit(1)
	}

	pres, err := parser.ParseFiles([]string{os.Args[1]})
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
		os.Exit(2)
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "check: %v\n", err)
		os.Exit(2)
	}
	doc, err := emit.Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emit: %v\n", err)
		os.Exit(2)
	}
	bin, err := encode.Encode(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[2], bin, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("encoded %d bytes to %s\n", len(bin), os.Args[2])
}
