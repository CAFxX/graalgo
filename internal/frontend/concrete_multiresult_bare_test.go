package main

// P3-M8 (WS D): exporter pinning tests for bare-statement concrete
// multi-result calls (D2). Red-green: the bare-call tests fail on the
// pre-D2 tree (a bare w.Write/r.Read is the M8 scope error) and pass
// after.
//
//  1. TestConcreteBarePipeWriteDesugar pins the wire shape of a bare
//     w.Write(data) on *io.PipeWriter: an exprstmt wrapping a call to
//     __pipeWriterWrite with the receiver prepended, typed as the pair.
//  2. TestConcreteBarePipeReadDesugar pins the same for a bare
//     r.Read(buf) on *io.PipeReader (__pipeReaderRead).
//  3. TestConcreteBareM7StatementPositions pins the generalization to
//     every concrete helper-call plan: bare hex.DecodeString and bare
//     base64 (*Encoding).Decode desugar to their __-helpers (moved out
//     of TestEnccoreFailFast, where statement position was a pinned
//     fail-fast before D2).
//  4. TestConcreteBareXmlTokenDesugar pins bare dec.Token() → __tokenOf
//     (the mrXmlToken plan shares the concrete helper-call shape).
//  5. TestConcreteBareM5StillFailFast pins the kind-gate boundary: a
//     bare context.WithCancel keeps the pinned M5 statement-position
//     fail-fast (TestMultiResultFailFastKept, GG-0017) — the D2
//     interception covers only mrM7/mrXmlToken. Interface multi-result
//     calls in this position keep the D1 path (TestD1BareIfaceCall).

import (
	"strings"
	"testing"
)

// exportBareProg exports src and decodes the wire doc, failing the test
// on any export error.
func exportBareProg(t *testing.T, src string) map[string]any {
	t.Helper()
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	return decodeExport(t, out)
}

// bareHelperCall finds, in main's body, an exprstmt whose expression is a
// call to the named __-helper; it returns the call node's fields.
func bareHelperCall(t *testing.T, doc map[string]any, helper string) map[string]any {
	t.Helper()
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != "main" {
			continue
		}
		for _, n := range findAllKind(fm, "exprstmt") {
			x, _ := n["fields"].(map[string]any)["x"].(map[string]any)
			if x == nil || x["kind"] != "call" {
				continue
			}
			fun, _ := x["fields"].(map[string]any)["fun"].(map[string]any)
			if fun == nil || fun["kind"] != "ident" {
				continue
			}
			if fun["fields"].(map[string]any)["name"] == helper {
				return x["fields"].(map[string]any)
			}
		}
	}
	t.Fatalf("no bare-statement call to %s in main", helper)
	return nil
}

// callArgIdent returns the i-th call arg's ident name, failing unless it
// is a plain ident.
func callArgIdent(t *testing.T, call map[string]any, i int) string {
	t.Helper()
	args, _ := call["args"].([]any)
	if i >= len(args) {
		t.Fatalf("call has %d args, want index %d", len(args), i)
	}
	a, _ := args[i].(map[string]any)
	if a["kind"] != "ident" {
		t.Fatalf("arg %d is %q, want ident", i, a["kind"])
	}
	name, _ := a["fields"].(map[string]any)["name"].(string)
	return name
}

// callResultTypeName resolves the call node's "type" id to the type
// table entry's detail name.
func callResultTypeName(t *testing.T, doc map[string]any, call map[string]any) string {
	t.Helper()
	typeID, _ := call["type"].(float64)
	types := typeEntries(t, doc)
	te, ok := types[int(typeID)].(map[string]any)
	if !ok {
		t.Fatalf("call type id %v out of range", typeID)
	}
	detail, _ := te["detail"].(map[string]any)
	name, _ := detail["name"].(string)
	return name
}

func TestConcreteBarePipeWriteDesugar(t *testing.T) {
	const src = `package main

import "io"

func main() {
	r, w := io.Pipe()
	_ = r
	data := []byte("hi")
	w.Write(data)
}
`
	doc := exportBareProg(t, src)
	call := bareHelperCall(t, doc, "__pipeWriterWrite")
	// prependRecv: the receiver is the first helper argument.
	if got := callArgIdent(t, call, 0); got != "w" {
		t.Fatalf("arg 0 = %q, want receiver %q", got, "w")
	}
	if got := callResultTypeName(t, doc, call); got != "io.pipeWriterWritePair" {
		t.Fatalf("call result type = %q, want io.pipeWriterWritePair", got)
	}
}

func TestConcreteBarePipeReadDesugar(t *testing.T) {
	const src = `package main

import "io"

func main() {
	r, w := io.Pipe()
	_ = w
	buf := make([]byte, 8)
	r.Read(buf)
}
`
	doc := exportBareProg(t, src)
	call := bareHelperCall(t, doc, "__pipeReaderRead")
	if got := callArgIdent(t, call, 0); got != "r" {
		t.Fatalf("arg 0 = %q, want receiver %q", got, "r")
	}
	if got := callResultTypeName(t, doc, call); got != "io.pipeReaderReadPair" {
		t.Fatalf("call result type = %q, want io.pipeReaderReadPair", got)
	}
}

func TestConcreteBareM7StatementPositions(t *testing.T) {
	// Package-level function: no receiver prepended.
	doc := exportBareProg(t, `package main

import "encoding/hex"

func main() {
	hex.DecodeString("6162")
}
`)
	call := bareHelperCall(t, doc, "__hexDecodeString")
	args, _ := call["args"].([]any)
	if len(args) != 1 {
		t.Fatalf("__hexDecodeString call has %d args, want 1", len(args))
	}

	// Method on a named receiver type: receiver prepended.
	doc = exportBareProg(t, `package main

import "encoding/base64"

func main() {
	enc := base64.StdEncoding
	enc.Decode(make([]byte, 10), []byte("aGk="))
}
`)
	call = bareHelperCall(t, doc, "__base64Decode")
	if got := callArgIdent(t, call, 0); got != "enc" {
		t.Fatalf("arg 0 = %q, want receiver %q", got, "enc")
	}
}

func TestConcreteBareXmlTokenDesugar(t *testing.T) {
	// The synthetic xml.Reader is an M7 stand-in (Next() ReadResult),
	// so the test program defines its own reader.
	doc := exportBareProg(t, `package main

import "encoding/xml"

type byteReader struct {
	data []byte
	pos  int
}

func (b *byteReader) Next() xml.ReadResult {
	if b.pos >= len(b.data) {
		return xml.ReadResult{OK: false}
	}
	r := xml.ReadResult{Byte: b.data[b.pos], OK: true}
	b.pos++
	return r
}

func main() {
	br := &byteReader{data: []byte("<a/>")}
	dec := xml.NewDecoder(br)
	dec.Token()
}
`)
	call := bareHelperCall(t, doc, "__tokenOf")
	if got := callArgIdent(t, call, 0); got != "dec" {
		t.Fatalf("arg 0 = %q, want receiver %q", got, "dec")
	}
}

func TestConcreteBareM5StillFailFast(t *testing.T) {
	// The D2 interception is kind-gated (mrM7/mrXmlToken only): the M5
	// statement-position fail-fast is untouched.
	const src = `package main

import "context"

func main() {
	context.WithCancel(context.Background())
}
`
	_, err := ExportFile(writeIfaceProg(t, src))
	if err == nil {
		t.Fatal("expected M5 scope error for bare context.WithCancel, got success")
	}
	if !strings.Contains(err.Error(), "not in M5 scope") {
		t.Fatalf("error %q does not name the M5 scope", err.Error())
	}
}
