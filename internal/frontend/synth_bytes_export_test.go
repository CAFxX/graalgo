package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSyntheticBytesM7 verifies the P3-M7 wave-1 synthetic bytes package:
// a program importing "bytes" exports cleanly; the real-Go declarations
// become funcdecls in the wire doc; the multi-result members (Cut,
// Buffer.Read, Reader.Seek, ...) are never emitted as raw multi-result
// funcdecls. Members covered by a desugar table (Buffer.Read,
// Reader.Read, Buffer.Write) are skipped (their __-helper carries the
// body); the rest are pair-transformed via the general userMRFuncs
// mechanism (Cut, CutPrefix, CutSuffix, CutLast first, then the
// remaining members). The converted Buffer/Reader members are covered
// by TestSyntheticBytesConvertedMethods; only the M8-scope
// ReadFrom/WriteTo members still fail fast (see their stubs).
//
// The absence check is scoped to pkgpath "bytes" AND to result count:
// a bare-name walk false-positives on transitive imports (io.Read,
// internal/race.Read, ...) that share the member names, and pair
// transformation legitimately leaves single-result (pair) funcdecls
// behind under the same names.
func TestSyntheticBytesM7(t *testing.T) {
	src := `package main

import "bytes"

func main() {
	println(bytes.Contains([]byte("hello"), []byte("ell")))
	println(bytes.Count([]byte("aaa"), []byte("a")))
	println(string(bytes.ToUpper([]byte("abc"))))
	println(string(bytes.TrimSpace([]byte("  x  "))))
	println(bytes.EqualFold([]byte("ABC"), []byte("abc")))
	parts := bytes.Split([]byte("a,b"), []byte(","))
	println(len(parts), string(parts[0]))
	println(string(bytes.Join([][]byte{[]byte("a"), []byte("b")}, []byte("-"))))
	println(string(bytes.Repeat([]byte("ab"), 2)))
	b := bytes.NewBufferString("xy")
	println(b.String(), b.Len())
	_ = b.WriteByte('z')
	println(b.Len())
	println(string(b.Next(2)))
	r := bytes.NewReader([]byte("abc"))
	println(r.Len(), r.Size())
	r.Reset([]byte("z"))
	println(r.Len())
	println(bytes.MinRead)
	println(bytes.ErrTooLarge.Error())
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synbytes.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	// bytesResults records, for each funcdecl name in pkgpath "bytes",
	// the largest result count seen (0/1 = single-result or pair form;
	// >1 = raw multi-result leak).
	bytesResults := map[string]int{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						names[n] = true
						if pp, _ := f["pkgpath"].(string); pp == "bytes" {
							nr := 0
							if rs, ok := f["results"].([]any); ok {
								nr = len(rs)
							}
							if nr > bytesResults[n] {
								bytesResults[n] = nr
							}
						}
					}
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
	walk(raw)
	// Real-Go declarations must be exported as funcdecls (bare names —
	// the wire doc does not qualify synthetic members).
	for _, want := range []string{
		"Contains", "Count", "ToUpper", "TrimSpace", "EqualFold",
		"Split", "Join", "Repeat", "NewBufferString", "String",
		"WriteByte", "Next", "NewReader", "Len", "Size", "Reset",
		"IndexRune", "Map", "Fields", "Clone",
	} {
		if !names[want] {
			t.Errorf("wire doc missing funcdecl %q", want)
		}
	}
	// No bytes multi-result member may leak as a raw multi-result
	// funcdecl: table-covered members are skipped (helpers carry the
	// body) and the rest are pair-transformed (exactly 1 pair result).
	// Note: Cut, CutPrefix, CutSuffix, CutLast now have real implementations
	// and are pair-transformed via the general userMRFuncs mechanism.
	for _, name := range []string{
		"Read", "Write", "ReadByte", "ReadRune", "ReadBytes", "ReadString",
		"Peek", "WriteString", "WriteRune", "ReadFrom", "WriteTo", "Seek", "ReadAt",
	} {
		if nr := bytesResults[name]; nr > 1 {
			t.Errorf("wire doc contains raw multi-result bytes funcdecl %q (%d results)", name, nr)
		}
	}
}

// TestSyntheticBytesConvertedMethods verifies the L-I (REVIEW-2026-11-03-16
// P1-R1) retirement of the M7 fail-fast stubs for the bytes Buffer/Reader
// multi-result members with real gc 1.27.1 bodies: direct calls export
// (previously Reader.Seek failed export with the WS D scope error — see
// the retired TestSyntheticBytesCutFailsFast), and each converted member
// appears in the wire doc in pair-transformed (exactly 1 result) form,
// never as a raw multi-result funcdecl. ReadBytes/ReadString exercise the
// unexported readSlice intra-package call, so its pair form is asserted
// too. This test pins export shape, not execution: the backend must still
// lower the pair calls, which the differential corpus covers.
func TestSyntheticBytesConvertedMethods(t *testing.T) {
	src := `package main

import (
	"bytes"
	"io"
)

func main() {
	b := bytes.NewBufferString("a,b\nc")
	p, _ := b.Peek(2)
	println(len(p))
	n, _ := b.WriteRune('é')
	println(n)
	c, _ := b.ReadByte()
	println(c)
	r, sz, _ := b.ReadRune()
	println(r, sz)
	line, _ := b.ReadBytes('\n')
	println(len(line))
	s, _ := b.ReadString('\n')
	println(len(s))
	rdr := bytes.NewReader([]byte("hello"))
	buf := make([]byte, 3)
	m, _ := rdr.ReadAt(buf, 1)
	println(m)
	cb, _ := rdr.ReadByte()
	println(cb)
	rr, rs, _ := rdr.ReadRune()
	println(rr, rs)
	pos, _ := rdr.Seek(0, io.SeekStart)
	println(pos)
	var sk io.Seeker = rdr
	pos2, _ := sk.Seek(2, io.SeekStart)
	println(pos2)
}
`
	doc := decodeExport(t, mustExport(t, src))

	// Each converted member must appear as a pair-transformed funcdecl
	// (exactly 1 result) in pkgpath "bytes".
	results := map[string]int{}
	for _, f := range findAllKind(doc, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		pp, _ := fl["pkgpath"].(string)
		if pp != "bytes" {
			continue
		}
		name, _ := fl["name"].(string)
		nr := 0
		if rs, ok := fl["results"].([]any); ok {
			nr = len(rs)
		}
		if nr > results[name] {
			results[name] = nr
		}
	}
	for _, name := range []string{
		"Peek", "WriteRune", "ReadByte", "ReadRune", "ReadBytes",
		"readSlice", "ReadString", "ReadAt", "Seek",
	} {
		nr, ok := results[name]
		if !ok {
			t.Errorf("wire doc missing funcdecl %q", name)
		} else if nr != 1 {
			t.Errorf("wire doc funcdecl %q has %d results, want pair form (1)", name, nr)
		}
	}
	// The interface Seek call must desugar to an ifacemethod dispatch
	// node, not fail fast.
	sawSeek := false
	for _, im := range findAllKind(doc, "ifacemethod") {
		fl, _ := im["fields"].(map[string]any)
		if fl["method"] == "Seek" {
			sawSeek = true
		}
	}
	if !sawSeek {
		t.Error("no ifacemethod dispatch node for the io.Seeker.Seek interface call")
	}
}

// TestSyntheticBytesBufferReadDesugar verifies the retired M7 fail-fast
// stub for (*bytes.Buffer).Read: the m7 table now drives it through the
// real __bytesBufferRead helper (gc 1.27.1 semantics), and the
// bytes.Buffer method table maps Read to that helper so interface
// dispatch (e.g. io.ReadAll over a *bytes.Buffer) resolves.
//
// Red-green: pre-fix, the direct call below fails with the named M7 scope
// error, the wire has no __bytesBufferRead funcdecl, and the bytes.Buffer
// type-table entry carries no methods.
func TestSyntheticBytesBufferReadDesugar(t *testing.T) {
	src := `package main

import (
	"bytes"
	"io"
)

func main() {
	b := bytes.NewBufferString("hi")
	p := make([]byte, 4)
	n, _ := b.Read(p)
	println(n)
	var r io.Reader = b
	m, _ := r.Read(p)
	println(m)
}
`
	doc := decodeExport(t, mustExport(t, src))

	// The helper funcdecl is emitted.
	helperObj := -1
	for _, f := range findAllKind(doc, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		if fl["name"] == "__bytesBufferRead" {
			o, _ := fl["obj"].(float64)
			helperObj = int(o)
		}
	}
	if helperObj < 0 {
		t.Fatal("wire doc missing funcdecl __bytesBufferRead")
	}

	// The *bytes.Buffer pointer entry maps Read to the helper's obj id.
	// (Pointer entries carry the method set; the named bytes.Buffer
	// entry's value method set is empty — all its methods have pointer
	// receivers. Pointer entries have no "name" field; they are found
	// via "elem".)
	bufID := -1
	for i, te := range doc["types"].([]any) {
		tm, _ := te.(map[string]any)
		if tm["kind"] != "named" {
			continue
		}
		det, _ := tm["detail"].(map[string]any)
		if det["name"] == "bytes.Buffer" {
			bufID = i
		}
	}
	if bufID < 0 {
		t.Fatal("no named bytes.Buffer type-table entry")
	}
	found := false
	for _, te := range doc["types"].([]any) {
		tm, _ := te.(map[string]any)
		if tm["kind"] != "pointer" {
			continue
		}
		det, _ := tm["detail"].(map[string]any)
		elem, _ := det["elem"].(float64)
		if int(elem) != bufID {
			continue
		}
		for _, m := range det["methods"].([]any) {
			mm, _ := m.(map[string]any)
			if mm["name"] == "Read" {
				found = true
				o, _ := mm["obj"].(float64)
				if int(o) != helperObj {
					t.Fatalf("*bytes.Buffer Read obj %d, want __bytesBufferRead obj %d", int(o), helperObj)
				}
			}
		}
	}
	if !found {
		t.Fatal("*bytes.Buffer type-table entry has no Read method")
	}

	// The interface call desugars to an ifacemethod dispatch node (not a
	// fail-fast): the itab target above is what it resolves to at runtime.
	ifacemethods := findAllKind(doc, "ifacemethod")
	sawRead := false
	for _, im := range ifacemethods {
		fl, _ := im["fields"].(map[string]any)
		if fl["method"] == "Read" {
			sawRead = true
		}
	}
	if !sawRead {
		t.Fatal("no ifacemethod dispatch node for the r.Read(p) interface call")
	}
}
