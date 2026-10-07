package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Red-green evidence for the P3-M8 unsafe bring-up (builtin design).
// Run in an isolated tree copy with staged patches 01/02/04 applied.

func exportSrc(t *testing.T, src string) (string, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "p.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return ExportFile(p)
}

func TestUnsafeMembersPresent(t *testing.T) {
	pkg := syntheticUnsafePackage()
	for _, name := range []string{"Pointer", "Sizeof", "Alignof", "Offsetof", "Slice", "SliceData", "String", "StringData", "Add"} {
		if pkg.Scope().Lookup(name) == nil {
			t.Errorf("synthetic unsafe package lacks %s", name)
		}
	}
	for _, name := range []string{"Slice", "Sizeof", "Add"} {
		obj := pkg.Scope().Lookup(name)
		if !isUnsafeIntrinsic(obj) {
			t.Errorf("%s is not recognized as an unsafe intrinsic (got %T)", name, obj)
		}
	}
}

func TestUnsafeSizeofConstFold(t *testing.T) {
	out, err := exportSrc(t, `package main

import "unsafe"

const c = unsafe.Sizeof(int(0))

var _ [unsafe.Alignof(int64(0))]byte
var _ = unsafe.Offsetof(struct{ a int8; b int64 }{}.b)

func main() { println(c) }
`)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// go/types folds Sizeof/Alignof/Offsetof to constants: no wire nodes.
	for _, kind := range []string{"unsafeslice", "unsafestring", "unsafeadd"} {
		if strings.Contains(out, `"`+kind+`"`) {
			t.Errorf("unexpected wire node %s in const-folded output", kind)
		}
	}
}

func TestUnsafeConversionsWire(t *testing.T) {
	out, err := exportSrc(t, `package main

import "unsafe"

func main() {
	p := unsafe.Pointer(new(int))
	q := (*int)(p)
	_ = q
	_ = unsafe.Pointer(p)
}
`)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	for _, kind := range []string{`"unsafefromptr"`, `"unsafetooptr"`} {
		if !strings.Contains(out, kind) {
			t.Errorf("expected wire node %s", kind)
		}
	}
}

func TestUnsafePointerNil(t *testing.T) {
	out, err := exportSrc(t, `package main

import "unsafe"

func main() {
	var p unsafe.Pointer = unsafe.Pointer(nil)
	println(p == nil)
}
`)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(out, `"unsafefromptr"`) {
		t.Errorf("expected unsafefromptr for unsafe.Pointer(nil)")
	}
}

func TestUnsafeSliceWire(t *testing.T) {
	out, err := exportSrc(t, `package main

import "unsafe"

func main() {
	s := []byte{1, 2}
	p := unsafe.SliceData(s)
	s2 := unsafe.Slice(p, 2)
	println(len(s2))
	st := "hi"
	q := unsafe.StringData(st)
	st2 := unsafe.String(q, 2)
	println(st2)
	_ = unsafe.Add(unsafe.Pointer(p), 0)
}
`)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	for _, kind := range []string{`"unsafeslice"`, `"unsafeslicedata"`, `"unsafestring"`, `"unsafestringdata"`, `"unsafeadd"`} {
		if !strings.Contains(out, kind) {
			t.Errorf("expected wire node %s", kind)
		}
	}
}

// gc-exact rejections inherited from go/types' builtin checking.
func TestUnsafeGcExactRejections(t *testing.T) {
	cases := []struct{ name, src string }{
		{"explicit type args", "package main\nimport \"unsafe\"\nfunc main() {\n p := new(byte)\n _ = unsafe.Slice[byte](p, 1)\n}\n"},
		{"negative const len", "package main\nimport \"unsafe\"\nfunc main() {\n p := new(byte)\n _ = unsafe.Slice(p, -1)\n}\n"},
		{"builtin in value position", "package main\nimport \"unsafe\"\nvar _ = unsafe.Slice\nfunc main() {}\n"},
		{"String needs *byte", "package main\nimport \"unsafe\"\nfunc main() {\n p := new(int)\n _ = unsafe.String(p, 1)\n}\n"},
		{"SliceData needs slice", "package main\nimport \"unsafe\"\nfunc main() {\n _ = unsafe.SliceData(42)\n}\n"},
		{"Add needs Pointer", "package main\nimport \"unsafe\"\nfunc main() {\n p := new(int)\n _ = unsafe.Add(p, 1)\n}\n"},
	}
	for _, tc := range cases {
		if _, err := exportSrc(t, tc.src); err == nil {
			t.Errorf("%s: expected a type error, got nil", tc.name)
		}
	}
}

func TestUnsafeGenericSizeofScopeError(t *testing.T) {
	_, err := exportSrc(t, `package main

import "unsafe"

func gsize[T any](v T) uintptr { return unsafe.Sizeof(v) }

func main() { println(gsize(1)) }
`)
	if err == nil || !strings.Contains(err.Error(), "unsafe.Sizeof") {
		t.Errorf("expected named unsafe.Sizeof scope error, got: %v", err)
	}
}
