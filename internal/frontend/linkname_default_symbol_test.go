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
// Scope pins (L-L F6): bodyless METHODS fail export (methods bind
// through method tables, not the symbol table), and a multi-result
// defaulted declaration fails export too (LinknameRootNode dispatch
// returns one value and a defaulted declaration never aliases to a
// compiled provider — internal/cpu.cpuid is that shape, allow-listed
// in export.go as never-called, not stubbed by default).

import (
	"encoding/json"
	"go/token"
	"go/types"
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

func TestBodylessMethodFailsExport(t *testing.T) {
	// L-L F6: a bodyless method with no linkname symbol and no
	// provider fails export instead of becoming a guest panic stub.
	// (Previously this pinned the stub form; the stub is now reserved
	// for the reviewed never-called allow-list.)
	src := `package main

type T struct{}

func (T) M() int

var _ interface{ M() int } = T{}

func main() {}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err == nil {
		t.Fatal("ExportFile(bodyless method): got nil error, want a no-provider failure")
	} else if !strings.Contains(err.Error(), "no provider") && !strings.Contains(err.Error(), "no linkname symbol") {
		t.Fatalf("ExportFile(bodyless method) error = %q, want it to name the missing provider/symbol", err)
	}
}

func TestBodylessMultiResultDefaultFailsExport(t *testing.T) {
	// The internal/cpu.cpuid shape: multi-result, no directive.
	// Dispatch cannot carry multiple results and a defaulted
	// declaration never aliases — but unless the declaration is on
	// the reviewed never-called allow-list (internal/cpu's own
	// decls, not this main-package one), export now fails instead of
	// emitting a panic stub with pair synthesis for its callers.
	src := `package main

func pair() (int, int)

func main() {
	_ = pair
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err == nil {
		t.Fatal("ExportFile(multi-result defaulted decl): got nil error, want a no-provider failure")
	} else if !strings.Contains(err.Error(), "no provider") {
		t.Fatalf("ExportFile(multi-result defaulted decl) error = %q, want it to name the missing provider", err)
	}
}

// TestLinknameStubAllowlist pins the allow-list predicate: exactly the
// reviewed never-called entries pass, and lookalikes do not.
func TestLinknameStubAllowlist(t *testing.T) {
	mkFunc := func(pkgPath, name string, recv *types.Named) *funcDeclInfo {
		pkg := types.NewPackage(pkgPath, pkgPath)
		var recvVar *types.Var
		if recv != nil {
			recvVar = types.NewVar(token.NoPos, pkg, "", recv)
		}
		sig := types.NewSignatureType(recvVar, nil, nil, nil, nil, false)
		return &funcDeclInfo{obj: types.NewFunc(token.NoPos, pkg, name, sig), sig: sig}
	}
	mainPkg := types.NewPackage("main", "main")
	recvT := types.NewNamed(types.NewTypeName(token.NoPos, mainPkg, "T", types.Typ[types.Int]), types.Typ[types.Int], nil)
	for _, tc := range []struct {
		pkgPath string
		key     string
		fi      *funcDeclInfo
		want    bool
	}{
		{"internal/cpu", "internal/cpu.cpuid", mkFunc("internal/cpu", "cpuid", nil), true},
		{"internal/cpu", "internal/cpu.xgetbv", mkFunc("internal/cpu", "xgetbv", nil), true},
		{"internal/cpu", "internal/cpu.doinit", mkFunc("internal/cpu", "doinit", nil), false},
		{"main", "main.pair", mkFunc("main", "pair", nil), false},
		{"main", "main.T.M", mkFunc("main", "M", recvT), false},
	} {
		if gotKey := linknameStubKey(tc.pkgPath, tc.fi); gotKey != tc.key {
			t.Errorf("linknameStubKey = %q, want %q", gotKey, tc.key)
		}
		if _, ok := linknameStubAllowed(tc.pkgPath, tc.fi); ok != tc.want {
			t.Errorf("linknameStubAllowed(%q) = %v, want %v", tc.key, ok, tc.want)
		}
	}
}
