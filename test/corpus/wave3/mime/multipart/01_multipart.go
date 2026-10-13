// P3-M9 wave3: mime/multipart — Writer→Reader round-trip, nested parts,
// ReadForm spilling, adversarial inputs.
// SCOPE-LIMITED: no synthetic mime/multipart source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
)

func main() {
	// Build a message: field part, file part, unicode part.
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.SetBoundary("testboundary123"); err != nil {
		fmt.Println("setboundary-err", err)
		return
	}

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="field1"`)
	h.Set("Content-Type", "text/plain")
	pw, err := w.CreatePart(h)
	if err != nil {
		fmt.Println("createpart-err", err)
		return
	}
	fmt.Fprintln(pw, "value1")

	fw, err := w.CreateFormFile("upload", "hello.txt")
	if err != nil {
		fmt.Println("createformfile-err", err)
		return
	}
	fmt.Fprint(fw, "file-bytes-")

	uw, err := w.CreateFormField("uni")
	if err != nil {
		fmt.Println("createformfield-err", err)
		return
	}
	fmt.Fprint(uw, "caf\u00e9 \u20ac")
	if err := w.Close(); err != nil {
		fmt.Println("close-err", err)
		return
	}
	fmt.Println("boundary", w.Boundary() == "testboundary123")

	// Read back.
	r := multipart.NewReader(&buf, w.Boundary())
	type part struct{ name, file, body string }
	var parts []part
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("nextpart-err", err)
			return
		}
		b, err := io.ReadAll(p)
		if err != nil {
			fmt.Println("readpart-err", err)
			return
		}
		parts = append(parts, part{p.FormName(), p.FileName(), string(b)})
	}
	fmt.Println("nparts", len(parts) == 3)
	fmt.Println("p0", parts[0].name == "field1", parts[0].file == "", parts[0].body == "value1\n")
	fmt.Println("p1", parts[1].name == "upload", parts[1].file == "hello.txt", parts[1].body == "file-bytes-")
	fmt.Println("p2", parts[2].name == "uni", parts[2].body == "caf\u00e9 \u20ac")

	// Nested multipart: a part whose body is itself a multipart message.
	var outer bytes.Buffer
	ow := multipart.NewWriter(&outer)
	ow.SetBoundary("outer456")
	var inner bytes.Buffer
	iw := multipart.NewWriter(&inner)
	iw.SetBoundary("inner789")
	ipw, _ := iw.CreateFormField("innerfield")
	fmt.Fprint(ipw, "innervalue")
	iw.Close()
	nh := make(textproto.MIMEHeader)
	nh.Set("Content-Disposition", `form-data; name="nested"`)
	nh.Set("Content-Type", "multipart/mixed; boundary=inner789")
	npw, _ := ow.CreatePart(nh)
	npw.Write(inner.Bytes())
	ow.Close()

	or := multipart.NewReader(&outer, ow.Boundary())
	np, err := or.NextPart()
	if err != nil {
		fmt.Println("nested-nextpart-err", err)
		return
	}
	nb, _ := io.ReadAll(np)
	ir := multipart.NewReader(bytes.NewReader(nb), "inner789")
	ip, err := ir.NextPart()
	if err != nil {
		fmt.Println("nested-inner-err", err)
		return
	}
	ib, _ := io.ReadAll(ip)
	_, err = ir.NextPart()
	fmt.Println("nested", ip.FormName() == "innerfield", string(ib) == "innervalue", err == io.EOF)

	// ReadForm with tiny maxMemory: file part spills to disk (TMPDIR).
	var fbuf bytes.Buffer
	fw2 := multipart.NewWriter(&fbuf)
	fw2.SetBoundary("form999")
	big, _ := fw2.CreateFormFile("big", "big.bin")
	big.Write(bytes.Repeat([]byte("z"), 4096))
	fw2.Close()
	fr := multipart.NewReader(&fbuf, fw2.Boundary())
	mf, err := fr.ReadForm(10)
	if err != nil {
		fmt.Println("readform-err", err)
		return
	}
	defer mf.RemoveAll()
	fhs := mf.File["big"]
	f0, err := fhs[0].Open()
	if err != nil {
		fmt.Println("readform-open-err", err)
		return
	}
	fb, _ := io.ReadAll(f0)
	f0.Close()
	fmt.Println("readform", len(fhs) == 1, len(fb) == 4096, bytes.Equal(fb, bytes.Repeat([]byte("z"), 4096)))

	// Adversarial: truncated message.
	trunc := buf.Bytes()[:len(buf.Bytes())/2]
	tr := multipart.NewReader(bytes.NewReader(trunc), "testboundary123")
	advErr := false
	for {
		p, err := tr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			advErr = true
			break
		}
		io.Copy(io.Discard, p)
	}
	fmt.Println("adv-truncated", advErr)

	// Adversarial: wrong boundary -> no parts.
	wr := multipart.NewReader(bytes.NewReader(buf.Bytes()), "wrongboundary")
	_, err = wr.NextPart()
	fmt.Println("adv-wrongboundary", err != nil)

	// Adversarial: empty boundary.
	er := multipart.NewReader(bytes.NewReader(buf.Bytes()), "")
	_, err = er.NextPart()
	fmt.Println("adv-emptyboundary", err != nil)

	fmt.Println("ok")
}
