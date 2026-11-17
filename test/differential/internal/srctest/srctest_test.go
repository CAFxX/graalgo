package srctest

import (
	"os"
	"path/filepath"
	"testing"

	"diffharness/internal/typeparam"
)

// writeFile stages one testdata file under dir.
func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// classifyOne runs Scan over a single-file dir and returns its class/detail.
func classifyOne(t *testing.T, name, body string) (class, detail string) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, name, body)
	files, err := Scan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %d", len(files))
	}
	return files[0].Class, files[0].Detail
}

func TestClassifyRun(t *testing.T) {
	class, _ := classifyOne(t, "hello.go", `package main

import "fmt"

func main() { fmt.Println("hi") }
`)
	if class != "run" {
		t.Fatalf("want run, got %q", class)
	}
}

func TestClassifyRunNoImports(t *testing.T) {
	class, _ := classifyOne(t, "noimp.go", `package main

func main() { println("hi") }
`)
	if class != "run" {
		t.Fatalf("want run, got %q", class)
	}
}

func TestClassifyNotMain(t *testing.T) {
	class, detail := classifyOne(t, "lib.go", `package foo

func Foo() {}
`)
	if class != "notrun" {
		t.Fatalf("want notrun, got %q", class)
	}
	if detail == "" {
		t.Fatal("want a detail reason")
	}
}

func TestClassifyNoFuncMain(t *testing.T) {
	class, _ := classifyOne(t, "nomain.go", `package main

func helper() {}
`)
	if class != "notrun" {
		t.Fatalf("want notrun, got %q", class)
	}
}

func TestClassifyIgnoreConstraint(t *testing.T) {
	class, _ := classifyOne(t, "ign.go", `//go:build ignore

package main

func main() {}
`)
	if class != "notrun" {
		t.Fatalf("want notrun, got %q", class)
	}
}

func TestClassifyUnsatisfiableConstraint(t *testing.T) {
	class, _ := classifyOne(t, "win.go", `//go:build windows

package main

func main() {}
`)
	if class != "notrun" {
		t.Fatalf("want notrun, got %q", class)
	}
}

func TestClassifySatisfiableConstraint(t *testing.T) {
	class, _ := classifyOne(t, "lin.go", `//go:build linux && amd64

package main

func main() {}
`)
	if class != "run" {
		t.Fatalf("want run, got %q", class)
	}
}

func TestClassifyCgo(t *testing.T) {
	class, _ := classifyOne(t, "cgo.go", `package main

import "C"

func main() {}
`)
	// No automatic cgo exclusion: it runs, and a divergence must be listed
	// explicitly in srctest-exclusions.yaml.
	if class != "run" {
		t.Fatalf("want run, got %q", class)
	}
}

func TestClassifyStdlibImport(t *testing.T) {
	class, detail := classifyOne(t, "strs.go", `package main

import "strings"

func main() { _ = strings.ToUpper("a") }
`)
	// No automatic import-based exclusion (the product launcher runs the
	// stdlib): stdlib-importing programs are compared.
	if class != "run" {
		t.Fatalf("want run, got %q (detail %q)", class, detail)
	}
}

func TestClassifyVersionTag(t *testing.T) {
	// go1.26 is satisfied by the go1.27 toolchain; go1.28 is not.
	class, _ := classifyOne(t, "v26.go", "//go:build go1.26\n\npackage main\n\nfunc main() {}\n")
	if class != "run" {
		t.Fatalf("go1.26: want run, got %q", class)
	}
	class, _ = classifyOne(t, "v28.go", "//go:build go1.28\n\npackage main\n\nfunc main() {}\n")
	if class != "notrun" {
		t.Fatalf("go1.28: want notrun, got %q", class)
	}
}

func TestSatisfiableExpr(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"linux", true},
		{"windows", false},
		{"linux && amd64", true},
		{"linux && arm64", false},
		{"!windows", true},
		{"!(linux && amd64)", false},
		{"linux || windows", true},
		{"plan9 || windows", false},
		{"gc", true},
		{"gccgo", false},
		{"go1.27", true},
		{"go1.21", true},
		{"ignore", false},
		{"cgo", false},
		{"linux && (amd64 || arm64)", true},
	}
	for _, c := range cases {
		if got := satisfiable(c.expr); got != c.want {
			t.Errorf("satisfiable(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestYamlExclusionOverrides(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hello.go", "package main\n\nfunc main() {}\n")
	excl := map[string]typeparam.Exclusion{
		"hello.go": {File: "hello.go", Reason: "deferred-parity", Detail: "test"},
	}
	files, err := Scan(dir, excl)
	if err != nil {
		t.Fatal(err)
	}
	// A listed file still runs (class unchanged) and carries the reason:
	// the run verdict is EXPECTED while it diverges, STALE once it matches.
	if files[0].Class != "run" || files[0].Expected != "deferred-parity" {
		t.Fatalf("want run+expected=deferred-parity, got %q/%q", files[0].Class, files[0].Expected)
	}
}

func TestScanSkipsSubdirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package main\n\nfunc main() {}\n")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "sub/b.go", "package main\n\nfunc main() {}\n")
	files, err := Scan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "a.go" {
		t.Fatalf("want only top-level a.go, got %v", files)
	}
}

func TestClassifyNegativeSyntax(t *testing.T) {
	// Intentionally invalid Go (bad escape): build-failure parity, not exclusion.
	class, _ := classifyOne(t, "char_lit1.go", "package main\n\nvar x = '\\x'\n\nfunc main() {}\n")
	if class != "errorcheck" {
		t.Fatalf("want errorcheck, got %q", class)
	}
}

func TestClassifyConcurrency(t *testing.T) {
	cases := []struct{ name, body string }{
		{"chan.go", "package main\n\nfunc main() {\n\tch := make(chan int)\n\t_ = ch\n}\n"},
		{"go.go", "package main\n\nfunc main() {\n\tgo func() {}()\n}\n"},
		{"select.go", "package main\n\nfunc main() {\n\tselect {}\n}\n"},
		{"send.go", "package main\n\nfunc f(ch chan int) {\n\tch <- 1\n}\n\nfunc main() {}\n"},
		{"recv.go", "package main\n\nfunc f(ch chan int) int {\n\treturn <-ch\n}\n\nfunc main() {}\n"},
	}
	for _, c := range cases {
		class, _ := classifyOne(t, c.name, c.body)
		// No automatic concurrency exclusion: the product launcher has
		// the scheduler.
		if class != "run" {
			t.Errorf("%s: want run, got %q", c.name, class)
		}
	}
}

func TestClassifyNoFalseConcurrency(t *testing.T) {
	// A plain program with no concurrency stays run.
	class, _ := classifyOne(t, "plain.go", "package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n")
	if class != "run" {
		t.Fatalf("want run, got %q", class)
	}
}
