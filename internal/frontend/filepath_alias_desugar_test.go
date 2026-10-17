package main

import (
	"strings"
	"testing"
)

// TestDiskPkgFuncVarAliasDesugar (RV-P0-03): a program importing
// path/filepath (disk-imported) must export successfully. The real
// path/filepath declares `var lstat = os.Lstat`; etaExpandMRFuncValues
// removes the declaration and records the alias, and call sites through
// the var must desugar via the os table (helper call) — not via the
// mrUser plan, which would exportCall the removed var and fail with
// `variable "lstat" used before declaration`. Red-green: fails before
// the classifyMultiResultCall reorder, passes after.
func TestDiskPkgFuncVarAliasDesugar(t *testing.T) {
	const src = `package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
)

func main() {
	fmt.Println(filepath.Join("a", "b"))
	// Walk exercises the lstat call sites inside path/filepath.
	_ = filepath.Walk(".", func(path string, info fs.FileInfo, err error) error { return nil })
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile(path/filepath): %v", err)
	}
	if strings.Contains(out, "used before declaration") {
		t.Errorf("exported wire references the removed lstat var")
	}
	// The lstat call sites must desugar to the synthetic os helper.
	if !strings.Contains(out, "__lstat") {
		t.Errorf("exported wire missing desugared __lstat helper call")
	}
}
