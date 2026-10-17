package main

import (
	"regexp"
	"strings"
	"testing"
)

// tarStubMsg matches the fail-fast panic strings of the 31 table-driven
// archive/tar stubs ("archive/tar.<name>: not in M7 scope ..."). The
// "archive/tar." prefix alone is not discriminating: it also appears in
// qualified type/method names in the wire type table (e.g. the implkey
// "archive/tar.allowedFormats\x00func()...").
var tarStubMsg = regexp.MustCompile(`archive/tar\.[A-Za-z]+: not in M7 scope`)

// TestTarDesugarNoPanicStubs (RV-P0-02): a program exercising the core
// archive/tar read/write API must export without any of the
// "not in M7 scope" fail-fast panic bodies leaking into the wire.
// The Appendix-M desugar (m7MultiResultFuncs archive/tar entries)
// redirects every multi-result call site to the pair-returning
// __-helpers; the panic stubs exist only for type-checking and must
// never be emitted as call targets.
//
// Wire-shape regression check (red-green): the synthetic
// __tarFileInfoHeader helper assigns the unlisted interface methods
// FileInfoNames.Gname/Uname in 2-target position
// (h.Gname, err = iface.Gname()). Those call sites classify only via
// the classifyUserIfaceMultiResultCall fallback in mrdesugar_m5.go;
// without it the wire carries raw 2-lhs/1-rhs assign nodes that the
// backend rejects ("assign.fields: lhs/rhs length mismatch"). The test
// asserts the desugared shape — a __p5mr_N pair temp holding the
// ifacemethod call plus single-target f0/f1 field extractions — and
// the absence of the raw mismatched-arity assign shape. Pre-fix the
// shape assertions fail; post-fix they pass.
func TestTarDesugarNoPanicStubs(t *testing.T) {
	const src = `package main

import (
	"archive/tar"
	"bytes"
	"io"
	"io/fs"
	"time"
)

type dummyFileInfo struct{}

func (dummyFileInfo) Name() string       { return "d.txt" }
func (dummyFileInfo) Size() int64        { return 0 }
func (dummyFileInfo) Mode() fs.FileMode { return 0 }
func (dummyFileInfo) ModTime() time.Time { return time.Time{} }
func (dummyFileInfo) IsDir() bool       { return false }
func (dummyFileInfo) Sys() any          { return nil }

func main() {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "a.txt", Mode: 0o644, Size: 3}); err != nil {
		panic(err)
	}
	if _, err := tw.Write([]byte("abc")); err != nil {
		panic(err)
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	tr := tar.NewReader(&buf)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		_ = hdr.Name
		body, err := io.ReadAll(tr)
		if err != nil {
			panic(err)
		}
		_ = body
	}
	fi, err := tar.FileInfoHeader(dummyFileInfo{}, "")
	if err != nil {
		panic(err)
	}
	_ = fi.Name
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile(tar round-trip): %v", err)
	}
	// No archive/tar fail-fast panic stub may be emitted. (Other
	// packages' non-table-driven stubs, e.g. bytes.Buffer.Peek, are
	// honestly emitted as fail-fast bodies by design — they panic only
	// if called. The tar stubs are table-driven: hasStaticMREntry skips
	// them at synthetic import, so their panic strings must be absent
	// from the wire.)
	if m := tarStubMsg.FindString(out); m != "" {
		t.Errorf("exported wire contains an archive/tar fail-fast panic stub: %s", m)
	}
	// The Appendix-M __-helpers must be the actual call targets.
	for _, want := range []string{
		"__tarReaderNext", "__tarReaderRead", "__tarWriterWrite",
		"__tarFileInfoHeader",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exported wire missing helper call target %s", want)
		}
	}

	// Wire-shape check: the Gname/Uname 2-target assignments inside the
	// synthetic __tarFileInfoHeader must be desugared.
	doc := decodeExport(t, out)
	helper := tarFileInfoHeaderFunc(t, doc)

	// (a) No assign in the helper may have mismatched lhs/rhs arity.
	// The raw 2-lhs/1-rhs shape is what the backend rejects; the
	// desugar always produces single-target assigns.
	for _, n := range findAllKind(helper, "assign") {
		f, _ := n["fields"].(map[string]any)
		lhs, _ := f["lhs"].([]any)
		rhs, _ := f["rhs"].([]any)
		if len(lhs) != len(rhs) {
			t.Errorf("__tarFileInfoHeader: assign with %d lhs / %d rhs targets (backend-invalid raw multi-assign)",
				len(lhs), len(rhs))
		}
	}

	// (b) Each unlisted interface multi-result call site desugars to a
	// __p5mr_N pair temp holding the ifacemethod call, plus
	// single-target extractions h.<method> = temp.f0 and err = temp.f1.
	for _, method := range []string{"Gname", "Uname"} {
		temp := tarPairTemp(t, helper, method)
		if temp == "" {
			continue
		}
		var sawF0, sawF1 bool
		for _, n := range findAllKind(helper, "assign") {
			f, _ := n["fields"].(map[string]any)
			lhs, _ := f["lhs"].([]any)
			rhs, _ := f["rhs"].([]any)
			if len(lhs) != 1 || len(rhs) != 1 {
				continue
			}
			r, _ := rhs[0].(map[string]any)
			if r["kind"] != "selector" {
				continue
			}
			rf, _ := r["fields"].(map[string]any)
			x, _ := rf["x"].(map[string]any)
			xf, _ := x["fields"].(map[string]any)
			if x["kind"] != "ident" || xf["name"] != temp {
				continue
			}
			l, _ := lhs[0].(map[string]any)
			lf, _ := l["fields"].(map[string]any)
			switch rf["field"] {
			case "f0":
				// h.Gname = temp.f0 (resp. h.Uname).
				xl, _ := lf["x"].(map[string]any)
				xlf, _ := xl["fields"].(map[string]any)
				if l["kind"] == "selector" && lf["field"] == method &&
					xl["kind"] == "ident" && xlf["name"] == "h" {
					sawF0 = true
				}
			case "f1":
				// err = temp.f1.
				if l["kind"] == "ident" && lf["name"] == "err" {
					sawF1 = true
				}
			}
		}
		if !sawF0 {
			t.Errorf("__tarFileInfoHeader: no single-target extraction h.%s = %s.f0", method, temp)
		}
		if !sawF1 {
			t.Errorf("__tarFileInfoHeader: no single-target extraction err = %s.f1", temp)
		}
	}
}

// tarFileInfoHeaderFunc returns the synthetic archive/tar
// __tarFileInfoHeader funcdecl wire node.
func tarFileInfoHeaderFunc(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	funcs, _ := doc["funcs"].([]any)
	for _, f := range funcs {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] == "__tarFileInfoHeader" && fl["pkgpath"] == "archive/tar" {
			return fm
		}
	}
	t.Fatal("no __tarFileInfoHeader funcdecl (archive/tar) in exported wire")
	return nil
}

// tarPairTemp finds the __p5mr_N pair temp vardecl in the helper whose
// single value is the ifacemethod call for the named method, and
// returns the temp name. It reports a test failure and returns "" when
// the desugared shape is absent (the pre-fix wire has no such temp).
func tarPairTemp(t *testing.T, helper map[string]any, method string) string {
	t.Helper()
	for _, n := range findAllKind(helper, "vardecl") {
		f, _ := n["fields"].(map[string]any)
		names, _ := f["names"].([]any)
		vals, _ := f["values"].([]any)
		if len(names) != 1 || len(vals) != 1 {
			continue
		}
		name, _ := names[0].(string)
		if !strings.HasPrefix(name, "__p5mr_") {
			continue
		}
		v, _ := vals[0].(map[string]any)
		vf, _ := v["fields"].(map[string]any)
		if v["kind"] != "ifacemethod" || vf["method"] != method {
			continue
		}
		return name
	}
	t.Errorf("__tarFileInfoHeader: no __p5mr_N pair temp holding the %s ifacemethod call (desugar missing)", method)
	return ""
}
