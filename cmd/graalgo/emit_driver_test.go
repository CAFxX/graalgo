package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

const emitDriverDemoSrc = "package demo\n\nfunc Add(a, b int) int { return a + b }\n"

const emitDriverDemoTest = `package demo

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("bad add")
	}
}

func TestSub(t *testing.T) {
	if Add(1, 2) == 4 {
		t.Fatal("impossible")
	}
}
`

const emitDriverMixedTest = `package demo

import "testing"

func TestWithSub(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		if Add(1, 2) != 3 {
			t.Fatal("bad add")
		}
	})
}

func ExampleHello() {
	println("hello")
	// Output: hello
}

func FuzzParse(f *testing.F) {
	f.Add("a")
	f.Fuzz(func(t *testing.T, s string) {})
}
`

// emitDriverFixture creates a minimal package with two tests.
func emitDriverFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "demo.go", emitDriverDemoSrc)
	writeFile(t, dir, "demo_test.go", emitDriverDemoTest)
	return dir
}

// emitDriver runs emitTestDriver on dir and returns the emitted source.
func emitDriver(t *testing.T, dir string, runRe *regexp.Regexp) string {
	t.Helper()
	importPkgNameCache = map[string]string{}
	out := filepath.Join(t.TempDir(), "driver.go")
	if err := emitTestDriver(dir, out, runRe); err != nil {
		t.Fatalf("emitTestDriver: %v", err)
	}
	src, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

func TestEmitDriverWritesVerboseDriver(t *testing.T) {
	dir := emitDriverFixture(t)
	src := emitDriver(t, dir, nil)
	for _, want := range []string{
		"TestAdd(tt)", "TestSub(tt)",
		"const graalgoVerbose = true",
		"func main() {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("emitted driver missing %q", want)
		}
	}
}

// TestEmitDriverSkipsExampleAndFuzz pins the driver's emitted
// format for a package mixing a subtest, an Example, and a Fuzz
// target: the Test (with its subtest) is emitted; Example/Fuzz entry
// points are not.
func TestEmitDriverSkipsExampleAndFuzz(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "demo.go", emitDriverDemoSrc)
	writeFile(t, dir, "demo_test.go", emitDriverMixedTest)
	src := emitDriver(t, dir, nil)
	if !strings.Contains(src, "TestWithSub(tt)") {
		t.Error("emitted driver missing TestWithSub")
	}
	for _, want := range []string{"ExampleHello", "FuzzParse"} {
		if strings.Contains(src, want) {
			t.Errorf("emitted driver contains %q entry point", want)
		}
	}
}

func TestEmitDriverRunFilter(t *testing.T) {
	dir := emitDriverFixture(t)
	src := emitDriver(t, dir, regexp.MustCompile("^TestAdd$"))
	if !strings.Contains(src, "TestAdd(tt)") {
		t.Error("emitted driver missing selected TestAdd")
	}
	if strings.Contains(src, "TestSub(tt)") {
		t.Error("emitted driver contains filtered-out TestSub")
	}
}

func TestEmitDriverNoTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "demo.go", emitDriverDemoSrc)
	importPkgNameCache = map[string]string{}
	err := emitTestDriver(dir, filepath.Join(t.TempDir(), "driver.go"), nil)
	if err == nil {
		t.Fatal("emitTestDriver on a package with no tests succeeded")
	}
	if !strings.Contains(err.Error(), "no tests to run") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEmitDriverRunFilterMatchesNothing(t *testing.T) {
	dir := emitDriverFixture(t)
	importPkgNameCache = map[string]string{}
	err := emitTestDriver(dir, filepath.Join(t.TempDir(), "driver.go"),
		regexp.MustCompile("^TestNoSuch$"))
	if err == nil {
		t.Fatal("emitTestDriver with a non-matching -run filter succeeded")
	}
	if !strings.Contains(err.Error(), "no tests to run") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEmitDriverBadOutputPath(t *testing.T) {
	dir := emitDriverFixture(t)
	importPkgNameCache = map[string]string{}
	err := emitTestDriver(dir,
		filepath.Join(t.TempDir(), "nonexistent", "driver.go"), nil)
	if err == nil {
		t.Fatal("emitTestDriver to an unwritable path succeeded")
	}
}

func TestEmitDriverViaCmdTest(t *testing.T) {
	dir := emitDriverFixture(t)
	out := filepath.Join(t.TempDir(), "driver.go")
	if rc := cmdTest(t.TempDir(), []string{"-emit-driver", out, dir}); rc != 0 {
		t.Fatalf("cmdTest -emit-driver returned %d", rc)
	}
	src, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "const graalgoVerbose = true") {
		t.Error("driver emitted via cmdTest is not verbose")
	}
}

func TestEmitDriverEqualsForm(t *testing.T) {
	dir := emitDriverFixture(t)
	out := filepath.Join(t.TempDir(), "driver.go")
	if rc := cmdTest(t.TempDir(), []string{"-emit-driver=" + out, dir}); rc != 0 {
		t.Fatalf("cmdTest -emit-driver= returned %d", rc)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("driver not written: %v", err)
	}
}

func TestEmitDriverMultiplePackages(t *testing.T) {
	dir1 := emitDriverFixture(t)
	dir2 := emitDriverFixture(t)
	out := filepath.Join(t.TempDir(), "driver.go")
	if rc := cmdTest(t.TempDir(), []string{"-emit-driver", out, dir1, dir2}); rc == 0 {
		t.Fatal("cmdTest -emit-driver with two packages succeeded")
	}
}

func TestEmitDriverMissingPackage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "driver.go")
	if rc := cmdTest(t.TempDir(), []string{"-emit-driver", out, "/nonexistent-dir-xyz"}); rc == 0 {
		t.Fatal("cmdTest -emit-driver with a missing package succeeded")
	}
}

func TestEmitDriverRunsUnderGC(t *testing.T) {
	dir := emitDriverFixture(t)
	out := filepath.Join(t.TempDir(), "driver.go")
	importPkgNameCache = map[string]string{}
	if err := emitTestDriver(dir, out, nil); err != nil {
		t.Fatalf("emitTestDriver: %v", err)
	}
	defer os.Remove(out)
	cmd := exec.Command(goBinary(t), "run", out)
	combined, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("go run driver: %v\noutput:\n%s", err, combined)
		}
	}
	if code != 0 {
		t.Fatalf("go run driver exited %d\noutput:\n%s", code, combined)
	}
	for _, want := range []string{"--- PASS: TestAdd", "--- PASS: TestSub"} {
		if !strings.Contains(string(combined), want) {
			t.Errorf("driver output missing %q:\n%s", want, combined)
		}
	}
}

// TestImportPathForDirStdlib is the regression test for the Gate 1
// wiring: importPathForDir on a standard-library directory must
// return the import path (what `go test` prints), not the absolute
// filesystem path it returned before this fix.
func TestImportPathForDirStdlib(t *testing.T) {
	if os.Getenv("GOROOT") != "" {
		t.Skip("GOROOT is set; goRoot() would not use the toolchain GOROOT")
	}
	goroot := runtime.GOROOT()
	if goroot == "" {
		t.Skip("no toolchain GOROOT")
	}
	for dir, want := range map[string]string{
		filepath.Join(goroot, "src", "strings"):     "strings",
		filepath.Join(goroot, "src", "net", "http"): "net/http",
	} {
		if got := importPathForDir(dir); got != want {
			t.Errorf("importPathForDir(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestImportPathForDirNoModule(t *testing.T) {
	dir := t.TempDir()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := importPathForDir(dir); got != abs {
		t.Errorf("importPathForDir(%q) = %q, want abs path %q", dir, got, abs)
	}
}
