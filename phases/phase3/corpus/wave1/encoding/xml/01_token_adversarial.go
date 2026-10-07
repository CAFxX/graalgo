package main

import "encoding/xml"

// xml01: adversarial token stream — malformed XML, entities, namespaces,
// comments, CDATA, processing instructions, charset edge cases.
type sliceReader struct {
	data []byte
	off  int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, errEOF{}
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

func (r *sliceReader) Next() xml.ReadResult {
	if r.off >= len(r.data) {
		return xml.ReadResult{}
	}
	b := r.data[r.off]
	r.off++
	return xml.ReadResult{Byte: b, OK: true}
}

type errEOF struct{}

func (errEOF) Error() string { return "EOF" }

func runCase(name string, data []byte) {
	println("=== " + name + " ===")
	d := xml.NewDecoder(&sliceReader{data: data})
	for {
		tok, err := d.Token()
		if err != nil {
			println("ERR:", err.Error())
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			println("START:", t.Name.Space, t.Name.Local)
		case xml.EndElement:
			println("END:", t.Name.Space, t.Name.Local)
		case xml.CharData:
			println("CHARDATA:", string(t))
		case xml.Comment:
			println("COMMENT:", string(t))
		case xml.ProcInst:
			println("PROCINST:", t.Target)
		case xml.Directive:
			println("DIRECTIVE:", string(t))
		}
	}
}

func main() {
	// Entities: predefined, decimal, hex, invalid.
	runCase("entities", []byte(`<a>&lt;&gt;&amp;&apos;&quot;&#65;&#x42;&#x1F600;</a>`))
	// Namespaces.
	runCase("namespaces", []byte(`<x:root xmlns:x="urn:ex" xmlns="urn:def"><x:child/><y:other xmlns:y="urn:other"/></x:root>`))
	// CDATA.
	runCase("cdata", []byte(`<a><![CDATA[<not>xml</not>]]></a>`))
	// Comments and PIs.
	runCase("comments-pi", []byte(`<a><!-- c1 --><!--c2--><?pi data?><?xml version="1.0"?></a>`))
	// Mismatched tags (non-strict recovers).
	runCase("mismatch", []byte(`<a><b></a>`))
	// Unclosed tag.
	runCase("unclosed", []byte(`<a><b>`))
	// Bad entity.
	runCase("bad-entity", []byte(`<a>&nosuchentity;</a>`))
	// Strict mode via RawToken is not testable here (needs API); Token only.
	// Empty.
	runCase("empty", []byte(``))
	// Only text.
	runCase("text-only", []byte(`hello`))
}
