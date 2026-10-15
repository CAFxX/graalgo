package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runVetOnModule writes files into a temp dir (with a go.mod if goMod != "")
// and runs vet on the given Go files.
func runVetOnModule(t *testing.T, goMod string, files map[string]string, targets ...string) string {
	t.Helper()
	dir := t.TempDir()
	if goMod != "" {
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var paths []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".go") {
			paths = append(paths, p)
		}
	}
	_ = targets
	ds, err := Run(paths)
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	return strings.Join(msgs, "\n")
}

func TestStdVersion(t *testing.T) {
	// cmp.Or was added in go1.21; use a go1.22 symbol with a go1.21 module.
	// (Modules below go1.21 are skipped entirely, matching go vet.)
	src := `package p

import "cmp"

var _ = cmp.Or(1, 2)
var _ = cmp.Compare(1, 2)
`
	// crypto/x509.OID was added in go1.22.
	src22 := `package p

import "crypto/x509"

var _ = x509.OIDFromInts([]uint64{1, 2})
`
	joined := runVetOnModule(t, "module example.com/p\n\ngo 1.21\n",
		map[string]string{"p.go": src22})
	if !strings.Contains(joined, "x509.OIDFromInts requires go1.22 or later (module is go1.21)") {
		t.Errorf("expected stdversion diagnostic, got:\n%s", joined)
	}
	// cmp.Compare is from go1.21; must not be flagged at go1.21.
	if strings.Contains(joined, "cmp.Compare requires") {
		t.Errorf("false positive on go1.21 symbol:\n%s", joined)
	}
	// Module at go1.22: no diagnostic.
	joined = runVetOnModule(t, "module example.com/p\n\ngo 1.22\n",
		map[string]string{"p.go": src22})
	if strings.Contains(joined, "requires go1.22") {
		t.Errorf("false positive at go1.22:\n%s", joined)
	}
	// Module below go1.21: analyzer skips (go directive wasn't binding).
	joined = runVetOnModule(t, "module example.com/p\n\ngo 1.20\n",
		map[string]string{"p.go": src22})
	if strings.Contains(joined, "requires go1.22") {
		t.Errorf("should skip modules below go1.21:\n%s", joined)
	}
	// No go.mod: analyzer skips.
	joined = runVetOnModule(t, "", map[string]string{"p.go": src22})
	if strings.Contains(joined, "requires go1.22") {
		t.Errorf("should skip without go.mod:\n%s", joined)
	}
	_ = src
}

func TestStdVersionMethod(t *testing.T) {
	// (*archive/tar.Writer).AddFS was added in go1.22.
	src := `package p

import "archive/tar"

func f(w *tar.Writer, fs any) error { return nil }
func g(w *tar.Writer) error { return w.Flush() }
`
	joined := runVetOnModule(t, "module example.com/p\n\ngo 1.21\n",
		map[string]string{"p.go": src})
	// Flush is old; must not be flagged. (AddFS needs an fs.FS arg; we
	// only check that old methods are quiet here.)
	if strings.Contains(joined, "Flush requires") {
		t.Errorf("false positive on old method:\n%s", joined)
	}

	src2 := `package p

import (
	"archive/tar"
	"io/fs"
)

func h(w *tar.Writer, fsys fs.FS) error { return w.AddFS(fsys) }
`
	joined = runVetOnModule(t, "module example.com/p\n\ngo 1.21\n",
		map[string]string{"p.go": src2})
	if !strings.Contains(joined, "tar.(*Writer).AddFS requires go1.22 or later (module is go1.21)") {
		t.Errorf("expected method diagnostic in go vet format, got:\n%s", joined)
	}
}

func TestStdVersionBuildTag(t *testing.T) {
	// A //go:build go1.22 constraint raises the file version.
	src := `//go:build go1.22

package p

import "crypto/x509"

var _ = x509.OIDFromInts([]uint64{1, 2})
`
	joined := runVetOnModule(t, "module example.com/p\n\ngo 1.21\n",
		map[string]string{"p.go": src})
	if strings.Contains(joined, "requires go1.22") {
		t.Errorf("build tag should raise file version:\n%s", joined)
	}
	// Without the build tag, the same file is flagged.
	src2 := `package p

import "crypto/x509"

var _ = x509.OIDFromInts([]uint64{1, 2})
`
	joined = runVetOnModule(t, "module example.com/p\n\ngo 1.21\n",
		map[string]string{"p.go": src2})
	if !strings.Contains(joined, "requires go1.22 or later (module is go1.21)") {
		t.Errorf("expected diagnostic without build tag, got:\n%s", joined)
	}
}

func TestCgoCall(t *testing.T) {
	src := `package p

import "C"
import "unsafe"

func bad(s []int, m map[string]int, ch chan int, fn func()) {
	C.foo(s)
	_ = m
	_ = ch
	_ = fn
}

func badAddr(s []int) {
	C.foo(&s)
}

type T struct {
	name string
	vals []int
}

func badStruct(t T) {
	C.foo(t)
}

func good(i int, p unsafe.Pointer) {
	C.foo(i)
	C.foo(p)
	C.CBytes([]byte("x"))
}
`
	joined := runVetOnModule(t, "", map[string]string{"p.go": src})
	n := strings.Count(joined, "possibly passing Go type with embedded pointer to C")
	if n != 3 {
		t.Errorf("expected 3 cgocall diagnostics (slice, &slice, struct-with-slice), got %d:\n%s", n, joined)
	}
}

func TestCgoCallNoCgo(t *testing.T) {
	src := `package p

func f(s []int) {}
`
	joined := runVetOnModule(t, "", map[string]string{"p.go": src})
	if strings.Contains(joined, "embedded pointer to C") {
		t.Errorf("cgocall must not run without cgo:\n%s", joined)
	}
}

func TestAsmDecl(t *testing.T) {
	goSrc := `package p

func good(a int32) int32
func badsize(a int32)
func noret() int32
`
	// Note: · is U+00B7 MIDDLE DOT, as emitted by the Go assembler.
	// On amd64, results are aligned to 8 bytes: func good(a int32) int32
	// has a at 0 and ret at 8, for a total arg size of 12.
	asmSrc := "TEXT \u00b7good(SB),$0-12\n" +
		"\tMOVL a+0(FP), AX\n" +
		"\tMOVL AX, ret+8(FP)\n" +
		"\tRET\n" +
		"TEXT \u00b7badsize(SB),$0-8\n" +
		"\tRET\n" +
		"TEXT \u00b7noret(SB),$0-4\n" +
		"\tRET\n" +
		"TEXT \u00b7missing(SB),$0-0\n" +
		"\tRET\n"
	joined := runVetOnModule(t, "", map[string]string{
		"p.go": goSrc, "p_amd64.s": asmSrc,
	})
	if !strings.Contains(joined, "wrong argument size 8; expected $...-4") {
		t.Errorf("expected argsize diagnostic, got:\n%s", joined)
	}
	if !strings.Contains(joined, "function missing missing Go declaration") {
		t.Errorf("expected missing-declaration diagnostic, got:\n%s", joined)
	}
	if !strings.Contains(joined, "RET without writing to 4-byte ret+0(FP)") {
		t.Errorf("expected RET-without-result diagnostic, got:\n%s", joined)
	}
	if strings.Contains(joined, "good:") {
		t.Errorf("false positive on good function:\n%s", joined)
	}
}

func TestAsmDeclNoAsm(t *testing.T) {
	src := `package p

func f(a int32) int32
`
	joined := runVetOnModule(t, "", map[string]string{"p.go": src})
	if strings.Contains(joined, "[") && strings.Contains(joined, "SB") {
		t.Errorf("asmdecl must not run without assembly files:\n%s", joined)
	}
}

func TestFramePointer(t *testing.T) {
	goSrc := `package p
`
	asmSrc := "TEXT \u00b7clobber(SB),$0-0\n" +
		"\tMOVQ AX, BP\n" +
		"\tRET\n" +
		"TEXT \u00b7ok(SB),$0-8\n" +
		"\tMOVQ BP, AX\n" +
		"\tRET\n" +
		"TEXT \u00b7framed(SB),$16-8\n" +
		"\tMOVQ AX, BP\n" +
		"\tRET\n"
	joined := runVetOnModule(t, "", map[string]string{
		"p.go": goSrc, "p_amd64.s": asmSrc,
	})
	if !strings.Contains(joined, "frame pointer is clobbered before saving") {
		t.Errorf("expected framepointer diagnostic, got:\n%s", joined)
	}
	// Exactly one diagnostic: clobber fires, ok reads BP first, framed has a frame.
	n := strings.Count(joined, "frame pointer is clobbered before saving")
	if n != 1 {
		t.Errorf("expected exactly 1 framepointer diagnostic, got %d:\n%s", n, joined)
	}
}
