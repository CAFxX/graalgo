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
// never be emitted as call targets. Red-green: fails if any of the 31
// tar stubs (or their panic message) appears in the exported wire.
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
}
