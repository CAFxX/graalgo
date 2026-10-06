package p3frontend

import (
	"go/build"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// P3-M6 (GG-0009) asm strategy tests. Every test here is
// demonstrably non-tautological: the oracle test compares against the
// independent go/build implementation, the completeness checker is
// exercised against a scratch tree with a fake file (and must report
// it), and hidden_files/m6-asm/redgreen.md records the mutation run in
// which the fail-fast was neutered and these tests went red.

// gorootSrc returns the pinned toolchain's src dir, failing the test
// when the toolchain is absent (other tests in this package already
// assume the real GOROOT: see TestBuildTagFiltering).
func gorootSrc(t *testing.T) string {
	t.Helper()
	src := filepath.Join(NewLoader().GOROOT(), "src")
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		t.Fatalf("pinned GOROOT src dir missing: %s", src)
	}
	return src
}

// enumerateSFiles walks root and returns every *.s path relative to
// root, slash-separated and sorted.
func enumerateSFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".s") {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// oracleSFiles is the independent go/build oracle for .s selection:
// (*build.Context{GOOS:linux, GOARCH:amd64}).ImportDir, i.e. the file
// set the stock toolchain would assemble.
func oracleSFiles(t *testing.T, dir string) []string {
	t.Helper()
	tags := []string{}
	for v := 1; v <= 27; v++ {
		tags = append(tags, "go1."+strconv.Itoa(v))
	}
	ctxt := &build.Context{GOOS: "linux", GOARCH: "amd64", Compiler: "gc", ReleaseTags: tags}
	p, err := ctxt.ImportDir(dir, 0)
	if err != nil {
		t.Fatalf("oracle ImportDir(%s): %v", dir, err)
	}
	got := append([]string{}, p.SFiles...)
	sort.Strings(got)
	return got
}

// TestAsmInventoryComplete re-enumerates the pinned GOROOT/src tree and
// asserts the embedded inventory covers it EXACTLY: every .s file on
// disk has an entry, and no entry is stale. A new .s file added by a
// toolchain upgrade (or a removed one) fails here until the generator
// is re-run.
func TestAsmInventoryComplete(t *testing.T) {
	src := gorootSrc(t)
	inv, err := loadAsmInventory()
	if err != nil {
		t.Fatal(err)
	}
	disk := enumerateSFiles(t, src)
	if len(disk) == 0 {
		t.Fatal("no .s files enumerated: walker broken?")
	}
	missing := checkInventoryCoversList(t, src, inv)
	var stale []string
	onDisk := map[string]bool{}
	for _, rel := range disk {
		onDisk[rel] = true
	}
	for rel := range inv {
		if !onDisk[rel] {
			stale = append(stale, rel)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d .s files on disk have no inventory entry (first 10): %v",
			len(missing), missing[:min(10, len(missing))])
	}
	if len(stale) > 0 {
		t.Errorf("%d inventory entries are stale (first 10): %v",
			len(stale), stale[:min(10, len(stale))])
	}
	t.Logf("inventory complete: %d files on disk == %d inventory entries", len(disk), len(inv))
}

// TestAsmInventoryCategoriesKnown asserts every stored category is one
// of the five SPEC §2.8 dispositions (the loader's parse-time check is
// what enforces this; this test pins the contract at the test level).
func TestAsmInventoryCategoriesKnown(t *testing.T) {
	inv, err := loadAsmInventory()
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{
		"evaporate": true, "intrinsify": true,
		"emulate-then-intrinsify": true, "jvm-math": true,
		"unsupported":             true,
	}
	counts := map[string]int{}
	for rel, e := range inv {
		if !known[e.Category] {
			t.Errorf("%s: unknown category %q", rel, e.Category)
		}
		counts[e.Category]++
		if e.Category != "unsupported" && e.Replacement == "" {
			t.Errorf("%s: replaced category %q has no replacement pointer", rel, e.Category)
		}
	}
	for c, n := range counts {
		t.Logf("category %-24s %d files", c, n)
	}
}

// TestAsmSelectionMatchesGoBuildOracle verifies the .s buildability
// filter (filename suffix + build constraints) agrees file-for-file
// with the stock go/build oracle on real stdlib dirs. If the filter
// drifted, replaced files could be skipped or foreign-arch files could
// fail the build spuriously.
func TestAsmSelectionMatchesGoBuildOracle(t *testing.T) {
	src := gorootSrc(t)
	loader := NewLoader()
	for _, pkg := range []string{
		"runtime", "internal/bytealg", "internal/cpu", "math", "math/big",
		"sync/atomic", "crypto/sha1", "crypto/internal/fips140/aes",
		"hash/crc32", "syscall", "reflect", "internal/abi",
	} {
		dir := filepath.Join(src, pkg)
		want := oracleSFiles(t, dir)
		res, err := loader.CheckAsmFiles(dir, pkg)
		if err != nil {
			// Unsupported packages fail fast by design; the oracle
			// comparison only applies to replaced categories.
			t.Logf("%s: CheckAsmFiles fail-fast (expected for unsupported): %v", pkg, err)
			continue
		}
		var got []string
		for _, r := range res {
			got = append(got, filepath.Base(r.File))
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: selection mismatch:\n got  %v\n want %v", pkg, got, want)
		} else {
			t.Logf("%s: %d .s files selected, oracle agrees", pkg, len(got))
		}
	}
}

// TestAsmCategoriesEndToEnd exercises the mechanism on real GOROOT
// dirs: one representative per replaced category must resolve with the
// right category and a replacement pointer (build proceeds), and the
// unsupported representatives must fail fast.
func TestAsmCategoriesEndToEnd(t *testing.T) {
	src := gorootSrc(t)
	loader := NewLoader()
	replaced := []struct {
		pkg          string
		category     string
		replacement  string // required substring of the pointer
	}{
		{"internal/bytealg", "intrinsify", "graalgo.runtime.Intrinsics.memequal"},
		{"sync/atomic", "intrinsify", "graalgo.runtime.Atomics"},
		{"internal/runtime/atomic", "intrinsify", "graalgo.runtime.Atomics"},
		{"runtime", "evaporate", "graalgo.runtime.Intrinsics"},
		{"internal/cpu", "evaporate", "graalgo host CPU-feature model"},
		{"crypto/sha1", "emulate-then-intrinsify", "emulate leg"},
		{"math", "jvm-math", "StrictMath"},
		{"math/big", "jvm-math", "BigInteger"},
	}
	for _, tc := range replaced {
		res, err := loader.CheckAsmFiles(filepath.Join(src, tc.pkg), tc.pkg)
		if err != nil {
			t.Errorf("%s: unexpected fail-fast: %v", tc.pkg, err)
			continue
		}
		if len(res) == 0 {
			t.Errorf("%s: no buildable .s files resolved (walker/filter broken?)", tc.pkg)
			continue
		}
		for _, r := range res {
			if r.Category != tc.category {
				t.Errorf("%s: %s category = %q, want %q", tc.pkg, r.File, r.Category, tc.category)
			}
			if !strings.Contains(r.Replacement, tc.replacement) {
				t.Errorf("%s: %s replacement %q lacks %q", tc.pkg, r.File, r.Replacement, tc.replacement)
			}
		}
		t.Logf("%s: %d files -> %s (build proceeds)", tc.pkg, len(res), tc.category)
	}

	unsupported := []struct {
		pkg    string
		reason string // required substring of the reason
	}{
		{"hash/crc32", "no graalgo intrinsic yet"},
		{"syscall", "Panama FFI"},
		{"reflect", "reimplemented on Truffle"},
	}
	for _, tc := range unsupported {
		_, err := loader.CheckAsmFiles(filepath.Join(src, tc.pkg), tc.pkg)
		if err == nil {
			t.Errorf("%s: expected fail-fast, build proceeded silently", tc.pkg)
			continue
		}
		assertAsmError(t, err, filepath.Join(src, tc.pkg), tc.pkg, "unsupported", tc.reason)
	}
}

// assertAsmError pins the fail-fast contract: the message must contain
// the file path, the package, the category (or "uncategorized"), and
// the reason.
func assertAsmError(t *testing.T, err error, file, pkg, category, reasonSub string) {
	t.Helper()
	msg := err.Error()
	for _, want := range []string{file, `"` + pkg + `"`, `"` + category + `"`, reasonSub} {
		if !strings.Contains(msg, want) {
			t.Errorf("fail-fast error missing %q:\n%s", want, msg)
		}
	}
	if !strings.Contains(msg, "asm fail-fast") {
		t.Errorf("fail-fast error missing the \"asm fail-fast\" marker:\n%s", msg)
	}
}

// TestAsmUncategorizedFails proves a .s file with no inventory entry
// fails fast instead of being silently ignored: (a) under a scratch
// GOROOT, (b) in a module-local package outside GOROOT.
func TestAsmUncategorizedFails(t *testing.T) {
	t.Run("scratch-goroot", func(t *testing.T) {
		fakeRoot := t.TempDir()
		pkgDir := filepath.Join(fakeRoot, "src", "fakelib")
		writeFiles(t, pkgDir, map[string]string{
			"newfeature_amd64.s": "TEXT ·newfeature(SB),$0-0\n\tRET\n",
		})
		loader := NewLoader()
		loader.goroot = fakeRoot
		_, err := loader.CheckAsmFiles(pkgDir, "fakelib")
		if err == nil {
			t.Fatal("uncategorized .s file under scratch GOROOT: build proceeded silently")
		}
		assertAsmError(t, err, filepath.Join(pkgDir, "newfeature_amd64.s"),
			"fakelib", "uncategorized", "no inventory entry")
	})
	t.Run("module-local", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"handasm_amd64.s": "TEXT ·handasm(SB),$0-0\n\tRET\n",
		})
		loader := NewLoader() // real GOROOT: dir is outside GOROOT/src
		_, err := loader.CheckAsmFiles(dir, "example.com/mod/handasm")
		if err == nil {
			t.Fatal("module-local .s file: build proceeded silently")
		}
		assertAsmError(t, err, filepath.Join(dir, "handasm_amd64.s"),
			"example.com/mod/handasm", "uncategorized", "outside the inventoried GOROOT/src")
	})
}

// TestAsmNonBuildableSkipped proves the buildability filter: .s files
// excluded for linux/amd64 (foreign-arch suffix, //go:build ignore)
// are NOT resolved -- even when uncategorized, they must not fail the
// build. (If the filter were dropped, this test would fail fast.)
func TestAsmNonBuildableSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"foreign_windows.s": "// no build tags; suffix excludes it on linux\nTEXT ·x(SB),$0-0\n\tRET\n",
		"ignored.s":         "//go:build ignore\n\nTEXT ·y(SB),$0-0\n\tRET\n",
		"future.s":          "//go:build go1.99\n\nTEXT ·z(SB),$0-0\n\tRET\n",
	})
	loader := NewLoader()
	res, err := loader.CheckAsmFiles(dir, "example.com/mod/noasm")
	if err != nil {
		t.Fatalf("non-buildable .s files must be skipped, got fail-fast: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("expected no resolutions, got %v", res)
	}
}

// TestAsmHookInLoader proves the fail-fast fires through the real
// importer path (mainRef/localRef), not just when CheckAsmFiles is
// called directly. The control module without .s files must load.
func TestAsmHookInLoader(t *testing.T) {
	buildModule := func(t *testing.T, withAsm bool) string {
		t.Helper()
		dir := t.TempDir()
		files := map[string]string{
			"go.mod":      "module example.com/asmhook\n\ngo 1.27\n",
			"main.go":     "package main\n\nimport \"example.com/asmhook/dep\"\n\nfunc main() { dep.Hello() }\n",
			"dep/dep.go":  "package dep\n\nfunc Hello() {}\n",
		}
		if withAsm {
			files["dep/evil_amd64.s"] = "TEXT ·evil(SB),$0-0\n\tRET\n"
		}
		writeFiles(t, dir, files)
		return dir
	}
	t.Run("control", func(t *testing.T) {
		dir := buildModule(t, false)
		loader := NewLoader()
		mainRef, err := loader.mainRef(dir)
		if err != nil {
			t.Fatalf("control mainRef: %v", err)
		}
		if _, err := loader.LoadClosure(dir, mainRef); err != nil {
			t.Fatalf("control LoadClosure: %v", err)
		}
	})
	t.Run("failfast", func(t *testing.T) {
		dir := buildModule(t, true)
		loader := NewLoader()
		mainRef, err := loader.mainRef(dir)
		if err != nil {
			t.Fatalf("mainRef should succeed (no .s in main dir): %v", err)
		}
		_, err = loader.LoadClosure(dir, mainRef)
		if err == nil {
			t.Fatal("dep with uncategorized .s: LoadClosure proceeded silently")
		}
		assertAsmError(t, err, filepath.Join(dir, "dep", "evil_amd64.s"),
			"example.com/asmhook/dep", "uncategorized", "outside the inventoried GOROOT/src")
	})
}

// TestAsmCompletenessCheckerDetectsFakeFile proves the completeness
// checker used by TestAsmInventoryComplete is capable of failing: run
// it against a scratch tree containing a fake .s file and require it
// to report exactly that file.
func TestAsmCompletenessCheckerDetectsFakeFile(t *testing.T) {
	inv, err := loadAsmInventory()
	if err != nil {
		t.Fatal(err)
	}
	fakeRoot := t.TempDir()
	writeFiles(t, fakeRoot, map[string]string{
		"src/fakepkg/sneaky_amd64.s": "TEXT ·sneaky(SB),$0-0\n\tRET\n",
	})
	uncovered := checkInventoryCoversList(t, filepath.Join(fakeRoot, "src"), inv)
	if len(uncovered) != 1 || uncovered[0] != "fakepkg/sneaky_amd64.s" {
		t.Fatalf("completeness checker failed to flag the fake file: %v", uncovered)
	}
}

// checkInventoryCoversList returns the .s files under root (paths
// relative to root) that have no inventory entry. Shared by the
// real-tree completeness test and the fake-file probe.
func checkInventoryCoversList(t *testing.T, root string, inv map[string]AsmEntry) []string {
	t.Helper()
	var missing []string
	for _, rel := range enumerateSFiles(t, root) {
		if _, ok := inv[rel]; !ok {
			missing = append(missing, rel)
		}
	}
	return missing
}
