package girinterp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"graalgo-toolexec/emit"
	"graalgo-toolexec/ir"
)

var only = flag.String("only", "", "run only programs whose name contains this")

// program is one differential test program: a single file (package main,
// importing nothing) or a module directory (go.mod + packages).
type program struct {
	name string
	file string // single file
	dir  string // module directory
}

const corpusDir = "../../../test/corpus"

// corpusPrograms lists the corpus programs that import nothing (the M1a
// gate set of toolexec-design.md §8).
func corpusPrograms(t *testing.T) []program {
	var out []program
	err := filepath.Walk(corpusDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil || len(f.Imports) > 0 || f.Name.Name != "main" {
			return nil
		}
		rel, _ := filepath.Rel(corpusDir, path)
		out = append(out, program{name: "corpus/" + rel, file: path})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// m1aPrograms lists test/m1a/programs, the M1a gate's own programs
// (test/m1a/run.sh runs them on the node package): the reference
// interpreter must agree with gc on them too — except programs with go
// statements, which it does not run (it has no scheduler).
func m1aPrograms(t *testing.T) []program {
	files, err := filepath.Glob("../../../test/m1a/programs/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no test/m1a/programs (%v)", err)
	}
	var out []program
	for _, f := range files {
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		goStmt := false
		ast.Inspect(af, func(n ast.Node) bool {
			_, ok := n.(*ast.GoStmt)
			goStmt = goStmt || ok
			return !goStmt
		})
		if !goStmt {
			out = append(out, program{name: "m1a/" + filepath.Base(f), file: f})
		}
	}
	return out
}

// ownPrograms lists testdata/progs: *.go files and module directories.
func ownPrograms(t *testing.T) []program {
	ents, err := os.ReadDir("testdata/progs")
	if err != nil {
		t.Fatal(err)
	}
	var out []program
	for _, e := range ents {
		p := filepath.Join("testdata/progs", e.Name())
		switch {
		case e.IsDir():
			out = append(out, program{name: "progs/" + e.Name(), dir: p})
		case strings.HasSuffix(e.Name(), ".go"):
			out = append(out, program{name: "progs/" + e.Name(), file: p})
		}
	}
	return out
}

// result is what a run produced.
type result struct {
	stdout, stderr string
	code           int
}

// ---------------------------------------------------------------- gc side

var gcCache sync.Map // source hash -> result

func sourceHash(p program) string {
	h := sha256.New()
	add := func(path string) {
		data, _ := os.ReadFile(path)
		h.Write([]byte(path))
		h.Write(data)
	}
	if p.file != "" {
		add(p.file)
	} else {
		filepath.Walk(p.dir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				add(path)
			}
			return nil
		})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// runGC builds the program with the stock toolchain and runs it.
func runGC(t *testing.T, p program) result {
	key := sourceHash(p)
	if r, ok := gcCache.Load(key); ok {
		return r.(result)
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "prog")
	var cmd *exec.Cmd
	if p.file != "" {
		abs, _ := filepath.Abs(p.file)
		cmd = exec.Command("go", "build", "-o", bin, abs)
		cmd.Dir = tmp
	} else {
		cmd = exec.Command("go", "build", "-o", bin, ".")
		cmd.Dir = p.dir
	}
	cmd.Env = append(os.Environ(), "GOFLAGS=", "CGO_ENABLED=0", "GO111MODULE=on")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: gc build: %v\n%s", p.name, err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	run := exec.CommandContext(ctx, bin)
	run.Dir = tmp
	var so, se bytes.Buffer
	run.Stdout, run.Stderr = &so, &se
	err := run.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("%s: run: %v", p.name, err)
	}
	r := result{so.String(), se.String(), code}
	gcCache.Store(key, r)
	return r
}

// ---------------------------------------------------------------- GIR side

// mutate, when set, rewrites each emitted file before encoding (the
// mutation test).
type emitHook func(*ir.File)

// emitProgram type-checks and emits every package of p and returns the
// GIR files after an encode/decode round trip.
func emitProgram(p program, hook emitHook) ([]*ir.File, error) {
	fset := token.NewFileSet()
	pkgFiles := map[string][]string{} // import path -> files
	modPath := ""
	if p.file != "" {
		pkgFiles["main"] = []string{p.file}
	} else {
		data, err := os.ReadFile(filepath.Join(p.dir, "go.mod"))
		if err != nil {
			return nil, err
		}
		for _, l := range strings.Split(string(data), "\n") {
			if f := strings.Fields(l); len(f) == 2 && f[0] == "module" {
				modPath = f[1]
			}
		}
		err = filepath.Walk(p.dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			rel, _ := filepath.Rel(p.dir, filepath.Dir(path))
			ip := modPath
			if rel != "." {
				ip += "/" + filepath.ToSlash(rel)
			}
			pkgFiles[ip] = append(pkgFiles[ip], path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	type checked struct {
		pkg   *types.Package
		info  *types.Info
		files []*ast.File
	}
	done := map[string]*checked{}
	var order []string
	var check func(ip string) (*types.Package, error)
	imp := importerFunc(func(path string) (*types.Package, error) {
		if _, ok := pkgFiles[path]; ok {
			return check(path)
		}
		return nil, fmt.Errorf("import of %q (only the program's own packages are available)", path)
	})
	check = func(ip string) (*types.Package, error) {
		if c := done[ip]; c != nil {
			return c.pkg, nil
		}
		names := pkgFiles[ip]
		sort.Strings(names)
		var files []*ast.File
		for _, n := range names {
			f, err := parser.ParseFile(fset, n, nil, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				return nil, err
			}
			files = append(files, f)
		}
		info := emit.NewInfo()
		conf := types.Config{Importer: imp, GoVersion: "go1.27", Sizes: types.SizesFor("gc", "amd64")}
		path := ip
		if files[0].Name.Name == "main" {
			path = "main"
		}
		pkg, err := conf.Check(path, fset, files, info)
		if err != nil {
			return nil, err
		}
		done[ip] = &checked{pkg, info, files}
		order = append(order, ip)
		return pkg, nil
	}
	var ips []string
	for ip := range pkgFiles {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	for _, ip := range ips {
		if _, err := check(ip); err != nil {
			return nil, err
		}
	}
	var out []*ir.File
	for _, ip := range order {
		c := done[ip]
		f, _, err := emit.Emit(emit.Config{Fset: fset, Pkg: c.pkg, Info: c.info, Files: c.files, Lang: "go1.27",
			GOOS: "linux", GOARCH: "amd64", FileName: filepath.Base})
		if err != nil {
			return nil, err
		}
		if hook != nil {
			hook(f)
		}
		data, err := ir.Encode(f)
		if err != nil {
			return nil, err
		}
		g, err := ir.DecodeValidate(data)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

type importerFunc func(string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// runGIR emits and interprets p.
func runGIR(p program, hook emitHook) (result, error) {
	files, err := emitProgram(p, hook)
	if err != nil {
		return result{}, err
	}
	var so, se bytes.Buffer
	in, err := New(files, &so, &se)
	if err != nil {
		return result{}, err
	}
	in.MaxSteps = 50_000_000
	code, err := in.Run()
	if err != nil {
		return result{stdout: so.String(), stderr: se.String()}, err
	}
	return result{so.String(), se.String(), code}, nil
}

// ---------------------------------------------------------------- comparison

var (
	hexRE     = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	funcNumRE = regexp.MustCompile(`\.func\d+((\.\d+)*)`)
	nestNumRE = regexp.MustCompile(`\.\d+`)
	// A gc throw prints the system-level header
	// "goroutine N gp=0x... m=... mp=0x... [reason]:" (as
	// test/corpus/lifecycle/normalize.py, both forms map to "goroutine N [").
	goroutRE   = regexp.MustCompile(`(?m)^goroutine \d+(?: gp=\S+ m=\S+(?: mp=\S+)?)? \[`)
	gorootPath = runtime.GOROOT()
)

// normalize maps a run's stderr to what is compared: the text before the
// goroutine trace verbatim (hex addresses masked), and from the trace
// the frames in program files as "name file:line" (closure numbering
// masked, runtime frames and compiler-generated wrappers dropped).
func normalize(stderr string) string {
	loc := goroutRE.FindStringIndex(stderr)
	if loc == nil {
		return hexRE.ReplaceAllString(stderr, "0xADDR")
	}
	head := hexRE.ReplaceAllString(stderr[:loc[0]], "0xADDR")
	lines := strings.Split(stderr[loc[0]:], "\n")
	var b strings.Builder
	b.WriteString(head)
	b.WriteString(goroutRE.ReplaceAllString(lines[0], "goroutine N [") + "\n")
	for i := 1; i+1 < len(lines); i += 2 {
		fn, loc := lines[i], strings.TrimSpace(lines[i+1])
		if fn == "" || !strings.HasPrefix(lines[i+1], "\t") {
			break
		}
		file := loc
		if j := strings.LastIndex(file, " +0x"); j >= 0 {
			file = file[:j]
		}
		// Compiler-generated wrappers (deferwrap; "<autogenerated>" method
		// wrappers, which gc shows only at the system traceback level of a
		// throw) have no source and are not modelled by the interpreter.
		if strings.HasPrefix(file, gorootPath) || strings.Contains(fn, "deferwrap") || file == "<autogenerated>:1" || strings.HasPrefix(fn, "panic(") {
			continue
		}
		if j := strings.LastIndex(fn, "("); j > 0 {
			fn = fn[:j]
		}
		if m := funcNumRE.FindStringSubmatchIndex(fn); m != nil {
			fn = fn[:m[0]] + ".func" + nestNumRE.ReplaceAllString(fn[m[2]:m[3]], ".N") + fn[m[1]:]
		}
		b.WriteString(fn + " " + filepath.Base(file) + "\n")
	}
	return b.String()
}

func compare(gc, gir result) string {
	var diffs []string
	if gc.code != gir.code {
		diffs = append(diffs, fmt.Sprintf("exit status %d, gc %d", gir.code, gc.code))
	}
	if gc.stdout != gir.stdout {
		diffs = append(diffs, fmt.Sprintf("stdout:\n%s\n--- gc:\n%s", clip(gir.stdout), clip(gc.stdout)))
	}
	if a, b := normalize(gir.stderr), normalize(gc.stderr); a != b {
		diffs = append(diffs, "stderr:\n"+firstDiff(a, b))
	}
	return strings.Join(diffs, "\n")
}

func clip(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "..."
	}
	return s
}

// firstDiff shows the first differing line with context.
func firstDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < max(len(g), len(w)); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			lo := max(0, i-3)
			return fmt.Sprintf("line %d:\n  gir: %q\n  gc:  %q\ncontext (gc):\n%s", i+1, gl, wl, strings.Join(w[lo:min(len(w), i+4)], "\n"))
		}
	}
	return ""
}

// ---------------------------------------------------------------- the test

// TestDifferential runs every program through gc and through
// emit -> encode -> decode -> girinterp and requires identical stdout,
// exit status and (normalized) stderr — println output, panic messages
// and the traceback's frames and lines.
func TestDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds every program with gc")
	}
	progs := append(append(corpusPrograms(t), ownPrograms(t)...), m1aPrograms(t)...)
	var mu sync.Mutex
	pass, total := 0, 0
	var failed []string
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, p := range progs {
		if *only != "" && !strings.Contains(p.name, *only) {
			continue
		}
		total++
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			gc := runGC(t, p)
			gir, err := runGIR(p, nil)
			msg := ""
			if err != nil {
				msg = "girinterp: " + err.Error()
			} else {
				msg = compare(gc, gir)
			}
			mu.Lock()
			defer mu.Unlock()
			if msg == "" {
				pass++
				return
			}
			failed = append(failed, p.name)
			t.Errorf("%s: %s", p.name, msg)
		}()
	}
	wg.Wait()
	sort.Strings(failed)
	corpus := 0
	for _, p := range progs {
		if strings.HasPrefix(p.name, "corpus/") {
			corpus++
		}
	}
	t.Logf("differential: %d/%d programs identical to gc (%d corpus programs that import nothing); failing: %s", pass, total, corpus, strings.Join(failed, " "))
}

var _ = strconv.Itoa
