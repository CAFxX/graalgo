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

// enumerateSFiles walks root and returns every assembly-file path
// (.s/.S/.sx, like go/build) relative to root, slash-separated and
// sorted.
func enumerateSFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if !d.IsDir() && (strings.HasSuffix(name, ".s") ||
			strings.HasSuffix(name, ".S") || strings.HasSuffix(name, ".sx")) {
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

// oracleAsmFiles is the independent go/build oracle for assembly
// selection: (*build.Context{GOOS:linux, GOARCH:amd64}).ImportDir, i.e.
// the SFiles the stock toolchain would assemble, plus the
// constraint-matched .S/.sx files go/build gates on cgo (they land in
// IgnoredOtherFiles when cgo is disabled; matched here via the public
// ctxt.MatchFile, which applies exactly go/build's goodOSArchFile +
// shouldBuild pre-filter). The loader is deliberately stricter than the
// cgo gate (see asm.go: no cgo/gcc path exists in graalgo), so the
// oracle for the loader's selection is the union.
func oracleAsmFiles(t *testing.T, dir string) []string {
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
	set := map[string]bool{}
	for _, f := range p.SFiles {
		set[f] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if !(strings.HasSuffix(name, ".S") || strings.HasSuffix(name, ".sx")) {
			continue
		}
		ok, err := ctxt.MatchFile(dir, name)
		if err != nil {
			t.Fatalf("oracle MatchFile(%s, %s): %v", dir, name, err)
		}
		if ok {
			set[name] = true
		}
	}
	var got []string
	for f := range set {
		got = append(got, f)
	}
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

// TestAsmSelectionMatchesGoBuildOracle verifies the assembly
// buildability filter (filename suffix + build constraints, over
// .s/.S/.sx) agrees file-for-file with the stock go/build oracle on
// real stdlib dirs -- on EVERY sampled dir, including unsupported and
// cgo dirs (a filter bug that spuriously dropped an unsupported file,
// i.e. a missing fail-fast, must be caught too). If the filter
// drifted, replaced files could be skipped or foreign-arch files could
// fail the build spuriously.
func TestAsmSelectionMatchesGoBuildOracle(t *testing.T) {
	src := gorootSrc(t)
	loader := NewLoader()
	for _, pkg := range []string{
		"runtime", "internal/bytealg", "internal/cpu", "math", "math/big",
		"sync/atomic", "crypto/sha1", "crypto/internal/fips140/aes",
		"hash/crc32", "syscall", "reflect", "internal/abi",
		"runtime/cgo",
	} {
		dir := filepath.Join(src, pkg)
		want := oracleAsmFiles(t, dir)
		got, err := loader.SelectAsmFiles(dir)
		if err != nil {
			t.Fatalf("%s: SelectAsmFiles: %v", pkg, err)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: selection mismatch:\n got  %v\n want %v", pkg, got, want)
		} else {
			t.Logf("%s: %d assembly files selected, oracle agrees", pkg, len(got))
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
		{"runtime/cgo", "cgo gcc-assembly"},
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

// TestAsmInventoryErrorNamesRealRegenScript is the regression test for
// codebase-review P1-2: the fail-fast error for a .s file with no
// inventory entry must name the real regen script
// (tools/stdlib-fork/gen-asm-inventory.py) and must not name the dead
// pre-migration path phases/phase3/stdlib-fork/... (that tree moved in
// the 2026-10-14 tools migration; naming it sends the user on a dead
// end on toolchain upgrade).
func TestAsmInventoryErrorNamesRealRegenScript(t *testing.T) {
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
	msg := err.Error()
	const realScript = "tools/stdlib-fork/gen-asm-inventory.py"
	if !strings.Contains(msg, realScript) {
		t.Errorf("fail-fast error must name the regen script %q:\n%s", realScript, msg)
	}
	if strings.Contains(msg, "phases/phase3/stdlib-fork") {
		t.Errorf("fail-fast error names the dead pre-migration script path:\n%s", msg)
	}
}

// TestAsmUppercaseSFailsFast is the regression test for the .S/.sx
// blind spot (P3-M6 remediation P1-1): go/build treats .s, .S and .sx
// identically as assembly, so a build-tagged evil_amd64.S must fail
// fast exactly like its lowercase twin -- never pass silently. (The
// pre-fix code ignored .S files entirely: this test failed with
// "build proceeded silently".)
func TestAsmUppercaseSFailsFast(t *testing.T) {
	t.Run("scratch-goroot", func(t *testing.T) {
		fakeRoot := t.TempDir()
		pkgDir := filepath.Join(fakeRoot, "src", "fakelib")
		writeFiles(t, pkgDir, map[string]string{
			"newfeature_amd64.S": "TEXT ·newfeature(SB),$0-0\n\tRET\n",
		})
		loader := NewLoader()
		loader.goroot = fakeRoot
		_, err := loader.CheckAsmFiles(pkgDir, "fakelib")
		if err == nil {
			t.Fatal("uncategorized .S file under scratch GOROOT: build proceeded silently")
		}
		assertAsmError(t, err, filepath.Join(pkgDir, "newfeature_amd64.S"),
			"fakelib", "uncategorized", "no inventory entry")
	})
	t.Run("module-local", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"evil_amd64.S": "TEXT ·evil(SB),$0-0\n\tRET\n",
		})
		loader := NewLoader() // real GOROOT: dir is outside GOROOT/src
		_, err := loader.CheckAsmFiles(dir, "example.com/mod/evil")
		if err == nil {
			t.Fatal("module-local .S file: build proceeded silently")
		}
		assertAsmError(t, err, filepath.Join(dir, "evil_amd64.S"),
			"example.com/mod/evil", "uncategorized", "outside the inventoried GOROOT/src")
	})
	t.Run("selection", func(t *testing.T) {
		// The .S/.sx files must be SELECTED (not skipped) for the
		// fail-fast to even see them.
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"evil_amd64.S":  "TEXT ·evil(SB),$0-0\n\tRET\n",
			"helper_amd64.sx": "TEXT ·helper(SB),$0-0\n\tRET\n",
		})
		names, err := NewLoader().SelectAsmFiles(dir)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(names, ",") != "evil_amd64.S,helper_amd64.sx" {
			t.Fatalf("SelectAsmFiles missed a .S/.sx file: %v", names)
		}
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
		// The .S/.sx suffixes get the same buildability filtering:
		// foreign-arch and constraint-excluded files stay skipped.
		"foreign_windows.S": "// no build tags; suffix excludes it on linux\nTEXT ·w(SB),$0-0\n\tRET\n",
		"ignored.S":         "//go:build ignore\n\nTEXT ·v(SB),$0-0\n\tRET\n",
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
// it against a scratch tree containing fake .s AND .S files and
// require it to report exactly those files.
func TestAsmCompletenessCheckerDetectsFakeFile(t *testing.T) {
	inv, err := loadAsmInventory()
	if err != nil {
		t.Fatal(err)
	}
	fakeRoot := t.TempDir()
	writeFiles(t, fakeRoot, map[string]string{
		"src/fakepkg/sneaky_amd64.s": "TEXT ·sneaky(SB),$0-0\n\tRET\n",
		"src/fakepkg/sneaky_amd64.S": "TEXT ·sneaky2(SB),$0-0\n\tRET\n",
	})
	uncovered := checkInventoryCoversList(t, filepath.Join(fakeRoot, "src"), inv)
	want := []string{"fakepkg/sneaky_amd64.S", "fakepkg/sneaky_amd64.s"}
	if strings.Join(uncovered, ",") != strings.Join(want, ",") {
		t.Fatalf("completeness checker failed to flag the fake files: %v", uncovered)
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
