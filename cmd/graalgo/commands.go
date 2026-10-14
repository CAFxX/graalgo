package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cmdRun implements `graalgo run [build flags] <file.go|package> [args...]`.
//
// For a single .go file, invokes the backend directly. Program arguments
// after the file are passed through to the program.
func cmdRun(root string, args []string) int {
	// Split flags from the file/package and program args. The first
	// non-flag argument is the file or package; everything after it is
	// program arguments.
	var flags []string
	var rest []string
	for i, a := range args {
		if strings.HasPrefix(a, "-") && len(rest) == 0 {
			flags = append(flags, a)
			continue
		}
		rest = args[i:]
		break
	}
	_ = flags // build flags accepted but currently unused

	if len(rest) == 0 {
		fmt.Fprintf(os.Stderr, "graalgo run: no Go files listed\n")
		return 2
	}

	target := rest[0]
	progArgs := rest[1:]

	// Single file: run it directly.
	if strings.HasSuffix(target, ".go") {
		runArgs := []string{target}
		runArgs = append(runArgs, progArgs...)
		return graalgoRun(root, runArgs)
	}

	// Package (directory or import path): find its main package files.
	files, err := resolveFiles([]string{target})
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo run: %v\n", err)
		return 1
	}
	// For a package, concatenate: the backend handles one file; for
	// multi-file packages we check each file then run the first.
	// (Full package support: run the package's main files in order.)
	for _, f := range files {
		if code := graalgoRun(root, []string{"--check", f}); code != 0 {
			return code
		}
	}
	runArgs := []string{files[0]}
	runArgs = append(runArgs, progArgs...)
	return graalgoRun(root, runArgs)
}

// cmdBuild implements `graalgo build [packages]`.
//
// Validates that the named packages translate through the frontend.
// Unlike go build, no binary is produced: the Truffle backend JIT-compiles
// at run time, so "build" is a frontend-correctness gate.
func cmdBuild(root string, args []string) int {
	files, err := resolveFiles(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
		return 1
	}
	failed := false
	for _, f := range files {
		if code := graalgoRun(root, []string{"--check", f}); code != 0 {
			fmt.Fprintf(os.Stderr, "graalgo build: %s: build failed\n", f)
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// cmdVet implements `graalgo vet [packages]`.
//
// Runs the frontend's static checks over the named packages. Currently
// this is the frontend translation check; deeper vet analyses arrive
// with the driver work.
func cmdVet(root string, args []string) int {
	files, err := resolveFiles(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
		return 1
	}
	failed := false
	for _, f := range files {
		if code := graalgoRun(root, []string{"--check", f}); code != 0 {
			failed = true
		}
	}
	if failed {
		return 1
	}
	fmt.Println("vet: no issues found")
	return 0
}

// cmdFmt implements `graalgo fmt [packages]`.
//
// Formats the Go source files using gofmt from the stock toolchain.
func cmdFmt(root string, args []string) int {
	files, err := resolveFiles(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo fmt: %v\n", err)
		return 1
	}
	gofmt := findGofmt()
	if gofmt == "" {
		fmt.Fprintf(os.Stderr, "graalgo fmt: cannot locate gofmt\n")
		return 1
	}
	cmdArgs := append([]string{"-w"}, files...)
	cmd := exec.Command(gofmt, cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "graalgo fmt: %v\n", err)
		return 1
	}
	return 0
}

// findGofmt locates the gofmt binary: GOROOT/bin/gofmt, beside the go
// binary on PATH, or ~/tools/go/bin/gofmt.
func findGofmt() string {
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		if p := filepath.Join(goroot, "bin", "gofmt"); isExec(p) {
			return p
		}
	}
	if goBin, err := exec.LookPath("go"); err == nil {
		if p := filepath.Join(filepath.Dir(goBin), "gofmt"); isExec(p) {
			return p
		}
	}
	for _, home := range []string{os.Getenv("HOME"), "/root"} {
		if home == "" {
			continue
		}
		if p := filepath.Join(home, "tools", "go", "bin", "gofmt"); isExec(p) {
			return p
		}
	}
	if p, err := exec.LookPath("gofmt"); err == nil {
		return p
	}
	return ""
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0111 != 0
}
