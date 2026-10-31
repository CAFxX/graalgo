package main

import "testing"

// TestSyntheticBytesMRDischarge verifies the review-bytes-mr WS D
// discharge of the remaining bytes multi-result members: every member
// listed here previously kept an M7/M8 fail-fast stub with no Appendix-M
// desugar row, so any call failed at export with the named scope error
// (or, for positions the scope checks missed, would have linked to a
// runtime panic stub). After the discharge each member has a real
// single-result __-helper and an m7MultiResultFuncs row, so direct
// calls export cleanly, the helper funcdecls are emitted, and no raw
// multi-result bytes funcdecl leaks into the wire doc.
//
// Red-green: before the discharge, mustExport fails for the program
// below with the named M7/M8 scope error (e.g. bytes.Reader.Seek).
func TestSyntheticBytesMRDischarge(t *testing.T) {
	src := `package main

import "bytes"

func main() {
	b := bytes.NewBufferString("a,b\nxy")
	pk, _ := b.Peek(3)
	println(len(pk))
	n64, _ := b.ReadFrom(bytes.NewReader([]byte("z")))
	println(n64)
	var w bytes.Buffer
	m64, _ := b.WriteTo(&w)
	println(m64)
	nw, _ := b.WriteRune('x')
	println(nw)
	c, _ := b.ReadByte()
	println(c)
	r, sz, _ := b.ReadRune()
	println(r, sz)
	line, _ := b.ReadBytes(',')
	println(len(line))
	s, _ := b.ReadString('\n')
	println(len(s))
	rd := bytes.NewReader([]byte("hello"))
	p := make([]byte, 3)
	na, _ := rd.ReadAt(p, 1)
	println(na)
	cb, _ := rd.ReadByte()
	println(cb)
	ch, sz2, _ := rd.ReadRune()
	println(ch, sz2)
	pos, _ := rd.Seek(0, 0)
	println(pos)
	var w2 bytes.Buffer
	mw, _ := rd.WriteTo(&w2)
	println(mw)
}
`
	doc := decodeExport(t, mustExport(t, src))
	assertBytesMRHelpers(t, doc, []string{
		"__bytesBufferPeek",
		"__bytesBufferReadFrom",
		"__bytesBufferWriteTo",
		"__bytesBufferWriteRune",
		"__bytesBufferReadByte",
		"__bytesBufferReadRune",
		"__bytesBufferReadBytes",
		"__bytesBufferReadString",
		"__bytesReaderReadAt",
		"__bytesReaderReadByte",
		"__bytesReaderReadRune",
		"__bytesReaderSeek",
		"__bytesReaderWriteTo",
	})
	assertNoRawBytesMultiResult(t, doc, []string{
		"Peek", "ReadFrom", "WriteTo", "WriteRune", "ReadByte",
		"ReadRune", "ReadBytes", "ReadString", "ReadAt", "Seek",
	})
}

// TestSyntheticBytesMRDischargeIface verifies the same discharged
// members through interface positions (io.Seeker, io.ReaderAt,
// io.ByteReader, io.RuneReader, io.WriterTo, io.ReaderFrom), where the
// itab maps the interface method to the concrete type's __-helper.
func TestSyntheticBytesMRDischargeIface(t *testing.T) {
	src := `package main

import (
	"bytes"
	"io"
)

func main() {
	b := bytes.NewBufferString("hello")
	rd := bytes.NewReader([]byte("hello"))
	p := make([]byte, 3)
	var s io.Seeker = rd
	_, _ = s.Seek(1, 0)
	var ra io.ReaderAt = rd
	_, _ = ra.ReadAt(p, 0)
	var br io.ByteReader = rd
	_, _ = br.ReadByte()
	var rr io.RuneReader = rd
	_, _, _ = rr.ReadRune()
	var wt io.WriterTo = rd
	var w2 bytes.Buffer
	_, _ = wt.WriteTo(&w2)
	var rf io.ReaderFrom = b
	_, _ = rf.ReadFrom(rd)
	var wt2 io.WriterTo = b
	_, _ = wt2.WriteTo(&w2)
}
`
	doc := decodeExport(t, mustExport(t, src))
	assertBytesMRHelpers(t, doc, []string{
		"__bytesReaderSeek",
		"__bytesReaderReadAt",
		"__bytesReaderReadByte",
		"__bytesReaderReadRune",
		"__bytesReaderWriteTo",
		"__bytesBufferReadFrom",
		"__bytesBufferWriteTo",
	})
}

// TestM7BytesMRDischargeWiring pins every new m7MultiResultFuncs row
// against its synthetic package with the shared checker: exactly one
// table entry, the __-helper exists with matching params, and the pair
// struct's fields match the multi-result signature's result types in
// order. This is the item-5 assertion for the bytes rows (field names
// in the table must equal the pair struct's fields, and the pair field
// types must equal the signature's result types positionally).
func TestM7BytesMRDischargeWiring(t *testing.T) {
	wants := []m7StrconvJsonExpect{
		{"bytes", "Peek", "Buffer", "__bytesBufferPeek", "__bytesPeekPair", []string{"b", "err"}},
		{"bytes", "ReadFrom", "Buffer", "__bytesBufferReadFrom", "__bytesReadFromPair", []string{"n", "err"}},
		{"bytes", "WriteTo", "Buffer", "__bytesBufferWriteTo", "__bytesWriteToPair", []string{"n", "err"}},
		{"bytes", "WriteRune", "Buffer", "__bytesBufferWriteRune", "__bytesWritePair", []string{"n", "err"}},
		{"bytes", "ReadByte", "Buffer", "__bytesBufferReadByte", "__bytesReadBytePair", []string{"c", "err"}},
		{"bytes", "ReadRune", "Buffer", "__bytesBufferReadRune", "__bytesReadRunePair", []string{"r", "size", "err"}},
		{"bytes", "ReadBytes", "Buffer", "__bytesBufferReadBytes", "__bytesReadBytesPair", []string{"line", "err"}},
		{"bytes", "ReadString", "Buffer", "__bytesBufferReadString", "__bytesReadStringPair", []string{"line", "err"}},
		{"bytes", "ReadAt", "Reader", "__bytesReaderReadAt", "__bytesReadPair", []string{"n", "err"}},
		{"bytes", "ReadByte", "Reader", "__bytesReaderReadByte", "__bytesReadBytePair", []string{"c", "err"}},
		{"bytes", "ReadRune", "Reader", "__bytesReaderReadRune", "__bytesReadRunePair", []string{"r", "size", "err"}},
		{"bytes", "Seek", "Reader", "__bytesReaderSeek", "__bytesSeekPair", []string{"n", "err"}},
		{"bytes", "WriteTo", "Reader", "__bytesReaderWriteTo", "__bytesWriteToPair", []string{"n", "err"}},
	}
	for _, want := range wants {
		name := want.pkgPath + "." + want.recvType + "." + want.name
		t.Run(name, func(t *testing.T) {
			checkM7DesugarWiring(t, want)
		})
	}
}

// assertBytesMRHelpers fails unless every named __-helper is emitted as
// a funcdecl in the wire doc.
func assertBytesMRHelpers(t *testing.T, doc map[string]any, helpers []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, f := range findAllKind(doc, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		if name, _ := fl["name"].(string); name != "" {
			seen[name] = true
		}
	}
	for _, h := range helpers {
		if !seen[h] {
			t.Errorf("wire doc missing funcdecl %q", h)
		}
	}
}

// assertNoRawBytesMultiResult fails if any named bytes funcdecl in the
// wire doc carries more than one result (a raw multi-result leak: the
// desugar table covers the member, so only its single-result __-helper
// may appear).
func assertNoRawBytesMultiResult(t *testing.T, doc map[string]any, names []string) {
	t.Helper()
	// Walk the decoded doc like TestSyntheticBytesM7 does the raw one.
	bytesResults := map[string]int{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
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
	walk(doc)
	for _, name := range names {
		if nr := bytesResults[name]; nr > 1 {
			t.Errorf("wire doc contains raw multi-result bytes funcdecl %q (%d results)", name, nr)
		}
	}
}
