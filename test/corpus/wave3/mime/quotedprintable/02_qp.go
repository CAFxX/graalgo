// P3-M9 wave3: mime/quotedprintable — Writer/Reader round-trip with soft
// breaks, special chars, trailing whitespace; adversarial inputs.
// SCOPE-LIMITED: no synthetic mime/quotedprintable source in frontend/v1
// yet (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/quotedprintable"
	"strings"
)

func main() {
	// Round-trip: long lines (soft breaks), '=', trailing space, non-ASCII.
	input := "line one is deliberately longer than seventy-six characters so a soft break must appear here=\n" +
		"trailing space here \n" +
		"caf\u00e9 \u20ac and a = sign\n" +
		"short\n"
	var buf bytes.Buffer
	w := quotedprintable.NewWriter(&buf)
	if _, err := io.WriteString(w, input); err != nil {
		fmt.Println("write-err", err)
		return
	}
	if err := w.Close(); err != nil {
		fmt.Println("close-err", err)
		return
	}
	enc := buf.String()
	fmt.Println("softbreak", strings.Contains(enc, "=\r\n"))
	fmt.Println("eq-encoded", strings.Contains(enc, "=3D"))
	fmt.Println("trailspace-encoded", strings.Contains(enc, "=20\r\n"))
	fmt.Println("nonascii-encoded", strings.Contains(enc, "=C3=A9"))

	r := quotedprintable.NewReader(strings.NewReader(enc))
	out, err := io.ReadAll(r)
	// The writer CRLF-normalizes bare \n on write; the reader decodes
	// back to the CRLF form, not the original LF input.
	fmt.Println("roundtrip", err == nil,
		string(out) == strings.ReplaceAll(input, "\n", "\r\n"))

	// Writer with no writes: empty output.
	var buf2 bytes.Buffer
	w2 := quotedprintable.NewWriter(&buf2)
	w2.Close()
	fmt.Println("empty", buf2.Len() == 0)

	// Reader is case-insensitive on hex digits (pins behavior).
	r = quotedprintable.NewReader(strings.NewReader("=e2=82=ac=E2=82=AC"))
	out, err = io.ReadAll(r)
	fmt.Println("hexcase", err == nil, string(out) == "\u20ac\u20ac")

	// Soft break at exact line limit is not emitted mid-escape.
	var buf3 bytes.Buffer
	w3 := quotedprintable.NewWriter(&buf3)
	w3.Write(bytes.Repeat([]byte("x"), 75))
	w3.Write([]byte("\u00e9")) // would overflow the 76-col limit mid-escape
	w3.Close()
	r = quotedprintable.NewReader(bytes.NewReader(buf3.Bytes()))
	out, err = io.ReadAll(r)
	fmt.Println("escape-boundary", err == nil,
		string(out) == string(bytes.Repeat([]byte("x"), 75))+"\u00e9")

	// Adversarial: the reader is lenient — invalid escapes are passed
	// through verbatim instead of erroring (pins the leniency).
	r = quotedprintable.NewReader(strings.NewReader("abc=ZZ"))
	out, err = io.ReadAll(r)
	fmt.Println("adv-badhex", err == nil, string(out) == "abc=ZZ")

	// Adversarial: truncated escape at EOF -> raw byte passthrough.
	r = quotedprintable.NewReader(strings.NewReader("abc=E2"))
	out, err = io.ReadAll(r)
	fmt.Println("adv-truncesc", err == nil, string(out) == "abc\xe2")

	// Adversarial: lone '=' at EOF is dropped.
	r = quotedprintable.NewReader(strings.NewReader("abc="))
	out, err = io.ReadAll(r)
	fmt.Println("adv-loneeq", err == nil, string(out) == "abc")

	// Adversarial: soft break is dropped, not literal.
	r = quotedprintable.NewReader(strings.NewReader("ab=\r\ncd"))
	out, err = io.ReadAll(r)
	fmt.Println("softbreak-drop", err == nil, string(out) == "abcd")

	fmt.Println("ok")
}
