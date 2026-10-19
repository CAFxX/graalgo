package asmstrategy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Every test here is demonstrably non-tautological: each fail-fast case
// asserts the build errors (a neutered check returns nil and the test
// fails with "proceeded silently"), and the exact-format test pins the
// ADR-0007 D7 contract byte-for-byte.

// testInventoryJSON builds a minimal inventory payload with the given
// entries, pinned to the running toolchain's version (NewInventory
// asserts go_version == runtime.Version()).
func testInventoryJSON(t *testing.T, files []AsmEntry) []byte {
	t.Helper()
	payload := map[string]any{
		"go_version": runtime.Version(),
		"categories": []string{"evaporate", "intrinsify", "emulate-then-intrinsify", "jvm-math", "unsupported"},
		"files":      files,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshaling test inventory: %v", err)
	}
	return data
}

func testInventory(t *testing.T, caller string, files []AsmEntry) *Inventory {
	t.Helper()
	inv, err := NewInventory(testInventoryJSON(t, files), caller)
	if err != nil {
		t.Fatalf("NewInventory: %v", err)
	}
	return inv
}

// fakeGorootPkg creates <tmp>/src/fakelib/x_amd64.s and returns the
// goroot, package dir, and absolute .s path.
func fakeGorootPkg(t *testing.T) (goroot, pkgDir, sPath string) {
	t.Helper()
	goroot = t.TempDir()
	pkgDir = filepath.Join(goroot, "src", "fakelib")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sPath = filepath.Join(pkgDir, "x_amd64.s")
	if err := os.WriteFile(sPath, []byte("TEXT ·x(SB),$0-0\n\tRET\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return goroot, pkgDir, sPath
}

// TestResolutionTable covers all five stored categories: the four
// replaced categories resolve (build proceeds) and unsupported fails
// fast with the ADR error format.
func TestResolutionTable(t *testing.T) {
	goroot, pkgDir, sPath := fakeGorootPkg(t)
	names := []string{"evap.s", "intr.s", "emul.s", "math.s", "unsup.s"}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(pkgDir, n), []byte("TEXT ·x(SB),$0-0\n\tRET\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inv := testInventory(t, "gofrontend", []AsmEntry{
		{Path: "fakelib/evap.s", Package: "fakelib", Category: "evaporate", Replacement: "evaporates", Reason: "r"},
		{Path: "fakelib/intr.s", Package: "fakelib", Category: "intrinsify", Replacement: "intrinsic.X", Reason: "r"},
		{Path: "fakelib/emul.s", Package: "fakelib", Category: "emulate-then-intrinsify", Replacement: "pure-Go fallback", Reason: "r"},
		{Path: "fakelib/math.s", Package: "fakelib", Category: "jvm-math", Replacement: "StrictMath", Reason: "r"},
		{Path: "fakelib/unsup.s", Package: "fakelib", Category: "unsupported", Reason: "no strategy yet"},
	})
	_ = sPath

	// The four replaced categories resolve.
	res, err := inv.CheckAsmFiles(pkgDir, "fakelib", names[:4], goroot)
	if err != nil {
		t.Fatalf("replaced categories must resolve, got: %v", err)
	}
	if len(res) != 4 {
		t.Fatalf("expected 4 resolutions, got %d", len(res))
	}
	wantCat := map[string]string{
		"evap.s": "evaporate", "intr.s": "intrinsify",
		"emul.s": "emulate-then-intrinsify", "math.s": "jvm-math",
	}
	for _, r := range res {
		base := filepath.Base(r.File)
		if wantCat[base] != r.Category {
			t.Errorf("%s: category = %q, want %q", base, r.Category, wantCat[base])
		}
		if r.Package != "fakelib" {
			t.Errorf("%s: package = %q, want fakelib", base, r.Package)
		}
		if r.Replacement == "" {
			t.Errorf("%s: empty replacement on a replaced category", base)
		}
	}

	// Unsupported fails fast with file+package+category+reason.
	_, err = inv.CheckAsmFiles(pkgDir, "fakelib", []string{"unsup.s"}, goroot)
	if err == nil {
		t.Fatal("unsupported .s file: build proceeded silently")
	}
	msg := err.Error()
	for _, want := range []string{
		filepath.Join(pkgDir, "unsup.s"), `"fakelib"`, `"unsupported"`, "no strategy yet", "asm fail-fast",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("fail-fast error missing %q:\n%s", want, msg)
		}
	}
}

// TestExactErrorFormat pins the ADR-0007 D7 contract byte-for-byte
// (after the caller prefix).
func TestExactErrorFormat(t *testing.T) {
	goroot, pkgDir, _ := fakeGorootPkg(t)
	inv := testInventory(t, "p3frontend", []AsmEntry{
		{Path: "fakelib/x_amd64.s", Package: "fakelib", Category: "unsupported", Reason: "boom"},
	})
	_, err := inv.CheckAsmFiles(pkgDir, "fakelib", []string{"x_amd64.s"}, goroot)
	if err == nil {
		t.Fatal("unsupported .s file: build proceeded silently")
	}
	abs, _ := filepath.Abs(filepath.Join(pkgDir, "x_amd64.s"))
	want := fmt.Sprintf("p3frontend: asm fail-fast: %s (package %q): category %q: %s",
		abs, "fakelib", "unsupported", "boom")
	if err.Error() != want {
		t.Errorf("error format mismatch:\n got: %s\nwant: %s", err.Error(), want)
	}
}

// TestUncategorizedFails proves a .s file with no inventory entry fails
// fast (toolchain drift) instead of being silently ignored.
func TestUncategorizedFails(t *testing.T) {
	goroot, pkgDir, sPath := fakeGorootPkg(t)
	inv := testInventory(t, "gofrontend", nil)
	_, err := inv.CheckAsmFiles(pkgDir, "fakelib", []string{"x_amd64.s"}, goroot)
	if err == nil {
		t.Fatal("uncategorized .s file: build proceeded silently")
	}
	msg := err.Error()
	for _, want := range []string{sPath, `"fakelib"`, `"uncategorized"`, "no inventory entry", "asm fail-fast"} {
		if !strings.Contains(msg, want) {
			t.Errorf("fail-fast error missing %q:\n%s", want, msg)
		}
	}
}

// TestOutsideGorootFails proves a .s file outside the inventoried
// GOROOT/src fails fast as uncategorized (p3frontend's module-local
// case; gofrontend's hook is additionally scoped by IsStdlibDir).
func TestOutsideGorootFails(t *testing.T) {
	goroot := t.TempDir()
	dir := t.TempDir() // outside goroot/src
	sPath := filepath.Join(dir, "handasm_amd64.s")
	if err := os.WriteFile(sPath, []byte("TEXT ·x(SB),$0-0\n\tRET\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv := testInventory(t, "gofrontend", nil)
	_, err := inv.CheckAsmFiles(dir, "example.com/mod/handasm", []string{"handasm_amd64.s"}, goroot)
	if err == nil {
		t.Fatal("outside-GOROOT .s file: build proceeded silently")
	}
	msg := err.Error()
	for _, want := range []string{sPath, `"example.com/mod/handasm"`, `"uncategorized"`, "outside the inventoried GOROOT/src"} {
		if !strings.Contains(msg, want) {
			t.Errorf("fail-fast error missing %q:\n%s", want, msg)
		}
	}
}

// TestEmptyReplacementRejected proves a replaced category with an empty
// replacement pointer fails fast: a dangling pointer is a
// generator/inventory bug, never a silent proceed.
func TestEmptyReplacementRejected(t *testing.T) {
	goroot, pkgDir, _ := fakeGorootPkg(t)
	inv := testInventory(t, "gofrontend", []AsmEntry{
		{Path: "fakelib/x_amd64.s", Package: "fakelib", Category: "intrinsify", Replacement: "", Reason: "r"},
	})
	_, err := inv.CheckAsmFiles(pkgDir, "fakelib", []string{"x_amd64.s"}, goroot)
	if err == nil {
		t.Fatal("empty replacement pointer: build proceeded silently")
	}
	if !strings.Contains(err.Error(), "empty replacement") {
		t.Errorf("expected empty-replacement error, got:\n%s", err.Error())
	}
}

// TestGoVersionMismatch proves a stale inventory (toolchain moved without
// regeneration) fails at load, not by miscategorizing files.
func TestGoVersionMismatch(t *testing.T) {
	payload := map[string]any{
		"go_version": "go9.9.9",
		"categories": []string{"evaporate"},
		"files":      []AsmEntry{},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewInventory(data, "gofrontend"); err == nil {
		t.Fatal("go_version mismatch: inventory loaded silently")
	} else if !strings.Contains(err.Error(), "go_version mismatch") {
		t.Errorf("expected go_version mismatch error, got:\n%s", err.Error())
	}
}

// TestLoadEmbeddedSmoke proves the generated artifact parses, carries the
// pinned toolchain version, and resolves a known entry (reflect's stub is
// the canonical unsupported case).
func TestLoadEmbeddedSmoke(t *testing.T) {
	inv, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	if len(inv.byPath) == 0 {
		t.Fatal("embedded inventory is empty")
	}
	e, ok := inv.byPath["reflect/asm_amd64.s"]
	if !ok {
		t.Fatal("embedded inventory missing reflect/asm_amd64.s")
	}
	if e.Category != "unsupported" {
		t.Errorf("reflect/asm_amd64.s category = %q, want unsupported", e.Category)
	}
}

// TestIsStdlibDir pins the structural scoping predicate: GOROOT/src
// prefix, never a package-name list.
func TestIsStdlibDir(t *testing.T) {
	goroot := filepath.Join(t.TempDir(), "go")
	cases := []struct {
		dir  string
		want bool
	}{
		{filepath.Join(goroot, "src", "sync", "atomic"), true},
		{filepath.Join(goroot, "src"), true},
		{filepath.Join(goroot, "src2", "sync"), false},            // prefix sibling, not child
		{filepath.Join(t.TempDir(), "modcache", "x@v1"), false},   // module cache
		{filepath.Join(t.TempDir(), "gopath", "src", "x"), false}, // GOPATH
		{"", false},
	}
	for _, c := range cases {
		if got := IsStdlibDir(c.dir, goroot); got != c.want {
			t.Errorf("IsStdlibDir(%q) = %v, want %v", c.dir, got, c.want)
		}
	}
	if IsStdlibDir(filepath.Join(goroot, "src", "x"), "") {
		t.Error("IsStdlibDir with empty goroot must be false")
	}
}
