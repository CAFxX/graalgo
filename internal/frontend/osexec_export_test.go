package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestOsExecImportExports (review-1105-osexec-synthos): any program
// importing os/exec must export. Pre-fix it failed type-checking
// os/exec with "undefined: os.ErrProcessDone" because the synthetic
// os var block lacked that sentinel (gc os/exec.go: ErrProcessDone =
// errors.New("os: process already finished")). The program also uses
// filepath.Walk, whose body export exercises the aliased-var desugar
// for path/filepath's `var lstat = os.Lstat` (see
// TestOsExecSyntheticOsCoverage): pre-fix that leg failed with
// `variable "lstat" used before declaration` because
// classifyMultiResultCall resolved the aliased call to an mrUser plan
// (normal call export, no such callee) instead of the os-table helper
// plan. The done-gate here is export only: the synthetic os Process
// methods remain documented M8 fail-fast stubs, so this test never
// runs a spawned process.
func TestOsExecImportExports(t *testing.T) {
	src := `package main

import (
	exec "os/exec"
	"path/filepath"
)

var _ = exec.Command
var _ = filepath.Walk

func main() {}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "p_exec.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile(os/exec program): %v", err)
	}
	// The os/exec Command funcdecl and the filepath walk body's
	// desugared lstat call (via the os.Lstat __-helper) must both be
	// in the wire document; their absence would mean the import was
	// only type-checked, not exported.
	for _, want := range []string{`"Command"`, "__lstat"} {
		if !strings.Contains(out, want) {
			t.Fatalf("export missing %s", want)
		}
	}
}

// TestOsExecSyntheticOsCoverage: every package-level os symbol the
// real os/exec package references on linux must exist in the synthetic
// os scope. The reference set is derived live from the toolchain's
// os/exec sources (runtime.GOROOT), restricted to the files built on
// linux: exec.go and lookpath.go (untagged), exec_unix.go
// (!plan9 && !windows), lp_unix.go (unix). read3.go is
// //go:build ignore and the plan9/windows/wasm files are not built on
// linux, so they contribute no requirements here. A missing symbol
// fails user programs at import time with "undefined: os.<name>".
func TestOsExecSyntheticOsCoverage(t *testing.T) {
	goroot := runtime.GOROOT()
	execDir := filepath.Join(goroot, "src", "os", "exec")
	if st, err := os.Stat(execDir); err != nil || !st.IsDir() {
		t.Skipf("no os/exec sources under GOROOT %q", goroot)
	}
	refRe := regexp.MustCompile(`\bos\.([A-Z][A-Za-z0-9_]*)\b`)
	refs := map[string]bool{}
	for _, f := range []string{"exec.go", "exec_unix.go", "lookpath.go", "lp_unix.go"} {
		data, err := os.ReadFile(filepath.Join(execDir, f))
		if err != nil {
			t.Fatalf("read os/exec/%s: %v", f, err)
		}
		for _, m := range refRe.FindAllSubmatch(data, -1) {
			refs[string(m[1])] = true
		}
	}
	if len(refs) == 0 {
		t.Fatal("no os.* references found in os/exec sources")
	}
	sp := getSyntheticPackage("os")
	scope := sp.pkg.Scope()
	var missing []string
	for name := range refs {
		if scope.Lookup(name) == nil {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("synthetic os lacks symbols referenced by os/exec: %v (refs=%v)", missing, refs)
	}
	// ErrProcessDone must be an error value with gc's exact message
	// behind it; the scope check above only proves the name exists.
	// The message is pinned by the export test's success against gc
	// os/exec, which errors.Is-matches on it; here assert the type.
	obj := scope.Lookup("ErrProcessDone")
	if obj == nil {
		t.Fatal("synthetic os lacks ErrProcessDone")
	}
	if obj.Type().String() != "error" {
		t.Fatalf("ErrProcessDone type = %s, want error", obj.Type())
	}
}
