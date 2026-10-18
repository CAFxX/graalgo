package main

// RV-P1 (2026-10-18): itab-wiring pin for the five synthetic compress
// packages' Read methods.
//
// The five Read methods (compress/flate decompressor.Read,
// compress/gzip Reader.Read, compress/zlib reader.Read,
// compress/lzw Reader.Read, compress/bzip2 reader.Read) are table-driven
// multi-result methods (m7MultiResultFuncs). Their bodies were fail-fast
// panics; they are now delegating bodies, but the runtime dispatch for
// interface calls (r.Read(p), io.ReadAll(r)) must still route through the
// itab to the pair-returning __-helpers — never to the method body.
//
// This test pins that wiring: for each concrete reader type, every
// method-table entry must map Read to the expected __-helper's funcdecl
// id (following the TestD1IfaceMethodTableHelperMapping pattern).
// A mis-wired itab — the method attached to the wrong type, or pointing
// at the method body instead of the helper — fails here.

import (
	"testing"
)

func TestCompressReadIfaceMethodTableHelperMapping(t *testing.T) {
	const src = `package main

import (
	"bytes"
	"compress/bzip2"
	"compress/flate"
	"compress/gzip"
	"compress/lzw"
	"compress/zlib"
	"io"
)

func main() {
	// Each reader is converted to io.Reader so the exporter builds the
	// interface method table for the concrete type.
	gz, _ := gzip.NewReader(bytes.NewReader(nil))
	var r1 io.Reader = gz
	var r2 io.Reader = flate.NewReader(bytes.NewReader(nil))
	var r3 io.Reader = lzw.NewReader(bytes.NewReader(nil), lzw.MSB, 8)
	zr, _ := zlib.NewReader(bytes.NewReader(nil))
	var r4 io.Reader = zr
	var r5 io.Reader = bzip2.NewReader(bytes.NewReader(nil))
	println(r1 == nil, r2 == nil, r3 == nil, r4 == nil, r5 == nil)
}
`
	doc := d1ExportIfaceProg(t, src)
	funcIDs := d1FuncIDByName(t, doc)
	cases := []struct {
		typeName string
		method   string
		helper   string
	}{
		{"*gzip.Reader", "Read", "__gzipReaderRead"},
		{"*flate.decompressor", "Read", "__flateDecompressorRead"},
		{"*zlib.reader", "Read", "__zlibReaderRead"},
		{"*lzw.Reader", "Read", "__lzwReaderRead"},
		{"*bzip2.reader", "Read", "__bzip2ReaderRead"},
	}
	for _, c := range cases {
		t.Run(c.typeName+"."+c.method, func(t *testing.T) {
			tables := d1MethodTable(t, doc, c.typeName)
			for ti, ms := range tables {
				found := false
				for _, m := range ms {
					mm, ok := m.(map[string]any)
					if !ok || mm["name"] != c.method {
						continue
					}
					if found {
						t.Fatalf("table %d for %s: duplicate %s entry", ti, c.typeName, c.method)
					}
					found = true
					obj, _ := mm["obj"].(float64)
					if got := funcIDs[obj]; got != c.helper {
						t.Errorf("table %d for %s: obj = %v (%q), want helper %q",
							ti, c.typeName, obj, got, c.helper)
					}
				}
				if !found {
					t.Errorf("table %d for %s: no %s entry", ti, c.typeName, c.method)
				}
			}
		})
	}
}
