package main

// REVIEW-2026-10-18-14 (review14-runtime-alias) regression tests: the
// runtime intrinsic interceptions in exportCall and the defer/go Gosched
// desugar in isGoschedCall must discriminate on the resolved object via
// types.Info, never on the package identifier's spelling (the R2-P2-9
// Goexit precedent, TestGoexitRenamedImport). An aliased import
// (rt "runtime") intercepts exactly like the canonical spelling; a
// shadowed local named runtime does not.

import (
	"strings"
	"testing"
)

// TestRuntimeIntrinsicRenamedImport: each intrinsic must lower identically
// under `import "runtime"` and `import rt "runtime"`.
func TestRuntimeIntrinsicRenamedImport(t *testing.T) {
	cases := []struct {
		name      string
		canonical string
		renamed   string
		want      string // wire-JSON marker present in both exports
	}{
		{
			name:      "GC-expr",
			canonical: "package main\n\nimport \"runtime\"\n\nfunc main() {\n\truntime.GC()\n}\n",
			renamed:   "package main\n\nimport rt \"runtime\"\n\nfunc main() {\n\trt.GC()\n}\n",
			want:      `"gcinterop": "GC"`,
		},
		{
			name:      "KeepAlive-expr",
			canonical: "package main\n\nimport \"runtime\"\n\nfunc main() {\n\tx := 42\n\truntime.KeepAlive(x)\n}\n",
			renamed:   "package main\n\nimport rt \"runtime\"\n\nfunc main() {\n\tx := 42\n\trt.KeepAlive(x)\n}\n",
			want:      `"gcinterop": "KeepAlive"`,
		},
		{
			name:      "NumCPU-expr",
			canonical: "package main\n\nimport \"runtime\"\n\nfunc main() {\n\tn := runtime.NumCPU()\n\t_ = n\n}\n",
			renamed:   "package main\n\nimport rt \"runtime\"\n\nfunc main() {\n\tn := rt.NumCPU()\n\t_ = n\n}\n",
			want:      `"kind": "runtimenumcpu"`,
		},
		{
			name:      "Gosched-expr",
			canonical: "package main\n\nimport \"runtime\"\n\nfunc main() {\n\truntime.Gosched()\n}\n",
			renamed:   "package main\n\nimport rt \"runtime\"\n\nfunc main() {\n\trt.Gosched()\n}\n",
			want:      `"kind": "runtimegosched"`,
		},
		{
			name:      "Gosched-defer-go",
			canonical: "package main\n\nimport \"runtime\"\n\nfunc main() {\n\tdefer runtime.Gosched()\n\tgo runtime.Gosched()\n}\n",
			renamed:   "package main\n\nimport rt \"runtime\"\n\nfunc main() {\n\tdefer rt.Gosched()\n\tgo rt.Gosched()\n}\n",
			want:      `"kind": "runtimegosched"`,
		},
	}
	for _, tc := range cases {
		for _, spelling := range []struct{ name, src string }{
			{"canonical", tc.canonical},
			{"renamed", tc.renamed},
		} {
			t.Run(tc.name+"/"+spelling.name, func(t *testing.T) {
				out, err := ExportFile(writeSyncProg(t, spelling.src))
				if err != nil {
					t.Fatalf("ExportFile: %v", err)
				}
				if !strings.Contains(out, tc.want) {
					t.Errorf("exported JSON missing %q (interception missed under %s spelling)", tc.want, spelling.name)
				}
			})
		}
	}
}

// TestRuntimeShadowedLocalNotIntercepted: a local variable named runtime
// must NOT trigger the runtime intrinsic interceptions — the gate fails
// closed when sel.X does not resolve to a *types.PkgName. (No imports:
// stdlib bodies like fmt pull in their own canonical runtime calls, which
// would legitimately carry gcinterop markers and pollute the assertion.)
func TestRuntimeShadowedLocalNotIntercepted(t *testing.T) {
	src := `package main

type shadowRt struct{}

func (shadowRt) GC() { println("shadow") }

func main() {
	runtime := shadowRt{}
	runtime.GC()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if strings.Contains(out, `"gcinterop"`) {
		t.Error("shadowed local runtime.GC() was intercepted as a runtime intrinsic")
	}
	if strings.Contains(out, `"kind": "runtimegosched"`) {
		t.Error("shadowed local runtime.Gosched() was intercepted as a runtime intrinsic")
	}
}

// TestRuntimeShadowedLocalGoschedNotIntercepted: `defer runtime.Gosched()`
// / `go runtime.Gosched()` with a shadowed runtime must take the ordinary
// defer/go path, not the runtimegosched desugar.
func TestRuntimeShadowedLocalGoschedNotIntercepted(t *testing.T) {
	src := `package main

type shadowRt struct{}

func (shadowRt) Gosched() {}

func main() {
	runtime := shadowRt{}
	defer runtime.Gosched()
	go runtime.Gosched()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if strings.Contains(out, `"kind": "runtimegosched"`) {
		t.Error("shadowed local runtime.Gosched() was intercepted as a runtime intrinsic in defer/go position")
	}
	if n := strings.Count(out, `"kind": "gostmt"`); n != 1 {
		t.Errorf("exported JSON has %d gostmt nodes, want 1 (ordinary go statement for shadowed call)", n)
	}
}
