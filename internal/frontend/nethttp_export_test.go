package main

// Export tests for real (disk) net/http (F1, REVIEW-2026-11-06-15).
//
// net/http retired from synthetic to the disk importer (cc0a78f4), but
// 17 stale m7 rows kept naming its deleted __-helpers, so
// classifyM7MultiResultCall called getSyntheticPackage("net/http") and
// panicked on any program calling http.Get / http.NewRequest /
// Client.Do — before the disk table was ever consulted. These tests
// pin the disk ownership the row deletion relies on, and the export
// behavior the deletion restores.

import (
	"strings"
	"testing"
)

// TestNetHTTPDiskOwned pins that net/http + net/http/httptest are
// disk-owned with generated multi-result helpers: the premise the F1
// row deletion relies on. The disk transform generates one __-helper
// + pair per multi-result func (transformMRFunc: __ + name, or
// recvType + "_" + name for methods) and one interface spec per
// (interface, method) with pair __<Iface>_<Method>Pair and f1..fN
// fields. If the disk transform ever stops owning one of these, this
// test fails before any export test can go green for the wrong reason.
func TestNetHTTPDiskOwned(t *testing.T) {
	for _, p := range []string{"net/http", "net/http/httptest"} {
		if _, ok := syntheticSources[p]; ok {
			t.Fatalf("package %q is still synthetic; the F1 deletion assumes it is disk-owned", p)
		}
		if !isDiskImportable(p) {
			t.Fatalf("package %q is not disk-importable; the F1 deletion assumes the disk importer owns it", p)
		}
	}
	sp, err := getDiskPackage("net/http")
	if err != nil {
		t.Fatalf("getDiskPackage(\"net/http\"): %v", err)
	}
	type wantSpec struct {
		name, recv, helper, pair string
		fields                   []string
	}
	for _, w := range []wantSpec{
		{"Get", "", "__Get", "__GetPair", []string{"f1", "f2"}},
		{"Head", "", "__Head", "__HeadPair", []string{"f1", "f2"}},
		{"Post", "", "__Post", "__PostPair", []string{"f1", "f2"}},
		{"NewRequest", "", "__NewRequest", "__NewRequestPair", []string{"f1", "f2"}},
		{"Do", "Client", "__Client_Do", "__Client_DoPair", []string{"f1", "f2"}},
		{"RoundTrip", "Transport", "__Transport_RoundTrip", "__Transport_RoundTripPair", []string{"f1", "f2"}},
		// Interface spec: no helper; the itab dispatches to each
		// concrete type's __-helper at runtime.
		{"RoundTrip", "RoundTripper", "", "__RoundTripper_RoundTripPair", []string{"f1", "f2"}},
	} {
		spec := lookupDiskMR("net/http", w.name, w.recv)
		if spec == nil {
			t.Fatalf("no disk MR spec for net/http.%s (recv %q)", w.name, w.recv)
		}
		if spec.helper != w.helper {
			t.Fatalf("disk spec helper for net/http.%s (recv %q) = %q, want %q", w.name, w.recv, spec.helper, w.helper)
		}
		if spec.pairType != w.pair {
			t.Fatalf("disk spec pair type for net/http.%s (recv %q) = %q, want %q", w.name, w.recv, spec.pairType, w.pair)
		}
		if len(spec.pairFields) != len(w.fields) {
			t.Fatalf("disk spec pair fields for net/http.%s (recv %q) = %v, want %v", w.name, w.recv, spec.pairFields, w.fields)
		}
		for i, f := range w.fields {
			if spec.pairFields[i] != f {
				t.Fatalf("disk spec pair fields for net/http.%s (recv %q) = %v, want %v", w.name, w.recv, spec.pairFields, w.fields)
			}
		}
		if w.helper != "" {
			if hobj := sp.pkg.Scope().Lookup(w.helper); hobj == nil {
				t.Fatalf("disk helper net/http.%s missing from package scope", w.helper)
			}
		}
		if pobj := sp.pkg.Scope().Lookup(w.pair); pobj == nil {
			t.Fatalf("disk pair type net/http.%s missing from package scope", w.pair)
		}
	}
	// httptest: ResponseRecorder.Write is a concrete disk MR method.
	htsp, err := getDiskPackage("net/http/httptest")
	if err != nil {
		t.Fatalf("getDiskPackage(\"net/http/httptest\"): %v", err)
	}
	spec := lookupDiskMR("net/http/httptest", "Write", "ResponseRecorder")
	if spec == nil {
		t.Fatal("no disk MR spec for net/http/httptest.ResponseRecorder.Write")
	}
	if spec.helper != "__ResponseRecorder_Write" || spec.pairType != "__ResponseRecorder_WritePair" {
		t.Fatalf("disk spec for httptest ResponseRecorder.Write = helper %q pair %q, want __ResponseRecorder_Write / __ResponseRecorder_WritePair", spec.helper, spec.pairType)
	}
	if hobj := htsp.pkg.Scope().Lookup("__ResponseRecorder_Write"); hobj == nil {
		t.Fatal("disk helper net/http/httptest.__ResponseRecorder_Write missing from package scope")
	}
}

const netHTTPClientSrc = `package main

import (
	"net/http"
	"net/http/httptest"
)

func main() {
	req, _ := http.NewRequest("GET", "http://example.com/", nil)
	resp, err := http.Get("http://example.com/")
	_, _ = resp, err
	var cl http.Client
	resp2, err2 := cl.Do(req)
	_, _ = resp2, err2
	var rt http.RoundTripper = http.DefaultTransport
	_, _ = rt.RoundTrip(req)
	rec := httptest.NewRecorder()
	_, _ = rec.Write([]byte("x"))
	_ = req
}
`

// TestNetHTTPClientExport: the F1 export test. At the F1 base this
// panics in the gofrontend ("no synthetic source for net/http") at the
// first classified call; after the row deletion the program exports
// via the disk desugar. The output must name the disk-generated
// helpers, proving the calls desugared through the disk table rather
// than being silently skipped.
func TestNetHTTPClientExport(t *testing.T) {
	out, err := ExportFile(writeSyncProg(t, netHTTPClientSrc))
	if err != nil {
		t.Fatalf("ExportFile with net/http client calls: %v", err)
	}
	for _, want := range []string{"__Get", "__NewRequest", "__Client_Do"} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing disk helper %q (calls did not desugar through the disk table)", want)
		}
	}
}

// TestWave3NetHTTPProgramsExport: the two wave-3 corpus programs F1
// names must export without panic (their .scopelimited markers predate
// real net/http and describe the retired synthetic world; export here
// is the F1 verification, not a scope claim about running them).
func TestWave3NetHTTPProgramsExport(t *testing.T) {
	for _, prog := range []string{
		"../../test/corpus/wave3/net/http/00_pipe_roundtrip.go",
		"../../test/corpus/wave3/net/http/03_loopback_client_server.go",
	} {
		prog := prog
		t.Run(prog, func(t *testing.T) {
			out, err := ExportFile(prog)
			if err != nil {
				t.Fatalf("ExportFile(%s): %v", prog, err)
			}
			if len(out) == 0 {
				t.Fatalf("ExportFile(%s) returned empty output", prog)
			}
		})
	}
}
