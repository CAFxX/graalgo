package p3frontend

import (
	"strings"
	"testing"
)

// The p3i_ namespace belongs to the flatten encoding (d.Foo ->
// p3i_<hash>_Foo). A user identifier in it would silently collide with
// a renamed dependency identifier, so Flatten must reject it LOUDLY.
// RV18-P1-06: the previous guard walked only pkg.Scope().Names(), so a
// user local or parameter named e.g. p3i_9f2ac41b_Foo bound the renamed
// reference to the local instead of the renamed global — a silent
// miscompile. These tests pin both the original package-level guard
// and the new every-identifier guard.

// mergeTestModule writes a module (go.mod + files) to a temp dir and
// runs the flatten pipeline on it, returning the merged source or the
// flatten error.
func mergeTestModule(t *testing.T, modulePath string, files map[string]string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	all := map[string]string{
		"go.mod": "module " + modulePath + "\n\ngo 1.27\n",
	}
	for name, content := range files {
		all[name] = content
	}
	writeFiles(t, dir, all)
	return MergeDir(dir)
}

func requireReservedPrefixError(t *testing.T, err error, pkgPath, ident string) {
	t.Helper()
	if err == nil {
		t.Fatalf("MergeDir succeeded, want loud reserved-p3i_ error for %q in %q", ident, pkgPath)
	}
	msg := err.Error()
	if !strings.Contains(msg, "reserved p3i_ encoding namespace") {
		t.Fatalf("error %q does not mention the reserved p3i_ encoding namespace", msg)
	}
	if !strings.Contains(msg, pkgPath) {
		t.Fatalf("error %q does not name package %q", msg, pkgPath)
	}
	if !strings.Contains(msg, ident) {
		t.Fatalf("error %q does not name identifier %q", msg, ident)
	}
}

// TestFlattenRejectsReservedPrefixPackageLevel pins the original guard:
// a package-level identifier with the p3i_ prefix fails loudly, in main
// and in a dependency.
func TestFlattenRejectsReservedPrefixPackageLevel(t *testing.T) {
	t.Run("main", func(t *testing.T) {
		_, err := mergeTestModule(t, "example.com/m", map[string]string{
			"main.go": "package main\n\nvar p3i_evil = 1\n\nfunc main() { println(p3i_evil) }\n",
		})
		requireReservedPrefixError(t, err, "example.com/m", "p3i_evil")
	})
	t.Run("dep", func(t *testing.T) {
		_, err := mergeTestModule(t, "example.com/m", map[string]string{
			"main.go": "package main\n\nimport \"example.com/m/d\"\n\nfunc main() { println(d.Foo()) }\n",
			"d/d.go":  "package d\n\nfunc p3i_evil() int { return 1 }\n\nfunc Foo() int { return p3i_evil() + 41 }\n",
		})
		requireReservedPrefixError(t, err, "example.com/m/d", "p3i_evil")
	})
}

// TestFlattenRejectsReservedPrefixLocal is the RV18-P1-06 hole: a user
// local variable whose name is exactly a renamed dependency identifier
// (d.Foo -> p3i_<hash>_Foo) must fail loudly instead of silently
// rebinding the renamed reference to the local.
func TestFlattenRejectsReservedPrefixLocal(t *testing.T) {
	depPath := "example.com/m/d"
	colliding := prefixFor(depPath) + "Foo"
	mainSrc := "package main\n\nimport \"example.com/m/d\"\n\n" +
		"func main() {\n" +
		"\t" + colliding + " := func() int { return -1 }\n" +
		"\tprintln(d.Foo(), " + colliding + "())\n" +
		"}\n"
	_, err := mergeTestModule(t, "example.com/m", map[string]string{
		"main.go": mainSrc,
		"d/d.go":  "package d\n\nfunc Foo() int { return 42 }\n",
	})
	requireReservedPrefixError(t, err, "example.com/m", colliding)
}

// TestFlattenRejectsReservedPrefixParam: a parameter with the p3i_
// prefix fails loudly (any p3i_ prefix, not just a colliding one).
func TestFlattenRejectsReservedPrefixParam(t *testing.T) {
	_, err := mergeTestModule(t, "example.com/m", map[string]string{
		"main.go": "package main\n\nfunc ident(p3i_00000000_x int) int { return p3i_00000000_x }\n\nfunc main() { println(ident(1)) }\n",
	})
	requireReservedPrefixError(t, err, "example.com/m", "p3i_00000000_x")
}

// TestFlattenRejectsReservedPrefixDepLocal: the guard covers
// dependencies' locals too, and the error names the dependency.
func TestFlattenRejectsReservedPrefixDepLocal(t *testing.T) {
	_, err := mergeTestModule(t, "example.com/m", map[string]string{
		"main.go": "package main\n\nimport \"example.com/m/d\"\n\nfunc main() { println(d.Foo()) }\n",
		"d/d.go":  "package d\n\nfunc Foo() int {\n\tp3i_zz := 1\n\treturn p3i_zz + 41\n}\n",
	})
	requireReservedPrefixError(t, err, "example.com/m/d", "p3i_zz")
}

// TestFlattenAllowsNormalIdents is the positive control: identifiers
// without the prefix flatten normally (guard must not over-fire), and
// the dependency reference is still renamed to the encoding namespace.
func TestFlattenAllowsNormalIdents(t *testing.T) {
	depPath := "example.com/m/d"
	merged, err := mergeTestModule(t, "example.com/m", map[string]string{
		"main.go": "package main\n\nimport \"example.com/m/d\"\n\nfunc main() {\n\tx := d.Foo()\n\tprintln(x)\n}\n",
		"d/d.go":  "package d\n\nfunc Foo() int { return 42 }\n",
	})
	if err != nil {
		t.Fatalf("MergeDir failed on clean input: %v", err)
	}
	renamed := prefixFor(depPath) + "Foo"
	if !strings.Contains(merged, renamed) {
		t.Fatalf("merged source does not contain renamed identifier %q:\n%s", renamed, merged)
	}
}
