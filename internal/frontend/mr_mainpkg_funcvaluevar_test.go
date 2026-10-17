package main

// F-1 (codebase review 2026-10-17): a main package with a package-level
// multi-result function-value variable (`var myStat = os.Stat`) must
// export successfully, with call sites through the var desugared as the
// aliased Func (os.Stat → __stat helper), identically to a direct call.
//
// Red-green: before the fix, ExportFile failed with
// `gofrontend: variable "myStat" used before declaration` because
// etaExpandMRFuncValues removed the declaration but ExportFiles discarded
// the alias map instead of installing it on the exporter.

import (
	"testing"
)

const mrFuncValueVarProg = `package main

import (
	"fmt"
	"os"
)

var myStat = os.Stat

func main() {
	fi, err := myStat("/etc/hostname")
	if err != nil {
		fmt.Println("ERR:", err)
		return
	}
	fmt.Println("OK size:", fi.Size())
}
`

// callsInFunc returns every "call" node inside the named function.
func callsInFunc(doc map[string]any, fname string) []map[string]any {
	var out []map[string]any
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != fname {
			continue
		}
		out = append(out, findAllKind(fm, "call")...)
	}
	return out
}

// countHelperCalls counts calls whose fun is the named __-helper.
func countHelperCalls(calls []map[string]any, helper string) int {
	n := 0
	for _, c := range calls {
		fun, _ := c["fields"].(map[string]any)["fun"].(map[string]any)
		if fun == nil {
			continue
		}
		if name, _ := fun["fields"].(map[string]any)["name"].(string); name == helper {
			n++
		}
	}
	return n
}

// TestMainPackageMRFuncValueVarExports pins that the aliased call
// desugars to the same __-helper as a direct os.Stat call.
func TestMainPackageMRFuncValueVarExports(t *testing.T) {
	out, err := ExportFile(writeIfaceProg(t, mrFuncValueVarProg))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	// The call through myStat must desugar to the os.Stat table helper.
	if got := countHelperCalls(callsInFunc(doc, "main"), "__stat"); got != 1 {
		t.Fatalf("want 1 __stat helper call in main (call through myStat), got %d", got)
	}
	// The removed declaration must not leak a package var for myStat.
	for _, n := range findAllKind(doc, "vardecl") {
		names, _ := n["fields"].(map[string]any)["names"].([]any)
		for _, nm := range names {
			if s, _ := nm.(string); s == "myStat" {
				t.Fatalf("exported a package var for myStat; declaration should be removed with call sites desugared")
			}
		}
	}
}

// TestMainPackageMRFuncValueVarMatchesDirect pins wire-shape parity:
// the desugared `myStat(path)` call must produce the same helper-call
// shape as a direct `os.Stat(path)` call in the same program.
func TestMainPackageMRFuncValueVarMatchesDirect(t *testing.T) {
	const both = `package main

import (
	"fmt"
	"os"
)

var myStat = os.Stat

func main() {
	fi1, err1 := os.Stat("/etc/hostname")
	fi2, err2 := myStat("/etc/hostname")
	fmt.Println(fi1.Size(), fi2.Size(), err1 == nil, err2 == nil)
}
`
	out, err := ExportFile(writeIfaceProg(t, both))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	if got := countHelperCalls(callsInFunc(doc, "main"), "__stat"); got != 2 {
		t.Fatalf("want 2 __stat helper calls in main (direct + aliased), got %d", got)
	}
}
