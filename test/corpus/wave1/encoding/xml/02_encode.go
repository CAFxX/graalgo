package main

import "encoding/xml"

// xml02: Encoder.EncodeToken — elements, chardata, comments, PIs, errors.
// Dual-mode writer: Write for gc's io.Writer, WriteBytes for synthetic.
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
	enc := xml.NewEncoder(w)
	// Basic document.
	se := xml.StartElement{Name: xml.Name{Local: "root"}, Attr: []xml.Attr{
		{Name: xml.Name{Local: "a"}, Value: "1"},
	}}
	if err := enc.EncodeToken(se); err != nil {
		println("ERR:", err.Error())
	}
	if err := enc.EncodeToken(xml.CharData("hello <world>")); err != nil {
		println("ERR:", err.Error())
	}
	if err := enc.EncodeToken(xml.Comment(" a comment ")); err != nil {
		println("ERR:", err.Error())
	}
	if err := enc.EncodeToken(xml.ProcInst{Target: "pi", Inst: []byte("data")}); err != nil {
		println("ERR:", err.Error())
	}
	if err := enc.EncodeToken(se.End()); err != nil {
		println("ERR:", err.Error())
	}
	if err := enc.Close(); err != nil {
		println("CLOSE ERR:", err.Error())
	}
	println("ENCODED:", string(w.buf))

	// Error: comment with --> marker.
	w2 := &bufWriter{}
	enc2 := xml.NewEncoder(w2)
	if err := enc2.EncodeToken(xml.Comment("bad --> marker")); err != nil {
		println("EXPECTED ERR:", err.Error())
	}
	enc2.Close()

	// Error: xml ProcInst not first.
	w3 := &bufWriter{}
	enc3 := xml.NewEncoder(w3)
	enc3.EncodeToken(xml.CharData("x"))
	if err := enc3.EncodeToken(xml.ProcInst{Target: "xml", Inst: []byte(`version="1.0"`)}); err != nil {
		println("EXPECTED ERR:", err.Error())
	}
	enc3.Close()

	// Indented output.
	w4 := &bufWriter{}
	enc4 := xml.NewEncoder(w4)
	enc4.Indent("", "  ")
	enc4.EncodeToken(xml.StartElement{Name: xml.Name{Local: "a"}})
	enc4.EncodeToken(xml.StartElement{Name: xml.Name{Local: "b"}})
	enc4.EncodeToken(xml.CharData("text"))
	enc4.EncodeToken(xml.EndElement{Name: xml.Name{Local: "b"}})
	enc4.EncodeToken(xml.EndElement{Name: xml.Name{Local: "a"}})
	enc4.Close()
	println("INDENTED:", string(w4.buf))
}
