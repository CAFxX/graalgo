package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P3-M7 (wave 1): export tests for the 8 encoding/* primitive synthetic
// packages (m7-wenc-core). A program importing each package exports
// cleanly; the real-Go declarations become funcdecls in the wire doc;
// the multi-result members are skipped at export (WS D — 2-/3-target
// call sites desugar via Appendix M, mrM7); the fail-fast single-result
// members (binary.Append, asn1.MarshalWithParams, ...) ARE exported
// (their panic bodies run if called).

func exportEnccoreSrc(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "p.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	return out
}

func enccoreFuncdecls(t *testing.T, out string) map[string]bool {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						names[n] = true
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
	walk(raw)
	return names
}

// TestSyntheticEnccoreM7 verifies the 8 encoding/* packages export: each
// import resolves, the real-Go bodies become funcdecls, and the
// multi-result members are skipped (desugared at call sites instead).
func TestSyntheticEnccoreM7(t *testing.T) {
	cases := []struct {
		pkg     string
		src     string
		present []string // funcdecls that must exist
		absent  []string // multi-result decls that must be skipped
	}{
		{
			"encoding/base32",
			"package main\nimport \"encoding/base32\"\nfunc main() {\nenc := base32.StdEncoding\ns := enc.EncodeToString([]byte(\"hello\"))\nprintln(s, enc.EncodedLen(5))\n}\n",
			[]string{"NewEncoding", "Encode", "EncodeToString", "EncodedLen", "DecodedLen", "WithPadding", "__base32Decode"},
			[]string{"Decode", "DecodeString", "AppendDecode"},
		},
		{
			"encoding/base64",
			"package main\nimport \"encoding/base64\"\nfunc main() {\nenc := base64.RawStdEncoding\ns := enc.EncodeToString([]byte(\"hello\"))\nprintln(s)\nprintln(base64.StdEncoding.EncodedLen(3))\n}\n",
			[]string{"NewEncoding", "Encode", "EncodeToString", "Strict", "WithPadding", "__base64Decode"},
			[]string{"Decode", "DecodeString", "AppendDecode"},
		},
		{
			"encoding/hex",
			"package main\nimport \"encoding/hex\"\nfunc main() {\nprintln(hex.EncodeToString([]byte(\"hi\")))\nprintln(hex.Dump([]byte(\"hi\")))\n}\n",
			[]string{"Encode", "EncodeToString", "EncodedLen", "DecodedLen", "Dump", "__hexDecode"},
			[]string{"Decode", "DecodeString", "AppendDecode"},
		},
		{
			"encoding/binary",
			"package main\nimport \"encoding/binary\"\nfunc main() {\nb := make([]byte, 8)\nbinary.LittleEndian.PutUint16(b, 1)\nprintln(binary.LittleEndian.Uint16(b))\nprintln(binary.PutUvarint(b, 300))\n}\n",
			[]string{"PutUvarint", "PutVarint", "AppendUvarint", "AppendVarint", "Uint16", "PutUint16", "__binaryUvarint"},
			[]string{"Uvarint", "Varint"},
		},
		{
			"encoding/ascii85",
			"package main\nimport \"encoding/ascii85\"\nfunc main() {\ndst := make([]byte, 100)\nprintln(ascii85.Encode(dst, []byte(\"hello\")))\nprintln(ascii85.MaxEncodedLen(5))\n}\n",
			[]string{"Encode", "MaxEncodedLen", "__ascii85Decode"},
			[]string{"Decode"},
		},
		{
			"encoding/pem",
			"package main\nimport \"encoding/pem\"\nfunc main() {\nblk := &pem.Block{Type: \"T\", Bytes: []byte(\"hi\")}\nprintln(len(pem.EncodeToMemory(blk)))\n}\n",
			// pem implies base64 (synthetic->synthetic dep): base64's
			// funcdecls must be present too.
			[]string{"EncodeToMemory", "__pemDecode", "Encode", "EncodeToString", "__base64Decode"},
			[]string{"Decode"},
		},
		{
			"encoding/asn1",
			"package main\nimport \"encoding/asn1\"\nfunc main() {\nb, _ := asn1.Marshal(42)\nprintln(len(b))\nprintln(asn1.TagInteger)\n}\n",
			[]string{"__asn1Marshal"},
			// Gate-1 #2: Marshal is table-driven (M7), so skipped.
			// MarshalWithParams, Unmarshal, UnmarshalWithParams are NOT
			// table-driven; they are now pair-transformed (present in
			// wire doc) instead of skipped.
			[]string{"Marshal"},
		},
		{
			"encoding",
			"package main\nimport \"encoding\"\nfunc main() {\nvar m encoding.BinaryMarshaler\nprintln(m == nil)\n}\n",
			[]string{},
			[]string{},
		},
	}
	for _, c := range cases {
		out := exportEnccoreSrc(t, c.src)
		names := enccoreFuncdecls(t, out)
		for _, want := range c.present {
			if !names[want] {
				t.Errorf("%s: wire doc missing funcdecl %q", c.pkg, want)
			}
		}
		for _, absent := range c.absent {
			if names[absent] {
				t.Errorf("%s: wire doc must not contain multi-result funcdecl %q", c.pkg, absent)
			}
		}
	}
}

// TestEnccoreDesugarShapes verifies the Appendix M (mrM7) desugar emits
// a call to the __-helper plus positional field extractions, for a
// 2-target method call, a 2-target package-level call, and the 3-target
// ascii85.Decode.
func TestEnccoreDesugarShapes(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		helper string
		fields []string
	}{
		{
			"base64 method 2-target",
			"package main\nimport \"encoding/base64\"\nfunc main() {\nenc := base64.StdEncoding\ndst := make([]byte, 100)\nn, err := enc.Decode(dst, []byte(\"aGk=\"))\nprintln(n, err == nil)\n}\n",
			"__base64Decode", []string{"n", "err"},
		},
		{
			"hex package-level 2-target",
			"package main\nimport \"encoding/hex\"\nfunc main() {\nb, err := hex.DecodeString(\"6162\")\nprintln(len(b), err == nil)\n}\n",
			"__hexDecodeString", []string{"b", "err"},
		},
		{
			"binary Uvarint 2-target",
			"package main\nimport \"encoding/binary\"\nfunc main() {\nbuf := make([]byte, 10)\nbinary.PutUvarint(buf, 300)\nx, n := binary.Uvarint(buf)\nprintln(x, n)\n}\n",
			"__binaryUvarint", []string{"x", "n"},
		},
		{
			"ascii85 3-target",
			"package main\nimport \"encoding/ascii85\"\nfunc main() {\nsrc := make([]byte, 100)\nndst, nsrc, err := ascii85.Decode(make([]byte, 100), src, true)\nprintln(ndst, nsrc, err == nil)\n}\n",
			"__ascii85Decode", []string{"ndst", "nsrc", "err"},
		},
		{
			"pem 2-target non-error pair",
			"package main\nimport \"encoding/pem\"\nfunc main() {\nblk := &pem.Block{Type: \"T\", Bytes: []byte(\"hi\")}\nenc := pem.EncodeToMemory(blk)\np, rest := pem.Decode(enc)\nprintln(p.Type, len(rest))\n}\n",
			"__pemDecode", []string{"p", "rest"},
		},
		{
			"asn1 Marshal 2-target",
			"package main\nimport \"encoding/asn1\"\nfunc main() {\nb, err := asn1.Marshal(42)\nprintln(len(b), err == nil)\n}\n",
			"__asn1Marshal", []string{"b", "err"},
		},
		{
			"base32 AppendDecode 2-target = form",
			"package main\nimport \"encoding/base32\"\nfunc main() {\nenc := base32.StdEncoding\nvar a []byte\nvar aerr error\na, aerr = enc.AppendDecode(nil, []byte(\"JBSWY3DP\"))\nprintln(len(a), aerr == nil)\n}\n",
			"__base32AppendDecode", []string{"b", "err"},
		},
	}
	for _, c := range cases {
		out := exportEnccoreSrc(t, c.src)
		if !strings.Contains(out, c.helper) {
			t.Errorf("%s: wire doc missing helper call %q", c.name, c.helper)
		}
		for _, f := range c.fields {
			if !strings.Contains(out, `"field": "`+f+`"`) {
				t.Errorf("%s: wire doc missing field extraction %q", c.name, f)
			}
		}
	}
}

// TestEnccoreFailFast verifies the named M7 scope errors: multi-result
// members in non-desugar positions, and the members that need
// reflect/WS D (binary.Append, asn1.Unmarshal, MarshalWithParams).
// NOTE (P3-M8 D2): bare-statement position is a desugar position for
// table entries now (see concrete_multiresult_bare_test.go) — the
// hex.DecodeString / base64.Decode statement cases that used to live
// here moved there.
func TestEnccoreFailFast(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // required substring of the error
	}{
		{
			"binary.Append needs reflect (M7 multi-result fail-fast)",
			"package main\nimport \"encoding/binary\"\nfunc main() {\nbinary.Append(nil, binary.LittleEndian, uint16(1))\n}\n",
			"M7",
		},
		{
			"asn1.Unmarshal needs WS D pointer store",
			"package main\nimport \"encoding/asn1\"\nfunc main() {\nvar x int\nasn1.Unmarshal([]byte{0x02, 0x01, 0x2a}, &x)\n}\n",
			"M7",
		},
		{
			// P3-M8b: encoding.BinaryMarshaler.MarshalBinary via interface
			// now desugars through the m8IfaceMultiResultMethods
			// "hash"/"MarshalBinary" entry (structural match on name +
			// ([]byte, error) results). Export succeeds; the itab maps the
			// method to hash.__binaryMarshal for synthetic digests, and
			// any other dynamic type fails loud at runtime with
			// "missing method" (never silent). The old M7 export-time
			// fail-fast pin is superseded by the feature.
			"encoding.BinaryMarshaler.MarshalBinary via interface",
			"package main\nimport \"encoding\"\nfunc f(m encoding.BinaryMarshaler) {\nm.MarshalBinary()\n}\nfunc main() {}\n",
			"", // must EXPORT cleanly (no M7 error; dispatch is runtime)
		},
		{
			// A user method with the same name as a desugared synthetic
			// method must NOT be desugared: the classifier keys on the
			// declaring package + receiver type, not the name. (A
			// classifier keyed on name alone would look up
			// __base64Decode in package main and panic.)
			"user type with same method name is not desugared",
			"package main\ntype E struct{}\nfunc (e E) Decode(a []byte) int { return len(a) }\nfunc main() {\nvar e E\nprintln(e.Decode([]byte(\"hi\")))\n}\n",
			"", // must EXPORT cleanly (no M7 error, no desugar)
		},
	}
	for _, c := range cases {
		dir := t.TempDir()
		p := filepath.Join(dir, "p.go")
		if err := os.WriteFile(p, []byte(c.src), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := ExportFile(p)
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: should export cleanly, got: %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got: %v", c.name, c.want, err)
		}
	}
}

// TestEnccoreStringsImportFixed is the regression test for the
// m7-wenc-core fix: "strings" was in the WS G allow-list but missing
// from the importer-selection list, so any program importing "strings"
// failed type-checking with failImporter.
func TestEnccoreStringsImportFixed(t *testing.T) {
	out := exportEnccoreSrc(t, "package main\nimport \"strings\"\nfunc main() {\nprintln(strings.Contains(\"hello\", \"ell\"))\n}\n")
	if !strings.Contains(out, `"name": "Contains"`) {
		t.Errorf("strings.Contains call missing from wire doc")
	}
}
