package main

// Review 2026-10-30 P1-17: the v1 exporter emits package-level func
// init() as a side-effect-only globals entry (a void call node) that
// the truffle factory executes in sequence before main's body
// (NodeFactory.buildGlobalInits / buildProgram). The 2026-10-06 probe
// claiming "the runtime never executes init" predates that machinery.
// This test pins the exporter half of the contract: a program with
// func init() must export a globals entry calling init. It fails on an
// exporter that drops init (the old frozen behaviour) and it is the
// premise the p3frontend init narrowing relies on.

import (
	"encoding/json"
	"testing"
)

func TestV1InitExportedAsGlobalCall(t *testing.T) {
	src := `package main

var v int

func init() { v = 7 }

func main() {
	_ = v
}
`
	out := exportUnsafeProg(t, src)
	var doc struct {
		Globals []struct {
			Obj  int `json:"obj"`
			Init *struct {
				Kind   string `json:"kind"`
				Fields struct {
					Fun *struct {
						Kind   string `json:"kind"`
						Fields struct {
							Name string `json:"name"`
						} `json:"fields"`
					} `json:"fun"`
				} `json:"fields"`
			} `json:"init"`
		} `json:"globals"`
		Funcs []struct {
			Kind   string `json:"kind"`
			Fields struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"funcs"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("ExportFile output is not JSON: %v", err)
	}
	initDecl := false
	for _, f := range doc.Funcs {
		if f.Kind == "funcdecl" && f.Fields.Name == "init" {
			initDecl = true
		}
	}
	if !initDecl {
		t.Fatalf("exported document has no funcdecl named init")
	}
	for _, g := range doc.Globals {
		if g.Obj == -1 && g.Init != nil && g.Init.Kind == "call" &&
			g.Init.Fields.Fun != nil && g.Init.Fields.Fun.Kind == "ident" &&
			g.Init.Fields.Fun.Fields.Name == "init" {
			return // pinned: init is called from the globals sequence
		}
	}
	t.Errorf("exported document has no globals entry calling init (init would never run)")
}
