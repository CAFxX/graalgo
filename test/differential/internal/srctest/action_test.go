package srctest

import (
	"reflect"
	"testing"

	"diffharness/internal/typeparam"
)

// The GOROOT/test action line drives classification the way gc's own test
// driver (cmd/internal/testdir) does.
func scanOne(t *testing.T, name, body string) typeparam.TFile {
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
	return files[0]
}

const mainBody = "\n\npackage main\n\nfunc main() {}\n"

func TestActionSkipIsNotRun(t *testing.T) {
	if f := scanOne(t, "s.go", "// skip"+mainBody); f.Class != typeparam.ClassNotRun {
		t.Fatalf("// skip: want notrun, got %q", f.Class)
	}
}

func TestActionCompileAndBuildAreCompileOnly(t *testing.T) {
	for _, a := range []string{"compile", "build"} {
		if f := scanOne(t, "c.go", "// "+a+mainBody); f.Class != typeparam.ClassCompile {
			t.Fatalf("// %s: want compile, got %q", a, f.Class)
		}
	}
}

func TestActionRunArgsAndDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "// run -gcflags -l=4 extra.go arg1 arg2"+mainBody)
	files, err := Scan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := files[0]
	if f.Class != "run" {
		t.Fatalf("want run, got %q", f.Class)
	}
	if !reflect.DeepEqual(f.Args, []string{"arg1", "arg2"}) {
		t.Fatalf("args: got %q", f.Args)
	}
	if f.RunDir != dir {
		t.Fatalf("run dir: got %q, want the test dir %q", f.RunDir, dir)
	}
}

func TestActionOnlyFirstLine(t *testing.T) {
	// A later "// run to run ..." comment line is not an action line.
	f := scanOne(t, "h.go", "// run\n\n// Copyright\n// run to run. More text.\n\npackage main\n\nfunc main() {}\n")
	if len(f.Args) != 0 {
		t.Fatalf("args from a non-action line: %q", f.Args)
	}
}

func TestSelect(t *testing.T) {
	files := []typeparam.TFile{{Name: "a.go"}, {Name: "b.go"}, {Name: "c.go"}}
	got, err := Select(files, []string{"c.go", "a.go"})
	if err != nil || len(got) != 2 || got[0].Name != "a.go" || got[1].Name != "c.go" {
		t.Fatalf("Select: got %v, %v", got, err)
	}
	if _, err := Select(files, []string{"zz.go"}); err == nil {
		t.Fatal("Select of an unknown file must fail")
	}
	if _, err := Select(files, []string{""}); err == nil {
		t.Fatal("Select of nothing must fail")
	}
}
