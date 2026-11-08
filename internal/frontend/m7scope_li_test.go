package main

import (
	"strings"
	"testing"
)

// L-I (REVIEW-2026-11-03-16 P1-R1): triage + conversion of the 179
// "not in M7 scope" fail-fast panics. The reviewer's grep count of 179
// overstates the true stub count: 165 are real panic sites and 14 are
// comments/error strings (see the lane triage list). Of the 165:
//   - 106 already had a desugar-table row (helper + pair struct);
//     this test pins that wiring so a dropped row is caught.
//   - 12 gained a row in this lane (csv 3, multipart 3, zip 5, math
//     Modf 1); their routing is pinned by TestM7ScopeNewRowsDesugar
//     (csv, zip, math: export-proven call path) and by
//     TestM7ScopeTableRowsWired (multipart: table-wiring only — the
//     export case is blocked by the path/filepath `lstat` export bug,
//     own lane review-1105-filepath-lstat; re-expand to an export
//     case when that lane lands).
//   - 16 gained real bodies (11 bytes, strings.Title, sort.Find,
//     xml.InputPos, json.RawMessage.UnmarshalJSON, json.HTMLEscape —
//     see TestSyntheticBytesConvertedMethods, TestM7ScopeStringsTitle,
//     TestM7ScopeSortFind, TestM7ScopeXmlInputPos,
//     TestM7ScopeJsonRawMessageHTMLEscape).
//   - the rest are genuinely blocked (reflect, streaming-design,
//     scanner ports, ports) and are listed with triggers in the lane
//     triage list, not converted. (Store-through-pointer was a blocker
//     class at lane start; the RawMessage conversion retired it — see
//     TestM7ScopeJsonRawMessageHTMLEscape.)

// m7ScopeLIPkgs are the synthetic packages the P1-R1 count covers
// (the 20 files of the review, with encoding/csv counted for the csv
// half of synth_enc_csv_gob.go).
var m7ScopeLIPkgs = map[string]bool{
	"archive/tar": true, "archive/zip": true,
	"encoding/ascii85": true, "encoding/asn1": true,
	"encoding/base32": true, "encoding/base64": true,
	"encoding/binary": true, "bytes": true, "encoding/csv": true,
	"encoding/hex": true, "encoding/json": true, "math": true,
	"mime": true, "mime/multipart": true,
	"mime/quotedprintable": true, "net/url": true,
	"encoding/pem": true, "sort": true, "strings": true,
	"encoding/xml": true,
}

// pairStructFields parses the field names, in order, of the pair struct
// named pair in src. It mirrors the layout every Appendix-M pair struct
// in the synthetic sources uses: one field per line, closing brace on
// its own (possibly indented) line.
func pairStructFields(src, pair string) ([]string, bool) {
	idx := strings.Index(src, "type "+pair+" struct {")
	if idx < 0 {
		return nil, false
	}
	rest := src[idx:]
	end := -1
	for _, closer := range []string{"\n}", "\n\t}"} {
		if i := strings.Index(rest, closer); i >= 0 && (end < 0 || i < end) {
			end = i
		}
	}
	if end < 0 {
		return nil, false
	}
	body := rest[:end]
	var fields []string
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "//") || strings.HasPrefix(ln, "type ") {
			continue
		}
		f := strings.Fields(ln)
		if len(f) == 0 {
			continue
		}
		fields = append(fields, strings.TrimSuffix(f[0], ","))
	}
	return fields, true
}

// TestM7ScopeTableRowsWired pins the desugar table's side of the
// P1-R1 bargain for every in-scope row: the named __-helper function
// and pair struct must exist in the package's synthetic source, and the
// row's field list must equal the struct's declared fields in order
// (synthPairField hard-errors on a mismatch, so a wrong row is an
// export failure, not a silent misroute). This is what caught the two
// stale single-result Close rows (deleted by this lane) and the nine
// wrong field lists on unexported helpers (corrected by this lane).
func TestM7ScopeTableRowsWired(t *testing.T) {
	for _, mf := range m7MultiResultFuncs {
		if !m7ScopeLIPkgs[mf.pkgPath] {
			continue
		}
		src, ok := syntheticSources[mf.pkgPath]
		if !ok {
			t.Errorf("row %s %s.%s: no synthetic source for package", mf.pkgPath, mf.recvType, mf.name)
			continue
		}
		if !strings.Contains(src, "func "+mf.helper+"(") {
			t.Errorf("row %s %s.%s: helper %s not found in synthetic source", mf.pkgPath, mf.recvType, mf.name, mf.helper)
		}
		fields, ok := pairStructFields(src, mf.pairType)
		if !ok {
			t.Errorf("row %s %s.%s: pair struct %s not found in synthetic source", mf.pkgPath, mf.recvType, mf.name, mf.pairType)
			continue
		}
		if len(fields) != len(mf.pairFields) {
			t.Errorf("row %s %s.%s: pair %s has %d fields %v, row lists %d %v",
				mf.pkgPath, mf.recvType, mf.name, mf.pairType, len(fields), fields, len(mf.pairFields), mf.pairFields)
			continue
		}
		for i, want := range mf.pairFields {
			if fields[i] != want {
				t.Errorf("row %s %s.%s: pair %s field %d is %q, row says %q",
					mf.pkgPath, mf.recvType, mf.name, mf.pairType, i, fields[i], want)
			}
		}
	}
}

// mainBodyIdents collects every ident name in the user main funcdecl's
// body, so a test can assert a call desugared to its __-helper (the
// helper ident appears in main's body) rather than merely exporting.
// NOTE (L-I): a wire funcdecl node carries its body under
// fields.body, NOT as a top-level "body" key — walking mainDecl["body"]
// silently yields nothing (this vacuous-walk bug made
// TestM7ScopeNewRowsDesugar report missing helpers for csv/math even
// though the desugar had worked: the dump shows __p5mr_N := __modf
// in main's body). Walk fields.body, falling back to a top-level body.
func mainBodyIdents(t *testing.T, doc map[string]any) map[string]bool {
	t.Helper()
	var mainDecl map[string]any
	for _, f := range findAllKind(doc, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		if fl["name"] == "main" {
			mainDecl = f
			break
		}
	}
	if mainDecl == nil {
		t.Fatal("wire doc missing funcdecl main")
	}
	body := mainDecl["body"]
	if body == nil {
		if fl, ok := mainDecl["fields"].(map[string]any); ok {
			body = fl["body"]
		}
	}
	if body == nil {
		t.Fatal("wire doc main funcdecl has no body")
	}
	found := map[string]bool{}
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if v["kind"] == "ident" {
				if name, _ := v["fields"].(map[string]any)["name"].(string); name != "" {
					found[name] = true
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
	walk(body)
	return found
}

// m7ScopeFuncBody returns a wire funcdecl node's body subtree, from
// fields.body (see mainBodyIdents' note) with a top-level fallback.
func m7ScopeFuncBody(f map[string]any) any {
	if b := f["body"]; b != nil {
		return b
	}
	if fl, ok := f["fields"].(map[string]any); ok {
		return fl["body"]
	}
	return nil
}

// TestM7ScopeNewRowsDesugar pins the 12 table rows this lane added:
// direct calls to the newly wired methods must export and must route
// to the __-helper in the user program's own body. Pre-lane, the csv
// calls pair-transformed the panic stub (the stub comments' desugar
// claim was false: no csv classifier existed) and the math calls
// likewise reached panic stubs, not helpers. The multipart rows are
// NOT export-proven here: any program importing mime/multipart also
// imports path/filepath, whose `var lstat = os.Lstat` export bug fails
// export with `variable "lstat" used before declaration` (own lane
// review-1105-filepath-lstat; see the wave3 multipart .scopelimited
// note). Their helper+pair wiring is pinned by
// TestM7ScopeTableRowsWired; re-add the export case below when the
// lstat lane lands.
func TestM7ScopeNewRowsDesugar(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		helpers []string
	}{
		{"csv", `package main

import (
	"encoding/csv"
	"strings"
)

func main() {
	r := csv.NewReader(strings.NewReader("a,b\nc,d\n"))
	rec, _ := r.Read()
	println(len(rec))
	recs, _ := r.ReadAll()
	println(len(recs))
	line, col := r.FieldPos(0)
	println(line, col)
}
`, []string{"__csvRead", "__csvReadAll", "__csvFieldPos"}},
		{"mathModf", `package main

import "math"

func main() {
	i, f := math.Modf(3.14)
	println(i, f)
}
`, []string{"__modf"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := decodeExport(t, mustExport(t, c.src))
			idents := mainBodyIdents(t, doc)
			for _, h := range c.helpers {
				if !idents[h] {
					t.Errorf("main body has no %s call (call did not desugar to the helper)", h)
				}
			}
		})
	}
}

// TestM7ScopeStringsTitle verifies the strings.Title conversion: its
// re-test trigger (the unicode wave) fired, so the gc body + isSeparator
// now live in the synthetic source and a direct call exports. The
// Title funcdecl's body must not contain the retired fail-fast panic.
func TestM7ScopeStringsTitle(t *testing.T) {
	src := `package main

import "strings"

func main() {
	var b strings.Builder
	_, _ = b.Write([]byte("x"))
	_, _ = b.WriteString("y")
	_, _ = b.WriteRune('z')
	println(b.String())
	println(strings.Title("hello world"))
}
`
	doc := decodeExport(t, mustExport(t, src))
	// Builder calls route to their helpers (table rows predate this
	// lane; the lane retired their stale "not in M7 scope" messages).
	idents := mainBodyIdents(t, doc)
	for _, h := range []string{"__builderWrite", "__builderWriteString", "__builderWriteRune"} {
		if !idents[h] {
			t.Errorf("main body has no %s call", h)
		}
	}
	// Title's own body must be real code, not the retired panic.
	var titleDecl map[string]any
	for _, f := range findAllKind(doc, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		if fl["name"] == "Title" && fl["pkgpath"] == "strings" {
			titleDecl = f
			break
		}
	}
	if titleDecl == nil {
		t.Fatal("wire doc missing funcdecl strings.Title")
	}
	// A panic call's message string survives in the wire as a literal,
	// so scan the whole Title subtree for the retired message text
	// (panic is a builtin call, not a distinct wire kind).
	sawPanic := false
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		case string:
			if strings.Contains(v, "not in M7 scope") {
				sawPanic = true
			}
		}
	}
	walk(m7ScopeFuncBody(titleDecl))
	if sawPanic {
		t.Error("strings.Title body still contains the retired fail-fast message")
	}
}

// TestM7ScopeSortFind verifies the sort.Find conversion: the real gc
// binary-search body is pair-transformed by the general path, so a
// direct call with a func-literal comparator exports and Find appears
// in pair form (exactly 1 result), never as a raw multi-result funcdecl.
func TestM7ScopeSortFind(t *testing.T) {
	src := `package main

import "sort"

func main() {
	i, found := sort.Find(5, func(i int) int { return i - 3 })
	println(i, found)
}
`
	doc := decodeExport(t, mustExport(t, src))
	for _, f := range findAllKind(doc, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		if fl["name"] == "Find" && fl["pkgpath"] == "sort" {
			nr := 0
			if rs, ok := fl["results"].([]any); ok {
				nr = len(rs)
			}
			if nr != 1 {
				t.Fatalf("sort.Find funcdecl has %d results, want pair form (1)", nr)
			}
			return
		}
	}
	t.Fatal("wire doc missing funcdecl sort.Find")
}

// TestM7ScopeXmlInputPos verifies the xml.Decoder.InputPos conversion:
// its gc body makes no multi-result call, so the general pair-transform
// serves it; a direct call exports and InputPos appears in pair form.
func TestM7ScopeXmlInputPos(t *testing.T) {
	src := `package main

import (
	"encoding/xml"
	"io"
)

// reader is the dual-mode xml.Reader documented in synth_xml.go: a
// program-defined type with Next() xml.ReadResult (what graalgo's
// synthetic NewDecoder accepts) plus Read (what gc's NewDecoder
// accepts). strings.Reader alone does NOT implement xml.Reader (no
// Next method) — passing it to NewDecoder fails type-check, which is
// what the first version of this test did.
type reader struct {
	s string
	i int
}

func (r *reader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

func (r *reader) Next() xml.ReadResult {
	if r.i >= len(r.s) {
		return xml.ReadResult{}
	}
	b := r.s[r.i]
	r.i++
	return xml.ReadResult{Byte: b, OK: true}
}

func main() {
	d := xml.NewDecoder(&reader{s: "<a/>"})
	line, col := d.InputPos()
	println(line, col)
}
`
	doc := decodeExport(t, mustExport(t, src))
	for _, f := range findAllKind(doc, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		if fl["name"] == "InputPos" && fl["pkgpath"] == "encoding/xml" {
			nr := 0
			if rs, ok := fl["results"].([]any); ok {
				nr = len(rs)
			}
			if nr != 1 {
				t.Fatalf("xml InputPos funcdecl has %d results, want pair form (1)", nr)
			}
			return
		}
	}
	t.Fatal("wire doc missing funcdecl encoding/xml.InputPos")
}

// TestM7ScopeStringsReader verifies the strings.Reader M8-label
// extension of this lane: Read/ReadAt/ReadByte/ReadRune/Seek were
// already table-driven (rows + helpers predate this lane), so direct
// calls must route to the helpers; the lane retired their stale
// "not in M8 scope" messages to redirect form. WriteTo and
// Replacer.WriteString stay M8-blocked (2-result interface dispatch).
func TestM7ScopeStringsReader(t *testing.T) {
	src := `package main

import (
	"io"
	"strings"
)

func main() {
	r := strings.NewReader("hello")
	buf := make([]byte, 3)
	n, _ := r.Read(buf)
	println(n)
	m, _ := r.ReadAt(buf, 1)
	println(m)
	c, _ := r.ReadByte()
	println(c)
	ch, sz, _ := r.ReadRune()
	println(ch, sz)
	pos, _ := r.Seek(0, io.SeekStart)
	println(pos)
}
`
	doc := decodeExport(t, mustExport(t, src))
	idents := mainBodyIdents(t, doc)
	for _, h := range []string{"__stringsReaderRead", "__stringsReaderReadAt", "__stringsReaderReadByte", "__stringsReaderReadRune", "__stringsReaderSeek"} {
		if !idents[h] {
			t.Errorf("main body has no %s call", h)
		}
	}
}

// TestM7ScopeJsonRawMessageHTMLEscape verifies the json conversions
// whose triggers the L-I probes confirmed: store-through-pointer
// (RawMessage.UnmarshalJSON) and variable address-of (HTMLEscape's
// &buf call). Both programs must export, and the converted funcdecls
// must not contain the retired fail-fast message.
func TestM7ScopeJsonRawMessageHTMLEscape(t *testing.T) {
	cases := []struct {
		name string
		src  string
		decl string
	}{
		{"RawMessage", `package main

import "encoding/json"

func main() {
	var m json.RawMessage
	_ = m.UnmarshalJSON([]byte("{}"))
	println(len(m))
}
`, "UnmarshalJSON"},
		{"HTMLEscape", `package main

import (
	"bytes"
	"encoding/json"
)

func main() {
	var buf bytes.Buffer
	json.HTMLEscape(&buf, []byte("<a>"))
	println(buf.Len())
}
`, "HTMLEscape"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := decodeExport(t, mustExport(t, c.src))
			var decl map[string]any
			for _, f := range findAllKind(doc, "funcdecl") {
				fl, _ := f["fields"].(map[string]any)
				if fl["name"] == c.decl && fl["pkgpath"] == "encoding/json" {
					decl = f
					break
				}
			}
			if decl == nil {
				t.Fatalf("wire doc missing funcdecl encoding/json.%s", c.decl)
			}
			sawStale := false
			var walk func(n any)
			walk = func(n any) {
				switch v := n.(type) {
				case map[string]any:
					for _, x := range v {
						walk(x)
					}
				case []any:
					for _, x := range v {
						walk(x)
					}
				case string:
					if strings.Contains(v, "not in M7 scope") {
						sawStale = true
					}
				}
			}
			walk(m7ScopeFuncBody(decl))
			if sawStale {
				t.Errorf("encoding/json.%s body still contains the retired fail-fast message", c.decl)
			}
		})
	}
}

// TestM7ScopeBytesWriteString verifies the bytes.Buffer.WriteString
// redirect-message retirement: the call routes to __bytesBufferWriteString
// (table row predates this lane; only its stale message is retired here).
func TestM7ScopeBytesWriteString(t *testing.T) {
	src := `package main

import "bytes"

func main() {
	b := bytes.NewBufferString("a")
	n, _ := b.WriteString("bc")
	println(n, b.String())
}
`
	doc := decodeExport(t, mustExport(t, src))
	if !mainBodyIdents(t, doc)["__bytesBufferWriteString"] {
		t.Error("main body has no __bytesBufferWriteString call")
	}
}
