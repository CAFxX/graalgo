// Command graalgo-tool is the -toolexec wrapper of the graalgo backend:
//
//	go build -toolexec /path/to/graalgo-tool ./...
//
// cmd/go then runs every toolchain tool as "graalgo-tool <tool> <args>".
// This walking skeleton implements the protocol of
// docs/arch/toolexec-design.md §2 for the two tools graalgo replaces:
//
//   - compile: run the stock compiler unchanged (it writes the archive with
//     gc export data and the build ID), then type-check the same files with
//     go/types against that export data and append the package's IR to the
//     archive.
//   - link: collect the IR of every package of the link into a bundle and
//     write launcher stub + bundle as the executable.
//
// Every other tool (asm, cgo, cover, vet, ...) is passed through.
// "<tool> -V=full" is answered with the stock line plus a graalgo suffix,
// which separates graalgo and gc entries in a shared build cache.
//
// Skeleton limits, each failing loudly rather than silently: the IR is the
// frontend/v2 document (so only the constructs its emitter supports
// compile), packages compiled with -std get no IR (the stdlib fork of
// design §5 does not exist yet), and response files are not expanded.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/encode"
	"graalgo-frontend-v2/parser"
	"graalgo-toolexec/archive"
	"graalgo-toolexec/bundle"
)

// irVersion is part of the -V=full answer: bumping it invalidates every
// cached archive that carries IR in an older format.
const irVersion = "v2doc1"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go build -toolexec graalgo-tool ... (graalgo-tool <tool> <args>)")
		os.Exit(2)
	}
	tool, args := os.Args[1], os.Args[2:]
	// cmd/go probes the host C compiler as a bare name; exec does no PATH
	// lookup for us.
	if !strings.ContainsRune(tool, filepath.Separator) {
		p, err := exec.LookPath(tool)
		if err != nil {
			fatalf("%v", err)
		}
		tool = p
	}
	logInvocation(tool, args)
	name := filepath.Base(tool)
	switch {
	case len(args) == 1 && args[0] == "-V=full":
		versionProbe(tool, name)
	case name == "compile":
		compile(tool, args)
	case name == "link":
		link(args)
	default:
		os.Exit(run(tool, args))
	}
}

// logInvocation appends one line per tool run to $GRAALGO_TOOL_LOG, which
// the tests use to observe what cmd/go invoked.
func logInvocation(tool string, args []string) {
	path := os.Getenv("GRAALGO_TOOL_LOG")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fatalf("%v", err)
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", filepath.Base(tool), strings.Join(args, " "))
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "graalgo-tool: "+format+"\n", a...)
	os.Exit(2)
}

// run executes the stock tool with stdio inherited and returns its exit
// status.
func run(tool string, args []string) int {
	cmd := exec.Command(tool, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.ExitCode()
	default:
		fatalf("%v", err)
		return 2
	}
}

// versionProbe answers "<tool> -V=full". cmd/go hashes the whole line into
// its action IDs for release toolchains.
func versionProbe(tool, name string) {
	out, err := exec.Command(tool, "-V=full").Output()
	if err != nil {
		fatalf("%s -V=full: %v", name, err)
	}
	line := strings.TrimSpace(string(out))
	files := []string{selfPath()}
	if name == "link" {
		// The stub is part of every linked binary's content.
		files = append(files, stubPath())
	}
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fatalf("%v", err)
		}
		h.Write(data)
	}
	fmt.Printf("%s graalgo-%s-%x\n", line, irVersion, h.Sum(nil)[:8])
}

func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		fatalf("%v", err)
	}
	return p
}

// stubPath locates the launcher stub: $GRAALGO_STUB, else "graalgo-stub"
// next to this executable.
func stubPath() string {
	if p := os.Getenv("GRAALGO_STUB"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(selfPath()), "graalgo-stub")
}

// flags is the subset of a tool's command line the wrapper interprets.
type flags struct {
	values map[string]string // -name value / -name=value
	bools  map[string]bool   // -name
	rest   []string          // trailing non-flag arguments
}

// parseFlags extracts the named value flags and boolean flags from args.
// Other flags are skipped; because their arity is unknown here, the
// positional arguments are taken to be the trailing run of arguments that
// do not start with "-", which is how cmd/go lays out compile and link
// command lines.
func parseFlags(args []string, valueFlags, boolFlags []string) flags {
	fl := flags{values: map[string]string{}, bools: map[string]bool{}}
	isValue := map[string]bool{}
	for _, v := range valueFlags {
		isValue[v] = true
	}
	isBool := map[string]bool{}
	for _, b := range boolFlags {
		isBool[b] = true
	}
	end := len(args)
	for end > 0 && !strings.HasPrefix(args[end-1], "-") {
		end--
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "@") {
			fatalf("response files are not supported by this skeleton (%s)", a)
		}
		if !strings.HasPrefix(a, "-") {
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch {
		case isValue[name] && hasVal:
			fl.values[name] = val
		case isValue[name] && i+1 < len(args):
			fl.values[name] = args[i+1]
			if i+1 >= end {
				end = i + 2
			}
			i++
		case isBool[name]:
			fl.bools[name] = true
		}
	}
	fl.rest = args[end:]
	return fl
}

// compileValueFlags are the cmd/compile flags that take a value as a
// separate argument when cmd/go passes them; parseFlags needs the full list
// to tell a flag's value from the first source file.
var compileValueFlags = []string{
	"o", "p", "importcfg", "lang", "trimpath", "embedcfg", "coveragecfg",
	"asmhdr", "symabis", "buildid", "goversion", "D", "I", "c", "d",
	"pgoprofile", "linkobj", "installsuffix", "json", "env", "spectre",
	"gendwarfinl", "bench", "blockprofile", "cpuprofile", "memprofile",
	"mutexprofile", "traceprofile", "goexperiment",
}

func compile(tool string, args []string) {
	// Shadow compile: gc produces the archive, export data, diagnostics.
	if code := run(tool, args); code != 0 {
		os.Exit(code)
	}
	fl := parseFlags(args, compileValueFlags, []string{"std", "race"})
	if fl.bools["std"] {
		return // no IR for the unforked standard library in the skeleton
	}
	out, pkgPath := fl.values["o"], fl.values["p"]
	if out == "" || pkgPath == "" || len(fl.rest) == 0 {
		fatalf("compile: cannot interpret command line (need -o, -p and source files)")
	}
	for _, f := range fl.rest {
		if !strings.HasSuffix(f, ".go") {
			fatalf("%s: compile: unexpected positional argument %q", pkgPath, f)
		}
	}
	for _, unsupported := range []string{"embedcfg", "coveragecfg"} {
		if fl.values[unsupported] != "" {
			fatalf("%s: -%s is not supported by this skeleton", pkgPath, unsupported)
		}
	}
	if fl.bools["race"] {
		fatalf("%s: the graalgo backend has no race detector", pkgPath)
	}
	ir, err := emitIR(pkgPath, fl.values["lang"], fl.values["importcfg"], fl.rest)
	if err != nil {
		fatalf("%s: %v", pkgPath, err)
	}
	if err := archive.Append(out, archive.IRMember, ir); err != nil {
		fatalf("%s: %v", pkgPath, err)
	}
}

// readImportcfg parses an importcfg file into import path -> archive, with
// importmap entries resolved.
func readImportcfg(path string) (map[string]string, error) {
	pkgFile := map[string]string{}
	if path == "" {
		return pkgFile, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	importMap := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		verb, rest, _ := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		before, after, ok := strings.Cut(rest, "=")
		if !ok {
			continue // comments, blank lines, modinfo
		}
		switch verb {
		case "packagefile":
			pkgFile[before] = after
		case "importmap":
			importMap[before] = after
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for from, to := range importMap {
		if file, ok := pkgFile[to]; ok {
			pkgFile[from] = file
		}
	}
	return pkgFile, nil
}

// emitIR type-checks the package with go/types, importing dependencies
// from the gc export data named by importcfg, and returns its encoded IR.
func emitIR(pkgPath, lang, importcfg string, files []string) ([]byte, error) {
	pkgFile, err := readImportcfg(importcfg)
	if err != nil {
		return nil, err
	}
	pres, err := parser.ParseFiles(files)
	if err != nil {
		return nil, err
	}
	lookup := func(path string) (io.ReadCloser, error) {
		file, ok := pkgFile[path]
		if !ok {
			return nil, fmt.Errorf("no packagefile for %q in importcfg", path)
		}
		return os.Open(file)
	}
	astFiles := make([]*ast.File, 0, len(pres.Files))
	for _, name := range pres.Files {
		astFiles = append(astFiles, pres.Pkg.Files[name])
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Scopes:     map[ast.Node]*types.Scope{},
	}
	conf := types.Config{
		Importer:  importer.ForCompiler(pres.Fset, "gc", lookup),
		GoVersion: lang,
		Sizes:     types.SizesFor("gc", "amd64"),
	}
	tpkg, err := conf.Check(pkgPath, pres.Fset, astFiles, info)
	if err != nil {
		// gc accepted these files; a go/types rejection is a wrapper bug.
		return nil, fmt.Errorf("internal: go/types disagrees with gc: %v", err)
	}
	doc, err := emit.Emit(&checker.Result{Pkg: tpkg, Info: info, Fset: pres.Fset}, pres.Pkg, pres.Files)
	if err != nil {
		return nil, err
	}
	return encode.Encode(doc)
}

func link(args []string) {
	fl := parseFlags(args, []string{"o", "importcfg", "buildid", "buildmode", "X", "extld", "extldflags", "linkmode", "installsuffix", "r", "E", "H", "I", "L", "T", "B", "k", "tmpdir", "pluginpath", "libgcc", "fipso", "randlayout", "compressdwarf"}, []string{"race", "msan", "asan"})
	out := fl.values["o"]
	if out == "" || len(fl.rest) != 1 {
		fatalf("link: cannot interpret command line (need -o and one main archive)")
	}
	if mode := fl.values["buildmode"]; mode != "" && mode != "exe" && mode != "pie" {
		fatalf("link: -buildmode=%s is not supported by the graalgo backend", mode)
	}
	if fl.bools["race"] || fl.bools["msan"] || fl.bools["asan"] {
		fatalf("link: the graalgo backend has no race detector or sanitizers")
	}
	pkgFile, err := readImportcfg(fl.values["importcfg"])
	if err != nil {
		fatalf("link: %v", err)
	}
	mainArchive := fl.rest[0]
	paths := make([]string, 0, len(pkgFile))
	for p := range pkgFile {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	b := &bundle.Bundle{Runner: os.Getenv("GRAALGO_RUNNER")}
	seenMain := false
	for _, p := range paths {
		ir, err := archive.Read(pkgFile[p], archive.IRMember)
		if errors.Is(err, archive.ErrNoMember) {
			continue // -std package (skeleton) or a package built by plain gc
		}
		if err != nil {
			fatalf("link: %s: %v", p, err)
		}
		if pkgFile[p] == mainArchive {
			seenMain = true
			b.Main = p
		}
		b.Packages = append(b.Packages, bundle.Package{ImportPath: p, IR: ir})
	}
	if !seenMain {
		// cmd/go lists the main package in importcfg.link; be robust if
		// it does not.
		ir, err := archive.Read(mainArchive, archive.IRMember)
		if err != nil {
			fatalf("link: main package archive %s: %v (was it compiled through graalgo-tool?)", mainArchive, err)
		}
		b.Main = "main"
		b.Packages = append(b.Packages, bundle.Package{ImportPath: "main", IR: ir})
	}
	stub, err := os.ReadFile(stubPath())
	if err != nil {
		fatalf("link: launcher stub: %v", err)
	}
	if id := fl.values["buildid"]; id != "" {
		placeholder := []byte(bundle.BuildIDPlaceholder)
		if len(id) != len(placeholder) {
			fatalf("link: build ID is %d bytes, the stub reserves %d", len(id), len(placeholder))
		}
		if !bytes.Contains(stub, placeholder) {
			fatalf("link: launcher stub %s was not linked with the placeholder build ID", stubPath())
		}
		stub = bytes.ReplaceAll(stub, placeholder, []byte(id))
	}
	enc := bundle.Encode(b)
	file := append(append(stub, enc...), bundle.Trailer(int64(len(stub)), int64(len(enc)))...)
	if err := os.WriteFile(out, file, 0o755); err != nil {
		fatalf("link: %v", err)
	}
}
