package main

import (
	"testing"
)

// TestM7NetHttpDesugarWiring pins the 17 P3-M10 m7MultiResultFuncs entries
// added for net/http and net/http/httptest. The M9 net/http drop omitted
// them, so any program importing net/http failed with the WS D
// multi-result scope error at the first client call. For each entry it
// verifies, against the synthetic sources in synth_net_http.go /
// synth_net_http_httptest.go:
//
//  1. exactly one table entry exists for (pkgPath, name, recvType), with
//     the expected helper name, pair type, and pair fields in order;
//  2. the multi-result function (or method on the named receiver type)
//     exists in the synthetic package scope;
//  3. the __-helper exists in the synthetic package scope and is a
//     *types.Func;
//  4. the helper's parameters accept the multi-result call's arguments
//     positionally (the desugar passes the original call's args straight
//     through, receiver prepended for methods);
//  5. the pair struct exists with exactly the listed fields in order,
//     each field's type identical to the multi-result signature's
//     corresponding result type.
//
// This test fails on the pre-M10 tree (no table entries for these
// functions) and fails on any wiring typo (wrong helper name, wrong pair
// type, wrong field name/order, arity mismatch).
var m7NetHttpTable = []m7StrconvJsonExpect{
	// net/http: package-level funcs.
	{"net/http", "NewRequest", "", "__newRequest", "__newRequestPair", []string{"req", "err"}},
	{"net/http", "ReadRequest", "", "__readRequest", "__readRequestPair", []string{"req", "err"}},
	{"net/http", "readLine", "", "__readLine", "__readLinePair", []string{"line", "err"}},
	{"net/http", "readMIMEHeader", "", "__readMIMEHeader", "__readMIMEHeaderPair", []string{"hdr", "err"}},
	{"net/http", "ReadResponse", "", "__readResponse", "__readResponsePair", []string{"resp", "err"}},
	{"net/http", "Get", "", "__httpGet", "__clientDoPair", []string{"resp", "err"}},
	{"net/http", "Head", "", "__httpHead", "__clientDoPair", []string{"resp", "err"}},
	{"net/http", "Post", "", "__httpPost", "__clientDoPair", []string{"resp", "err"}},
	{"net/http", "PostForm", "", "__httpPostForm", "__clientDoPair", []string{"resp", "err"}},
	// net/http: methods (recvType names the receiver's named type).
	{"net/http", "Do", "Client", "__clientDo", "__clientDoPair", []string{"resp", "err"}},
	{"net/http", "Handler", "ServeMux", "__muxHandler", "__muxHandlerPair", []string{"h", "pattern"}},
	{"net/http", "RoundTrip", "Transport", "__transportRoundTrip", "__clientDoPair", []string{"resp", "err"}},
	{"net/http", "Read", "chunkedReader", "__chunkedRead", "__chunkedReadPair", []string{"n", "err"}},
	{"net/http", "Read", "limitedReadCloser", "__limitedReadCloserRead", "__limitedReadCloserReadPair", []string{"n", "err"}},
	{"net/http", "Write", "responseWriter", "__responseWriterWrite", "__responseWriterWritePair", []string{"n", "err"}},
	{"net/http", "Hijack", "responseWriter", "__rwHijack", "__rwHijackPair", []string{"c", "rw", "err"}},
	// net/http/httptest: methods.
	{"net/http/httptest", "Write", "ResponseRecorder", "__recorderWrite", "__recorderWritePair", []string{"n", "err"}},
}

func TestM7NetHttpDesugarWiring(t *testing.T) {
	if len(m7NetHttpTable) != 17 {
		t.Fatalf("m7NetHttpTable has %d entries, want 17", len(m7NetHttpTable))
	}
	for _, want := range m7NetHttpTable {
		name := want.pkgPath + "." + want.name
		if want.recvType != "" {
			name = want.pkgPath + "." + want.recvType + "." + want.name
		}
		t.Run(name, func(t *testing.T) {
			checkM7DesugarWiring(t, want)
		})
	}
}
