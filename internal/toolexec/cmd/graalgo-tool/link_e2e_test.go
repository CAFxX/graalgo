package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-toolexec/bundle"
)

func (e *env) write(name, content string) {
	e.t.Helper()
	p := filepath.Join(e.moduleDir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// Design §5.3: the link binds body-less functions by symbol name — to a Go
// body another package pushes with //go:linkname, or to an intrinsic of the
// registry — unifies linknamed variables, and records all of it in the
// bundle.
func TestLinkResolvesSymbols(t *testing.T) {
	t.Parallel()
	e := setup(t)
	e.write("main.go", `package main

import (
	"example.com/skel/lib"
	"math/bits"
	_ "unsafe"
)

//go:linkname nanotime runtime.nanotime
func nanotime() int64

func pushed(x int) int

func main() {
	println(lib.Answer(), nanotime() > 0, pushed(1), bits.Len(8))
}
`)
	e.write("stub.s", "")
	e.write("lib/lib.go", `package lib

import _ "unsafe"

func Answer() int {
	return 42
}

//go:linkname impl main.pushed
func impl(x int) int {
	return x + 1
}
`)
	exe := filepath.Join(t.TempDir(), "skel")
	if out, err := e.build(exe); err != nil {
		t.Fatalf("go build -toolexec: %v\n%s", err, out)
	}
	b, err := bundle.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	funcs := map[string]bundle.FuncBinding{}
	for _, f := range b.Resolution.Funcs {
		funcs[f.Pkg+"."+f.Name] = f
	}
	if f := funcs["main.nanotime"]; f.Intrinsic != "runtime.nanotime" {
		t.Errorf("main.nanotime bound to %+v, want the intrinsic runtime.nanotime", f)
	}
	if f := funcs["runtime.nanotime"]; f.Intrinsic != "runtime.nanotime" {
		t.Errorf("runtime.nanotime bound to %+v, want the intrinsic", f)
	}
	if f := funcs["main.pushed"]; f.Intrinsic != "" || f.TargetPkg != "example.com/skel/lib" || f.TargetName != "impl" {
		t.Errorf("main.pushed bound to %+v, want the Go body lib.impl", f)
	}
	vars := map[string]bundle.VarAlias{}
	for _, v := range b.Resolution.Vars {
		vars[v.Pkg+"."+v.Name] = v
	}
	if v := vars["math/bits.overflowError"]; v.TargetPkg != "runtime" || v.TargetName != "overflowError" {
		t.Errorf("math/bits.overflowError aliases %+v, want runtime.overflowError", v)
	}
}

// Design §5.3 rule 3: a reachable function with neither a Go body nor an
// intrinsic fails the link, naming the symbol; an unreachable one does not.
func TestLinkFailsOnReachableUndefined(t *testing.T) {
	t.Parallel()
	e := setup(t)
	src := `package lib

func Answer() int {
	return 42
}

func missing() int

func Unused() int {
	return missing()
}
`
	e.write("lib/lib.go", src)
	e.write("lib/stub.s", "")
	exe := filepath.Join(t.TempDir(), "skel")
	if out, err := e.build(exe); err != nil {
		t.Fatalf("an unreachable undefined function failed the build: %v\n%s", err, out)
	}
	e.write("lib/lib.go", strings.Replace(src, "return 42", "return Unused()", 1))
	os.Remove(exe)
	out, err := e.build(exe)
	if err == nil {
		t.Fatalf("the build succeeded with a reachable undefined function")
	}
	want := "graalgo-tool: link: undefined: example.com/skel/lib.missing (no Go body and no graalgo intrinsic), referenced by example.com/skel/lib.Unused"
	if !strings.Contains(out, want) {
		t.Errorf("link error does not name the symbol:\n%s\nwant: %s", out, want)
	}
	if _, err := os.Stat(exe); err == nil {
		t.Errorf("a failed link wrote an executable")
	}
}

// Without the fork (an upstream runtime), the link fails rather than
// producing a program that cannot run: the runtime's assembly and
// compiler-provided functions have no intrinsics.
func TestLinkAgainstUpstreamRuntimeFails(t *testing.T) {
	t.Parallel()
	e := setup(t)
	os.Remove(e.toolLog)
	out, err := goCmd(t, e.moduleDir, []string{
		"GOCACHE=" + e.cache, "GOFLAGS=", "CGO_ENABLED=0", "GRAALGO_RUNNER=" + e.fakeRun,
	}, "build", "-toolexec", e.tool, "-o", filepath.Join(t.TempDir(), "skel"), ".")
	if err == nil {
		t.Fatalf("linked against the unforked runtime")
	}
	if !strings.Contains(out, "graalgo-tool: link: undefined: internal/runtime/atomic.Cas (no Go body and no graalgo intrinsic)") || !strings.Contains(out, "is the standard library graalgo's fork?") {
		t.Errorf("unexpected failure:\n%s", out)
	}
}
