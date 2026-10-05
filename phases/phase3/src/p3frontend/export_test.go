package p3frontend

import (
	"encoding/json"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns the graalgo repo root (four levels above this file's dir).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

type cmdResult struct {
	stdout string
	stderr string
	code   int
}

func runCommand(t *testing.T, dir, name string, args ...string) cmdResult {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("running %s %v: %v", name, args, err)
		}
	}
	return cmdResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// copyDir copies src (a directory tree) into a fresh temp dir.
func copyDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func goTool(t *testing.T) string {
	t.Helper()
	p := filepath.Join(os.Getenv("HOME"), "tools", "go", "bin", "go")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("go toolchain not found at %s", p)
	}
	return p
}

// graalgoRun executes the merged single-file program with the Phase-2
// launcher (make -s run ARGS="--gmp=1 <file>"); -s keeps make's own echo
// out of stdout so it stays byte-comparable with gc's.
func graalgoRun(t *testing.T, mergedPath string) cmdResult {
	t.Helper()
	phase2 := filepath.Join(repoRoot(t), "phases", "phase2")
	return runCommand(t, phase2, "make", "-s", "run", "ARGS=--gmp=1 "+mergedPath)
}

// normalizeDoc unmarshals a frontend/v1 document and normalizes the
// package.files basenames (ExportFile records the input basename,
// ExportDir the fixed merged basename).
func normalizeDoc(t *testing.T, doc string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("unmarshaling doc: %v", err)
	}
	if pkg, ok := v["package"].(map[string]any); ok {
		pkg["files"] = []any{"x.go"}
	}
	return v
}

// TestSyntheticBootstrapRetained verifies: GG-0012 — a single-file program
// importing the synthetic runtime+fmt packages produces a deep-equal
// frontend/v1 document through the new ExportDir path and through the
// frozen ExportFile directly. The bootstrap still works.
func TestSyntheticBootstrapRetained(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import (
	"fmt"
	"runtime"
)

func main() {
	runtime.GC()
	fmt.Println("bootstrap", fmt.Sprint(40+2))
}
`
	// gofmt first: the flattener re-emits via go/format, so the merged
	// source must start from canonical formatting for a byte-level match.
	formatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, formatted, 0644); err != nil {
		t.Fatal(err)
	}

	want, err := runGofrontend(path)
	if err != nil {
		t.Fatalf("runGofrontend: %v", err)
	}
	got, err := ExportDir(dir)
	if err != nil {
		t.Fatalf("ExportDir: %v", err)
	}
	if !reflect.DeepEqual(normalizeDoc(t, want), normalizeDoc(t, got)) {
		t.Fatalf("ExportDir doc != ExportFile doc:\n--- ExportFile ---\n%s\n--- ExportDir ---\n%s", want, got)
	}
}

// TestTwoPackageDifferential verifies: GG-0012 — a two-package program
// (main + helper, the latter with a linux-tagged file and a windows-suffixed
// file that must be excluded) runs byte-identically under stock gc
// (go run on the original tree) and under graalgo-run (on the merged file):
// same stdout bytes, same exit code. The panic variant must produce the
// identical panic line and exit code 2 on both.
func TestTwoPackageDifferential(t *testing.T) {
	twopkg := filepath.Join("testdata", "twopkg")
	mainDir := filepath.Join(twopkg, "main")

	merged, err := MergeDir(mainDir)
	if err != nil {
		t.Fatalf("MergeDir: %v", err)
	}
	mergedPath := filepath.Join(t.TempDir(), "merged.go")
	if err := os.WriteFile(mergedPath, []byte(merged), 0644); err != nil {
		t.Fatal(err)
	}
	// The merged file must also pass the frozen exporter end to end.
	if _, err := ExportDir(mainDir); err != nil {
		t.Fatalf("ExportDir: %v", err)
	}

	gc := runCommand(t, twopkg, goTool(t), "run", "./main")
	gr := graalgoRun(t, mergedPath)
	if gc.code != gr.code || gc.stdout != gr.stdout {
		t.Fatalf("gc divergence (normal variant):\n gc exit=%d stdout=%q stderr=%q\n gr exit=%d stdout=%q stderr=%q",
			gc.code, gc.stdout, gc.stderr, gr.code, gr.stdout, gr.stderr)
	}
	t.Logf("normal variant: %d stdout bytes, exit %d, identical", len(gc.stdout), gc.code)

	// Panic variant: helper.Boom is in the panic path. Note: `go run`
	// masks the program's exit code (it exits 1 itself), so the gc leg
	// builds once and runs the binary directly for a true code comparison.
	pdir := copyDir(t, twopkg)
	panicMain := `package main

import (
	"example.com/twopkg/helper"
	"fmt"
)

func main() {
	fmt.Println("before panic")
	helper.Boom()
	fmt.Println("after panic")
}
`
	if err := os.WriteFile(filepath.Join(pdir, "main", "main.go"), []byte(panicMain), 0644); err != nil {
		t.Fatal(err)
	}
	merged2, err := MergeDir(filepath.Join(pdir, "main"))
	if err != nil {
		t.Fatalf("MergeDir (panic variant): %v", err)
	}
	mergedPath2 := filepath.Join(t.TempDir(), "merged_panic.go")
	if err := os.WriteFile(mergedPath2, []byte(merged2), 0644); err != nil {
		t.Fatal(err)
	}
	progPath := filepath.Join(t.TempDir(), "panicprog")
	buildRes := runCommand(t, pdir, goTool(t), "build", "-o", progPath, "./main")
	if buildRes.code != 0 {
		t.Fatalf("go build (panic variant): exit=%d stderr=%q", buildRes.code, buildRes.stderr)
	}
	gcP := runCommand(t, pdir, progPath)
	grP := graalgoRun(t, mergedPath2)
	gcLine, grLine := firstPanicLine(gcP.stderr), firstPanicLine(grP.stderr)
	if gcP.code != grP.code || gcLine == "" || gcLine != grLine {
		t.Fatalf("gc divergence (panic variant):\n gc exit=%d panic=%q stderr=%q\n gr exit=%d panic=%q stderr=%q",
			gcP.code, gcLine, gcP.stderr, grP.code, grLine, grP.stderr)
	}
	t.Logf("panic variant: exit %d, panic line %q, identical", gcP.code, gcLine)
}

func firstPanicLine(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "panic: ") {
			return line
		}
	}
	return ""
}

// TestNoLanguagePinning verifies: GG-0013 — a //go:build go1.27 file is
// included by the evaluator (no version gating on build tags) and a
// go 1.27 module using generics (frontend/v1 supports them since M6)
// passes ExportDir.
func TestNoLanguagePinning(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod": "module example.com/langpin\n\ngo 1.27\n",
		"generic.go": `//go:build go1.27

package main

// Map applies f to every element.
func Map[T any](s []T, f func(T) T) []T {
	out := make([]T, len(s))
	for i, v := range s {
		out[i] = f(v)
	}
	return out
}
`,
		"main.go": `package main

import "fmt"

func main() {
	doubled := Map([]int{1, 2, 3}, func(x int) int { return 2 * x })
	fmt.Println(doubled)
}
`,
	})

	files, err := NewLoader().SelectFiles(dir)
	if err != nil {
		t.Fatalf("SelectFiles: %v", err)
	}
	found := false
	for _, f := range files {
		if f == "generic.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("//go:build go1.27 file excluded by evaluator: %v", files)
	}

	doc, err := ExportDir(dir)
	if err != nil {
		t.Fatalf("ExportDir (generics, go 1.27 module): %v", err)
	}
	if !strings.Contains(doc, "Map") {
		t.Fatalf("expected Map in exported doc")
	}
	t.Logf("generics doc: %d bytes", len(doc))
}

// TestDeterministic: ExportDir twice on the same input yields
// byte-identical output.
func TestDeterministic(t *testing.T) {
	mainDir := filepath.Join("testdata", "twopkg", "main")
	a, err := ExportDir(mainDir)
	if err != nil {
		t.Fatalf("ExportDir #1: %v", err)
	}
	b, err := ExportDir(mainDir)
	if err != nil {
		t.Fatalf("ExportDir #2: %v", err)
	}
	if a != b {
		t.Fatalf("ExportDir is not deterministic: %d vs %d bytes", len(a), len(b))
	}
}

// TestEmbeddedFieldRebinding verifies P0-1: an embedded field's type
// identifier must track the renamed dependency type through (a) the field
// declaration, (b) selectors naming the embedded field (o.T, including
// promotion through two levels), and (c) keyed composite-literal elements
// (Outer{T: ...}). The fixture collides helper's T with main's own T: a
// rebinding bug is SILENT (both legs exit 0; gc prints h.T, the broken
// merge prints main.T), so the test asserts the exact expected output in
// addition to gc-vs-graalgo byte equality.
func TestEmbeddedFieldRebinding(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod": "module example.com/embed\n\ngo 1.27\n",
		"h/h.go": `package h

type T struct{ X int }

func (t T) Who() string { return "h.T" }

type Outer struct{ T }

type Agg struct{ Outer }
`,
		"main.go": `package main

import (
	"example.com/embed/h"
	"fmt"
)

type T struct{ X int }

func (t T) Who() string { return "main.T" }

func main() {
	o := h.Outer{}
	fmt.Println(o.T.Who()) // (a)+(b): embedded-field selector
	fmt.Println(o.X)       // (b): promoted field selector
	a := h.Agg{Outer: h.Outer{T: h.T{X: 7}}}
	fmt.Println(a.Outer.T.Who(), a.Outer.X) // (c): keyed literal, two-level promotion
	var t T
	fmt.Println(t.Who()) // main's own T is untouched
}
`,
	})

	merged, err := MergeDir(dir)
	if err != nil {
		t.Fatalf("MergeDir: %v", err)
	}
	mergedPath := filepath.Join(t.TempDir(), "merged_embed.go")
	if err := os.WriteFile(mergedPath, []byte(merged), 0644); err != nil {
		t.Fatal(err)
	}
	progPath := filepath.Join(t.TempDir(), "embedprog")
	if res := runCommand(t, dir, goTool(t), "build", "-o", progPath, "."); res.code != 0 {
		t.Fatalf("go build: exit=%d stderr=%q", res.code, res.stderr)
	}
	gc := runCommand(t, dir, progPath)
	gr := graalgoRun(t, mergedPath)
	const want = "h.T\n0\nh.T 7\nmain.T\n"
	if gc.stdout != want {
		t.Fatalf("gc oracle moved: stdout=%q want=%q", gc.stdout, want)
	}
	if gr.code != gc.code || gr.stdout != gc.stdout {
		t.Fatalf("gc divergence (embedded fields):\n gc exit=%d stdout=%q stderr=%q\n gr exit=%d stdout=%q stderr=%q",
			gc.code, gc.stdout, gc.stderr, gr.code, gr.stdout, gr.stderr)
	}
	t.Logf("embedded-field rebinding: gc and graalgo agree on %q", want)
}
