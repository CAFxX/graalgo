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
	// cmp.Or was added in go1.22 and cmp.Compare in go1.21; at go1.21
	// only Or must be flagged.
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
	// Wire the cmp fixture into a real Run call: Or (go1.22) must be
	// flagged at go1.21, Compare (go1.21) must not.
	joined = runVetOnModule(t, "module example.com/p\n\ngo 1.21\n",
		map[string]string{"p.go": src})
	if !strings.Contains(joined, "cmp.Or requires go1.22 or later (module is go1.21)") {
		t.Errorf("expected cmp.Or diagnostic, got:\n%s", joined)
	}
	if strings.Contains(joined, "cmp.Compare requires") {
		t.Errorf("false positive on go1.21 symbol cmp.Compare:\n%s", joined)
	}
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

func TestParseAPIKeys(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		// Struct field: the key is the bare field name, not "name type".
		{"pkg net, type Dialer struct, KeepAliveConfig KeepAliveConfig #62254",
			[]string{"net.Dialer", "net.Dialer.KeepAliveConfig"}},
		{"pkg net/http, type Request struct, Pattern string #66405",
			[]string{"net/http.Request", "net/http.Request.Pattern"}},
		// Embedded field: the key is the embedded type's name.
		{"pkg archive/zip, type File struct, embedded FileHeader",
			[]string{"archive/zip.File", "archive/zip.File.FileHeader"}},
		// Bare-name field (old api form) keeps working.
		{"pkg crypto/tls, type ConnectionState struct, TLSUnique //deprecated",
			[]string{"crypto/tls.ConnectionState", "crypto/tls.ConnectionState.TLSUnique"}},
		// Interface method: the key is the bare method name.
		{"pkg io, type Reader interface, Read([]byte) (int, error)",
			[]string{"io.Reader", "(io.Reader).Read"}},
		{"pkg reflect, type Type interface, CanSeq() bool #66056",
			[]string{"reflect.Type", "(reflect.Type).CanSeq"}},
		// Const: "const " is 6 chars, not 4; every const key used to be "pkg.t".
		{"pkg crypto/tls, const QUICEncryptionLevelApplication = 3 #44886",
			[]string{"crypto/tls.QUICEncryptionLevelApplication"}},
		{"pkg crypto/tls, const QUICEncryptionLevelApplication QUICEncryptionLevel #44886",
			[]string{"crypto/tls.QUICEncryptionLevelApplication"}},
		// Var: unchanged behaviour.
		{"pkg foo, var V int",
			[]string{"foo.V"}},
		// Func and method keys were already correct; pin them.
		{"pkg slices, func Concat[S ~[]E, E any](slices ...S) S",
			[]string{"slices.Concat"}},
		{"pkg net/http, method (*Request) PathValue(string) string",
			[]string{"(*net/http.Request).PathValue"}},
	}
	for _, tc := range cases {
		got := parseAPIKeys(tc.line + "\n")
		if len(got) != len(tc.want) {
			t.Errorf("parseAPIKeys(%q) = %q, want %q", tc.line, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseAPIKeys(%q) = %q, want %q", tc.line, got, tc.want)
				break
			}
		}
	}
}

func TestStdVersionStructField(t *testing.T) {
	// net.Dialer.KeepAliveConfig and http.Request.Pattern were added in
	// go1.23. The DB keys for struct fields must be the bare field name:
	// parseAPIKeys used to keep the field's type in the key
	// (net.Dialer.KeepAliveConfig KeepAliveConfig), which no lookup key
	// could ever match, so no struct-field check could fire.
	src := `package p

import (
	"net"
	"net/http"
)

var d net.Dialer
var _ = d.KeepAliveConfig
var _ = d.Timeout

var r http.Request
var _ = r.Pattern
var _ = r.Method
`
	joined := runVetOnModule(t, "module example.com/p\n\ngo 1.21\n",
		map[string]string{"p.go": src})
	if !strings.Contains(joined, "net.Dialer.KeepAliveConfig requires go1.23 or later (module is go1.21)") {
		t.Errorf("expected KeepAliveConfig diagnostic, got:\n%s", joined)
	}
	if !strings.Contains(joined, "http.Request.Pattern requires go1.23 or later (module is go1.21)") {
		t.Errorf("expected Pattern diagnostic, got:\n%s", joined)
	}
	if strings.Contains(joined, "Timeout requires") || strings.Contains(joined, "Method requires") {
		t.Errorf("false positive on old field:\n%s", joined)
	}
	// Module at go1.23: no diagnostic.
	joined = runVetOnModule(t, "module example.com/p\n\ngo 1.23\n",
		map[string]string{"p.go": src})
	if strings.Contains(joined, "requires go1.23") {
		t.Errorf("false positive at go1.23:\n%s", joined)
	}
}

func TestStdVersionPromotedField(t *testing.T) {
	// A field promoted through an embedded stdlib struct is declared on
	// the embedded struct, so the DB key must use the declaring struct,
	// not the receiver type: md.KeepAliveConfig is net.Dialer.KeepAliveConfig,
	// not fixture.MyDialer.KeepAliveConfig.
	src := `package p

import (
	"net"
	"net/http"
)

type MyDialer struct{ net.Dialer }
type MyPDialer struct{ *net.Dialer }
type MyReq struct{ http.Request }

var md MyDialer
var _ = md.KeepAliveConfig
var _ = md.Timeout

var mpd MyPDialer
var _ = mpd.KeepAliveConfig

var mr MyReq
var _ = mr.Pattern
var _ = mr.Method
`
	joined := runVetOnModule(t, "module example.com/p\n\ngo 1.21\n",
		map[string]string{"p.go": src})
	if n := strings.Count(joined, "KeepAliveConfig requires go1.23 or later (module is go1.21)"); n != 2 {
		t.Errorf("expected 2 promoted KeepAliveConfig diagnostics (value + pointer embedding), got %d:\n%s", n, joined)
	}
	if !strings.Contains(joined, "Pattern requires go1.23 or later (module is go1.21)") {
		t.Errorf("expected promoted Pattern diagnostic, got:\n%s", joined)
	}
	if strings.Contains(joined, "Timeout requires") || strings.Contains(joined, "Method requires") {
		t.Errorf("false positive on old promoted field:\n%s", joined)
	}
}

func TestStdVersionConst(t *testing.T) {
	// crypto/tls.QUICResumeSession was added in go1.23. (The
	// crypto/tls.QUICEncryptionLevelApplication const from the review is
	// go1.21, so no module version can flag it end to end — the analyzer
	// deliberately skips modules below go1.21 — and it is pinned at the
	// parseAPIKeys level in TestParseAPIKeys instead.)
	src := `package p

import "crypto/tls"

var _ = tls.QUICResumeSession
var _ = tls.VersionTLS12
`
	joined := runVetOnModule(t, "module example.com/p\n\ngo 1.21\n",
		map[string]string{"p.go": src})
	if !strings.Contains(joined, "tls.QUICResumeSession requires go1.23 or later (module is go1.21)") {
		t.Errorf("expected QUICResumeSession diagnostic, got:\n%s", joined)
	}
	if strings.Contains(joined, "VersionTLS12 requires") {
		t.Errorf("false positive on old const:\n%s", joined)
	}
	// Module at go1.23: no diagnostic.
	joined = runVetOnModule(t, "module example.com/p\n\ngo 1.23\n",
		map[string]string{"p.go": src})
	if strings.Contains(joined, "requires go1.23") {
		t.Errorf("false positive at go1.23:\n%s", joined)
	}
}

func TestStdVersionInterfaceMethod(t *testing.T) {
	// reflect.Type.CanSeq was added in go1.23 on the long-existing
	// reflect.Type interface. The DB key is the bare method name:
	// (reflect.Type).CanSeq, not (reflect.Type).CanSeq() bool.
	src := `package p

import "reflect"

func f(t reflect.Type) bool { return t.CanSeq() }
func g(t reflect.Type) bool { return t.Comparable() }
`
	joined := runVetOnModule(t, "module example.com/p\n\ngo 1.21\n",
		map[string]string{"p.go": src})
	if !strings.Contains(joined, "reflect.(Type).CanSeq requires go1.23 or later (module is go1.21)") {
		t.Errorf("expected CanSeq diagnostic, got:\n%s", joined)
	}
	if strings.Contains(joined, "Comparable requires") {
		t.Errorf("false positive on old interface method:\n%s", joined)
	}
	// Module at go1.23: no diagnostic.
	joined = runVetOnModule(t, "module example.com/p\n\ngo 1.23\n",
		map[string]string{"p.go": src})
	if strings.Contains(joined, "requires go1.23") {
		t.Errorf("false positive at go1.23:\n%s", joined)
	}
}
