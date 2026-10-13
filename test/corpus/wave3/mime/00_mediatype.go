// P3-M9 wave3: mime — ParseMediaType / FormatMediaType / encoded words.
// SCOPE-LIMITED: no synthetic mime source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"fmt"
	"mime"
)

func main() {
	// Basic parse with params.
	mt, params, err := mime.ParseMediaType("text/html; charset=utf-8")
	fmt.Println("basic", mt == "text/html", params["charset"] == "utf-8", err == nil)

	// Quoted param with spaces and semicolon.
	mt, params, err = mime.ParseMediaType(`attachment; filename="foo; bar.txt"`)
	fmt.Println("quoted", mt == "attachment", params["filename"] == "foo; bar.txt", err == nil)

	// Case-insensitivity of type, param keys lowercased.
	mt, params, err = mime.ParseMediaType("Text/HTML; Charset=UTF-8")
	fmt.Println("case", mt == "text/html", params["charset"] == "UTF-8", err == nil)

	// RFC 2231 continuation: filename*0* / filename*1*.
	mt, params, err = mime.ParseMediaType(
		`attachment; filename*0*=utf-8''foo%20; filename*1*=bar.txt`)
	fmt.Println("rfc2231-cont", mt == "attachment", params["filename"] == "foo bar.txt", err == nil)

	// RFC 2231 single encoded param.
	_, params, err = mime.ParseMediaType(`attachment; filename*=utf-8''%e2%82%ac.txt`)
	fmt.Println("rfc2231-enc", params["filename"] == "€.txt", err == nil)

	// RFC 2231 continuation mixing encoded and plain sections: when the
	// first section is not encoded, later sections are taken literally
	// (the utf-8'' prefix is NOT stripped).
	_, params, err = mime.ParseMediaType(
		`attachment; filename*0="foo"; filename*1*=utf-8''%20bar.txt`)
	fmt.Println("rfc2231-mix", params["filename"] == "fooutf-8'' bar.txt", err == nil)

	// Whitespace around separators.
	mt, params, err = mime.ParseMediaType("  text/plain ; charset = us-ascii  ")
	fmt.Println("ws", mt == "text/plain", params["charset"] == "us-ascii", err == nil)

	// FormatMediaType round-trip; needs-quoting param.
	s := mime.FormatMediaType("text/html", map[string]string{"charset": "utf-8"})
	fmt.Println("format", s == "text/html; charset=utf-8")
	s = mime.FormatMediaType("attachment", map[string]string{"filename": "a b.txt"})
	mt2, p2, err2 := mime.ParseMediaType(s)
	fmt.Println("format-rt", mt2 == "attachment", p2["filename"] == "a b.txt", err2 == nil)

	// Encoded words: Q-encoding decode.
	dec := new(mime.WordDecoder)
	str, err := dec.DecodeHeader("=?utf-8?q?hello_=E2=82=AC?=")
	fmt.Println("word-q", str == "hello €", err == nil)

	// Encoded words: B-encoding decode.
	str, err = dec.DecodeHeader("=?utf-8?b?aGVsbG8gd29ybGQ=?=")
	fmt.Println("word-b", str == "hello world", err == nil)

	// Encoded words: multiple words joined, unencoded text preserved.
	str, err = dec.DecodeHeader("Re: =?utf-8?q?caf=C3=A9?= =?utf-8?q?_au_lait?=")
	fmt.Println("word-multi", str == "Re: café au lait", err == nil)

	// Encoded words: unknown charset -> error.
	_, err = dec.DecodeHeader("=?x-unknown?q?abc?=")
	fmt.Println("word-badcharset", err != nil)

	// Encoded words: malformed word is passed through unchanged, no error.
	str, err = dec.DecodeHeader("=?utf-8?q?abc")
	fmt.Println("word-malformed", str == "=?utf-8?q?abc", err == nil)

	// WordEncoder determinism.
	enc := mime.QEncoding.Encode("utf-8", "hello €")
	fmt.Println("encode-q", enc == "=?utf-8?q?hello_=E2=82=AC?=")
	enc = mime.BEncoding.Encode("utf-8", "hello world")
	str2, err := dec.DecodeHeader(enc)
	fmt.Println("encode-b-rt", str2 == "hello world", err == nil)

	// Adversarial: missing slash — no error, returned verbatim.
	mt, _, err = mime.ParseMediaType("bogus")
	fmt.Println("adv-noslash", mt == "bogus", err == nil)

	// Adversarial: empty.
	_, _, err = mime.ParseMediaType("")
	fmt.Println("adv-empty", err != nil)

	// Adversarial: bad token char in type.
	_, _, err = mime.ParseMediaType("te xt/html")
	fmt.Println("adv-badtoken", err != nil)

	// Adversarial: unterminated quote.
	_, _, err = mime.ParseMediaType(`text/html; charset="utf-8`)
	fmt.Println("adv-untermquote", err != nil)

	// Adversarial: duplicate param -> error, not last-wins.
	_, _, err = mime.ParseMediaType("text/html; charset=a; charset=b")
	fmt.Println("adv-dupparam", err != nil)

	fmt.Println("ok")
}
