package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestResolveFilesTagsFiltersTaggedFiles: -tags must change the file set
// a directory expands to (R1-1). Without the tag, the tagged file is
// excluded; with it, it is included.
func TestResolveFilesTagsFiltersTaggedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package foo\n\nfunc A() {}\n")
	writeFile(t, dir, "tagged.go", "//go:build mytag\n\npackage foo\n\nfunc Tagged() {}\n")

	untagged, err := resolveFilesTags([]string{dir}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range untagged {
		if strings.HasSuffix(f, "tagged.go") {
			t.Errorf("untagged resolveFilesTags included tagged.go: %v", untagged)
		}
	}
	tagged, err := resolveFilesTags([]string{dir}, "mytag")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range tagged {
		if strings.HasSuffix(f, "tagged.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("-tags=mytag resolveFilesTags excluded tagged.go: %v", tagged)
	}
	other, err := resolveFilesTags([]string{dir}, "othertag")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range other {
		if strings.HasSuffix(f, "tagged.go") {
			t.Errorf("-tags=othertag resolveFilesTags included tagged.go: %v", other)
		}
	}
}

// TestBuildableGoFilesTags is the same -tags contract for the test
// runner's file selection (R1-1, graalgo test path).
func TestBuildableGoFilesTags(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package foo\n\nfunc A() {}\n")
	writeFile(t, dir, "tagged.go", "//go:build mytag\n\npackage foo\n\nfunc Tagged() {}\n")
	if got := buildableGoFiles(dir, "mytag"); !containsBase(got, "tagged.go") {
		t.Errorf("buildableGoFiles(dir, mytag) excluded tagged.go: %v", got)
	}
	if got := buildableGoFiles(dir, ""); containsBase(got, "tagged.go") {
		t.Errorf("buildableGoFiles(dir, \"\") included tagged.go: %v", got)
	}
}

func containsBase(files []string, base string) bool {
	for _, f := range files {
		if filepath.Base(f) == base {
			return true
		}
	}
	return false
}

// TestResolveFilesImportPaths: import-path arguments must resolve (R1-2)
// for a same-module package and for the standard library, and a
// genuinely unknown path must still error instead of silently passing.
func TestResolveFilesImportPaths(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/m\n\ngo 1.24\n")
	writeFile(t, root, "sub/pkg.go", "package sub\n\nfunc Hello() string { return \"hi\" }\n")
	// t.Chdir needs a go1.24 module directive; this module is older, so
	// chdir manually with a cleanup restore instead.
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(oldWd) })

	files, err := resolveFiles([]string{"example.com/m/sub"})
	if err != nil {
		t.Fatalf("resolveFiles(module import path): %v", err)
	}
	if !containsBase(files, "pkg.go") {
		t.Errorf("resolveFiles(module import path) = %v, want pkg.go among them", files)
	}

	dirs, err := resolveTestDirs([]string{"example.com/m/sub"})
	if err != nil {
		t.Fatalf("resolveTestDirs(module import path): %v", err)
	}
	if len(dirs) != 1 || filepath.Base(dirs[0]) != "sub" {
		t.Errorf("resolveTestDirs(module import path) = %v, want [..%s sub]", dirs, string(filepath.Separator))
	}

	std, err := resolveFiles([]string{"fmt"})
	if err != nil {
		t.Fatalf("resolveFiles(stdlib import path): %v", err)
	}
	if len(std) == 0 || !containsBase(std, "format.go") {
		t.Errorf("resolveFiles(fmt) = %v, want the fmt package files", std)
	}
	if got := importPathForDir(filepath.Join(goRoot(), "src", "fmt")); got != "fmt" {
		t.Errorf("importPathForDir(GOROOT/src/fmt) = %q, want %q", got, "fmt")
	}

	if _, err := resolveFiles([]string{"example.com/nope/nothere"}); err == nil {
		t.Errorf("resolveFiles(unknown import path) succeeded, want an error")
	}
}

// TestDiscoverDroppedKinds: Benchmark/Example/Fuzz/TestMain must be
// reported as dropped (R1-4) while ordinary TestXxx functions still are
// discovered.
func TestDiscoverDroppedKinds(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "x_test.go", `package foo

import "testing"

func TestReal(t *testing.T) {}
func BenchmarkReal(b *testing.B) {}
func ExampleReal() {}
func FuzzReal(f *testing.F) {}
func TestMain(m *testing.M) {}
`)
	path := filepath.Join(dir, "x_test.go")
	tests, err := discoverTests(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestReal" {
		t.Errorf("discoverTests = %v, want [TestReal]", tests)
	}
	dropped, err := discoverDroppedKinds(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"BenchmarkReal", "ExampleReal", "FuzzReal", "TestMain"}
	if len(dropped) != len(want) {
		t.Fatalf("discoverDroppedKinds = %v, want %v", dropped, want)
	}
	for i, w := range want {
		if dropped[i] != w {
			t.Errorf("discoverDroppedKinds[%d] = %q, want %q", i, dropped[i], w)
		}
	}
}

// TestShimNewSurfaceRunsUnderHostGo synthesizes a driver for a test
// using the R1-4 shim additions (Cleanup, Parallel, Setenv, Chdir,
// TempDir, Context, Deadline) and runs it with the host toolchain.
// The driver is plain Go, so a host run validates the shim's semantics:
// cleanups run (marker files), Setenv/Chdir restore their old values
// (checked by cleanups registered before the mutation), and the test
// passes. This is a host-go run of the generated driver, not a graalgo
// backend run — it validates the shim source the backend will compile.
func TestShimNewSurfaceRunsUnderHostGo(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skipf("host go not available at %s: %v", goBin, err)
	}
	dir := t.TempDir()
	writeFile(t, dir, "foo.go", "package foo\n\nfunc Add(a, b int) int { return a + b }\n")
	writeFile(t, dir, "foo_test.go", `package foo

import (
	"os"
	"testing"
)

func TestShimSurface(t *testing.T) {
	t.Parallel()
	if _, ok := t.Deadline(); ok {
		t.Errorf("Deadline unexpectedly reported a deadline")
	}
	ctx := t.Context()
	select {
	case <-ctx.Done():
		t.Errorf("Context already done")
	default:
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Cleanups registered BEFORE a mutation must observe the restored
	// value (LIFO: the mutation's own restore cleanup runs first).
	t.Cleanup(func() {
		if os.Getenv("GRAALGO_L05_SHIM") != "" {
			t.Errorf("Setenv was not restored by Cleanup")
		}
		if got, _ := os.Getwd(); got != wd {
			t.Errorf("Chdir was not restored by Cleanup: got %q want %q", got, wd)
		}
		if err := os.WriteFile("cleanup-marker", []byte("done"), 0o644); err != nil {
			t.Errorf("cleanup marker write: %v", err)
		}
	})
	tmp := t.TempDir()
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("TempDir missing: %v", err)
	}
	t.Setenv("GRAALGO_L05_SHIM", "x")
	if os.Getenv("GRAALGO_L05_SHIM") != "x" {
		t.Errorf("Setenv did not take effect")
	}
	t.Chdir(tmp)
	if got, _ := os.Getwd(); got != tmp {
		t.Errorf("Chdir did not take effect: got %q want %q", got, tmp)
	}
	if Add(2, 3) != 5 {
		t.Errorf("Add broken")
	}
	t.Run("sub", func(t *testing.T) {
		// The test is Chdir'd into tmp at this point, so the marker
		// needs an absolute path (the TempDir cleanup removes tmp
		// at the end of the outer test).
		t.Cleanup(func() {
			if err := os.WriteFile(wd+"/sub-cleanup-marker", []byte("done"), 0o644); err != nil {
				t.Errorf("sub cleanup marker write: %v", err)
			}
		})
	})
}
`)
	testFile := filepath.Join(dir, "foo_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 {
		t.Fatalf("discoverTests = %v, want [TestShimSurface]", tests)
	}
	importPkgNameCache = map[string]string{}
	driver, err := synthesizeTestDriver(dir, []fileTests{{file: testFile, tests: tests}}, []string{filepath.Join(dir, "foo.go")}, false, "")
	if err != nil {
		t.Fatalf("synthesizeTestDriver: %v", err)
	}
	defer os.Remove(driver)

	runDir := t.TempDir()
	// The driver writes its markers into its working directory, so
	// run it from a fresh directory (not the package dir).
	dst := filepath.Join(runDir, "driver.go")
	src, err := os.ReadFile(driver)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, src, 0o644); err != nil {
		t.Fatal(err)
	}
	// The shim source must mention the new surface; a missing method
	// would otherwise fail only later, at backend compile time.
	for _, want := range []string{"func (t *graalgoT) Cleanup", "func (t *graalgoT) Parallel", "func (t *graalgoT) Setenv", "func (t *graalgoT) Chdir", "func (t *graalgoT) TempDir", "func (t *graalgoT) Context", "func (t *graalgoT) Deadline", "tt.runCleanups()"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("generated driver lacks %q", want)
		}
	}
	cmd := exec.Command(goBin, "run", dst)
	cmd.Dir = runDir
	// Ensure the probe variable starts unset in the child.
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GRAALGO_L05_SHIM=") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("host go run of generated driver failed: %v\n%s", err, out)
	}
	for _, marker := range []string{"cleanup-marker", "sub-cleanup-marker"} {
		if _, err := os.Stat(filepath.Join(runDir, marker)); err != nil {
			t.Errorf("marker %s missing after driver run (cleanup did not run): %v", marker, err)
		}
	}
}
