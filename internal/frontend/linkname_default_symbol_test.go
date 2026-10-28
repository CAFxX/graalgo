package main

// fix-redmaster-r41: a bodyless plain function without a //go:linkname
// directive exports under gc's default symbol (pkgPath + "." + name)
// as a linkname dispatch, not as an "unimplemented" panic stub. In gc
// such declarations are the runtime-provided surface — the linker
// binds them from the runtime (reflect's unsafe_New/typedmemmove/
// resolveNameOff are pushed in exactly this shape, with no directive
// on the reflect side). The stub classification killed live calls
// with the degraded panic (?) 0xADDR: packEfaceData (every
// Value.Interface of an addressable element, hence fmt %v of slices)
// and Type.String (nameOff).
//
// Scope pins: bodyless METHODS keep the stub (methods bind through
// method tables, not the symbol table), and a multi-result defaulted
// declaration keeps the stub too (LinknameRootNode dispatch returns
// one value and a defaulted declaration never aliases to a compiled
// provider — internal/cpu.cpuid is that shape).

import (
	"encoding/json"
	"strings"
	"testing"
)

// funcDeclsBySymbol decodes an export and indexes its funcdecls by
// "<pkgpath>.<name>".
func funcDeclsBySymbol(t *testing.T, out string) map[string]map[string]any {
	t.Helper()
	var doc any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("export is not valid JSON: %v", err)
	}
	decls := map[string]map[string]any{}
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if fields, ok := v["fields"].(map[string]any); ok {
					pkg, _ := fields["pkgpath"].(string)
					name, _ := fields["name"].(string)
					decls[pkg+"."+name] = fields
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(doc)
	return decls
}

func TestBodylessReflectHelpersExportAsLinknames(t *testing.T) {
	src := `package main

import (
	"fmt"
	"reflect"
)

func main() {
	s := []int{10, 20, 30}
	fmt.Println(reflect.ValueOf(s).Index(1).Interface())
	fmt.Println(reflect.TypeOf("hi").String())
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	decls := funcDeclsBySymbol(t, out)
	for _, sym := range []string{
		"reflect.unsafe_New", "reflect.unsafe_NewArray",
		"reflect.typedmemmove", "reflect.resolveNameOff",
		"reflect.resolveTypeOff", "reflect.resolveTextOff",
		"reflect.addReflectOff",
		"internal/reflectlite.unsafe_New",
		"internal/reflectlite.typedmemmove",
		"internal/reflectlite.resolveNameOff",
		"internal/reflectlite.resolveTypeOff",
	} {
		fields, ok := decls[sym]
		if !ok {
			t.Errorf("no funcdecl exported for %s", sym)
			continue
		}
		if fields["unimplemented"] == true {
			t.Errorf("%s exported as unimplemented stub; want linkname dispatch", sym)
		}
		if fields["linkname"] != sym {
			t.Errorf("%s linkname = %v; want %q", sym, fields["linkname"], sym)
		}
	}
}

func TestBodylessPlainFuncDefaultSymbol(t *testing.T) {
	// A bodyless plain function in any package (here main itself)
	// takes the default symbol; the sync lane's runtime_* surface is
	// the same shape and stays dispatched.
	src := `package main

import "sync"

func asmHelper(x int) int

var mu sync.Mutex

func main() {
	mu.Lock()
	mu.Unlock()
	_ = asmHelper
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	decls := funcDeclsBySymbol(t, out)
	if fields, ok := decls["main.asmHelper"]; !ok {
		t.Errorf("no funcdecl exported for main.asmHelper")
	} else {
		if fields["unimplemented"] == true {
			t.Errorf("main.asmHelper exported as unimplemented stub; want linkname dispatch")
		}
		if fields["linkname"] != "main.asmHelper" {
			t.Errorf("main.asmHelper linkname = %v; want main.asmHelper", fields["linkname"])
		}
	}
	if fields, ok := decls["sync.runtime_Semacquire"]; !ok {
		t.Errorf("no funcdecl exported for sync.runtime_Semacquire")
	} else if fields["linkname"] != "sync.runtime_Semacquire" {
		t.Errorf("sync.runtime_Semacquire linkname = %v; want sync.runtime_Semacquire",
			fields["linkname"])
	}
}

func TestBodylessMethodStaysUnimplemented(t *testing.T) {
	src := `package main

type T struct{}

func (T) M() int

var _ interface{ M() int } = T{}

func main() {}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if !strings.Contains(out, "M") {
		t.Fatalf("export does not mention the method at all")
	}
	decls := funcDeclsBySymbol(t, out)
	found := false
	for sym, fields := range decls {
		if strings.HasSuffix(sym, ".M") || strings.Contains(sym, "T).M") {
			found = true
			if fields["linkname"] != nil {
				t.Errorf("bodyless method %s exported with linkname %v; methods must keep the stub",
					sym, fields["linkname"])
			}
			if fields["unimplemented"] != true {
				t.Errorf("bodyless method %s: unimplemented = %v; want true (stub form)",
					sym, fields["unimplemented"])
			}
		}
	}
	if !found {
		t.Errorf("bodyless method decl not present in export")
	}
}

func TestBodylessMultiResultDefaultStaysUnimplemented(t *testing.T) {
	// The internal/cpu.cpuid shape: multi-result, no directive.
	// Dispatch cannot carry multiple results and a defaulted
	// declaration never aliases, so the stub form (with its pair
	// synthesis for callers) must stand.
	src := `package main

func pair() (int, int)

func main() {
	_ = pair
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	decls := funcDeclsBySymbol(t, out)
	fields, ok := decls["main.pair"]
	if !ok {
		t.Fatalf("no funcdecl exported for main.pair")
	}
	if fields["linkname"] != nil {
		t.Errorf("multi-result defaulted decl exported with linkname %v; want stub form",
			fields["linkname"])
	}
	if fields["unimplemented"] != true {
		t.Errorf("multi-result defaulted decl: unimplemented = %v; want true (stub form)",
			fields["unimplemented"])
	}
}
