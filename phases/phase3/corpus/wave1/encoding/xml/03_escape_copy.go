package main

import "encoding/xml"

// xml03: EscapeText/Escape, CopyToken, SyntaxError, HTMLEntity.
type bufWriter struct {
	buf []byte
}

func (w *bufWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func (w *bufWriter) WriteBytes(p []byte) {
	w.buf = append(w.buf, p...)
}

func main() {
	w := &bufWriter{}
	// EscapeText escapes < > & ' " and \n.
	xml.EscapeText(w, []byte(`<a href="x&y">it's
newline</a>`))
	println("ESCAPED:", string(w.buf))

	w2 := &bufWriter{}
	xml.Escape(w2, []byte(`<>&'"`))
	println("ESCAPE:", string(w2.buf))

	// CopyToken deep-copies.
	se := xml.StartElement{
		Name: xml.Name{Space: "urn:x", Local: "root"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "a"}, Value: "1"}},
	}
	c := xml.CopyToken(se).(xml.StartElement)
	c.Attr[0].Value = "MUTATED"
	println("ORIG ATTR:", se.Attr[0].Value)
	println("COPY ATTR:", c.Attr[0].Value)

	cd := xml.CharData("hello")
	cd2 := xml.CopyToken(cd).(xml.CharData)
	println("CHARDATA EQ:", string(cd) == string(cd2))

	// SyntaxError rendering.
	se2 := &xml.SyntaxError{Msg: "test error"}
	println("SYNTAXERR:", se2.Error())

	// HTMLEntity lookup.
	println("ENTITY amp:", xml.HTMLEntity["amp"])
	println("ENTITY lt:", xml.HTMLEntity["lt"])
	println("ENTITY euro:", xml.HTMLEntity["euro"])

	// StartElement.End and Copy.
	e := se.End()
	println("END NAME:", e.Name.Local, e.Name.Space)
	seCopy := se.Copy()
	println("COPY NAME:", seCopy.Name.Local)

	// InputOffset.
	sr := &sliceReader{data: []byte(`<a>xy</a>`)}
	d := xml.NewDecoder(sr)
	d.Token()
	println("OFFSET:", d.InputOffset())
}

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
