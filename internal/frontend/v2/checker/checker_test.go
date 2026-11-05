// Checker tests (review-1105-v2-types): error wrapping and
// deterministic file order.
package checker

import (
	"errors"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/parser"
)

func TestCheckErrorWraps(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(fn, []byte("package demo\nfunc main() { undefinedFn() }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pres, err := parser.ParseFiles([]string{fn})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Check(pres.Fset, pres.Pkg, &Config{GoVersion: "go1.27"})
	if err == nil {
		t.Fatal("expected a type-check error")
	}
	if errors.Unwrap(err) == nil {
		t.Errorf("Check error %q does not wrap its cause (want %%w, not %%v)", err.Error())
	}
	var terr types.Error
	if !errors.As(err, &terr) {
		t.Errorf("Check error %q does not unwrap to a types.Error", err.Error())
	}
}

func TestCheckFilesSorted(t *testing.T) {
	// Three files, each with a distinct type error. The first error
	// reported must always come from a.go (alphabetically first),
	// regardless of Go map iteration order inside the checker.
	dir := t.TempDir()
	for name, ident := range map[string]string{
		"a.go": "undefinedA",
		"m.go": "undefinedM",
		"z.go": "undefinedZ",
	} {
		src := "package demo\nfunc f" + strings.ToUpper(name[:1]) + "() { " + ident + "() }\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{
		filepath.Join(dir, "z.go"),
		filepath.Join(dir, "m.go"),
		filepath.Join(dir, "a.go"),
	}
	for i := 0; i < 10; i++ {
		pres, err := parser.ParseFiles(paths)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Check(pres.Fset, pres.Pkg, &Config{GoVersion: "go1.27"})
		if err == nil {
			t.Fatal("expected a type-check error")
		}
		if !strings.Contains(err.Error(), "undefinedA") {
			t.Errorf("run %d: first error = %q, want the a.go error (undefinedA)", i, err.Error())
		}
	}
}
