package main

import "encoding/xml"

// xml00: basic token stream — elements, attributes, chardata.
// Dual-mode reader: Read for gc's io.Reader, Next for synthetic.
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

func main() {
	data := []byte(`<root a="1" b="two"><child>hello &lt;world&gt;</child><!-- comment --><?pi target?><empty/></root>`)
	d := xml.NewDecoder(&sliceReader{data: data})
	for {
		tok, err := d.Token()
		if err != nil {
			println("ERR:", err.Error())
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			println("START:", t.Name.Local, len(t.Attr))
			for _, a := range t.Attr {
				println("  ATTR:", a.Name.Local, a.Value)
			}
		case xml.EndElement:
			println("END:", t.Name.Local)
		case xml.CharData:
			println("CHARDATA:", string(t))
		case xml.Comment:
			println("COMMENT:", string(t))
		case xml.ProcInst:
			println("PROCINST:", t.Target, string(t.Inst))
		case xml.Directive:
			println("DIRECTIVE:", string(t))
		}
	}
}
