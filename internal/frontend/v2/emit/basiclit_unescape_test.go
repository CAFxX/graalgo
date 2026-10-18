// Tests for L02: string/rune literals must be decoded on the Go side.
//
// The v2 wire format carries basic literals as raw source text in "value".
// Go escape sequences (\n, \x41, \u00e9, octal escapes), raw-string
// backquotes, and multi-byte runes cannot be recovered from that text by
// quote-stripping on the Java side (the L02 silent data corruption bug).
// The emitter therefore decodes every string/rune literal with
// strconv.Unquote and ships the decoded value in "str" (format v2).
package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// unescapeCase is one literal whose decoded wire value is asserted.
type unescapeCase struct {
	name string // test name
	lit  string // Go literal source text, as written in the program
	str  string // expected decoded "str" field ("" = field must be absent)
	// constVal is the expected "const" field; "" means do not check.
	constVal string
}

var unescapeCases = []unescapeCase{
	// Interpreted strings: escapes must be decoded.
	{name: "newline", lit: `"a\nb"`, str: "a\nb"},
	{name: "tab", lit: `"tab\there"`, str: "tab\there"},
	{name: "hexEscapes", lit: `"\x41\x42"`, str: "AB"},
	{name: "unicodeEscape", lit: `"caf\u00e9"`, str: "café"},
	{name: "astralEscape", lit: `"emoji\U0001F600"`, str: "emoji😀"},
	{name: "octalEscape", lit: `"\101"`, str: "A"},
	{name: "quoteEscape", lit: `"say \"hi\""`, str: `say "hi"`},
	// A decoded value that itself starts and ends with a quote: this is why
	// "str" is a separate field instead of replacing "value" — a Java side
	// that stripped quotes from the decoded text would corrupt this to "".
	{name: "decodedQuotes", lit: `"\"\""`, str: `""`},
	// Raw strings: backquotes stripped, contents verbatim (no unescaping).
	{name: "rawBackslash", lit: "`raw\\nbackquote`", str: `raw\nbackquote`},
	{name: "rawNewline", lit: "`line1\nline2`", str: "line1\nline2"},
	{name: "rawQuotes", lit: "`say \"hi\"`", str: `say "hi"`},
	// Rune literals: decoded to the single character.
	{name: "runeSimple", lit: `'a'`, str: "a"},
	{name: "runeNewline", lit: `'\n'`, str: "\n"},
	{name: "runeHex", lit: `'\x41'`, str: "A"},
	{name: "runeUnicode", lit: `'é'`, str: "é"},
	{name: "runeOctal", lit: `'\101'`, str: "A"},
	{name: "runeAstral", lit: `'😀'`, str: "😀"},
	// Integer literals: the canonical decimal rides in "const" (go/constant),
	// normalizing the legacy octal/hex/binary/underscore spellings that the
	// Java side's raw Long.parseLong(value) misdecoded (017 -> 17 not 15).
	{name: "legacyOctal", lit: `017`, constVal: "15"},
	{name: "hex", lit: `0x10`, constVal: "16"},
	{name: "binary", lit: `0b101`, constVal: "5"},
	{name: "octalPrefix", lit: `0o17`, constVal: "15"},
	{name: "underscores", lit: `1_000`, constVal: "1000"},
	{name: "decimal", lit: `42`, constVal: "42"},
}

// emitOneLit emits "package main; func main() { _ = <lit> }" and returns the
// fields of the single basiclit node it must contain.
func emitOneLit(t *testing.T, lit string) map[string]any {
	t.Helper()
	src := "package main\nfunc main() {\n_ = " + lit + "\n}\n"
	dir := t.TempDir()
	fn := filepath.Join(dir, "p.go")
	if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	pres, err := parser.ParseFiles([]string{fn})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	doc, err := Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	var found []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["kind"] == "basiclit" {
				f, ok := x["fields"].(map[string]any)
				if !ok {
					t.Fatalf("basiclit node has no fields map")
				}
				found = append(found, f)
			}
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(doc.Funcs)
	if len(found) != 1 {
		t.Fatalf("expected exactly 1 basiclit for %s, found %d", lit, len(found))
	}
	return found[0]
}

func TestBasicLitUnescape(t *testing.T) {
	for _, tc := range unescapeCases {
		t.Run(tc.name, func(t *testing.T) {
			fields := emitOneLit(t, tc.lit)
			if tc.str != "" {
				got, ok := fields["str"].(string)
				if !ok {
					t.Fatalf("lit %s: missing string \"str\" field (fields=%v)", tc.lit, fields)
				}
				if got != tc.str {
					t.Fatalf("lit %s: str = %q, want %q", tc.lit, got, tc.str)
				}
			} else if _, ok := fields["str"]; ok {
				t.Fatalf("lit %s: unexpected \"str\" field %q", tc.lit, fields["str"])
			}
			if tc.constVal != "" {
				got, ok := fields["const"].(string)
				if !ok {
					t.Fatalf("lit %s: missing string \"const\" field (fields=%v)", tc.lit, fields)
				}
				if got != tc.constVal {
					t.Fatalf("lit %s: const = %q, want %q", tc.lit, got, tc.constVal)
				}
			}
			// "value" keeps the raw source text for diagnostics.
			if got, ok := fields["value"].(string); !ok || got != tc.lit {
				t.Fatalf("lit %s: value = %q, want raw source %q", tc.lit, fields["value"], tc.lit)
			}
		})
	}
}
