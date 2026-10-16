package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyntheticBytesM7 verifies the P3-M7 wave-1 synthetic bytes package:
// a program importing "bytes" exports cleanly; the real-Go declarations
// become funcdecls in the wire doc; the multi-result members (Cut,
// Buffer.Read, Reader.Seek, ...) are skipped at export (WS D — call sites
// fail fast with the named M7 scope error).
func TestSyntheticBytesM7(t *testing.T) {
	src := `package main

import "bytes"

func main() {
	println(bytes.Contains([]byte("hello"), []byte("ell")))
	println(bytes.Count([]byte("aaa"), []byte("a")))
	println(string(bytes.ToUpper([]byte("abc"))))
	println(string(bytes.TrimSpace([]byte("  x  "))))
	println(bytes.EqualFold([]byte("ABC"), []byte("abc")))
	parts := bytes.Split([]byte("a,b"), []byte(","))
	println(len(parts), string(parts[0]))
	println(string(bytes.Join([][]byte{[]byte("a"), []byte("b")}, []byte("-"))))
	println(string(bytes.Repeat([]byte("ab"), 2)))
	b := bytes.NewBufferString("xy")
	println(b.String(), b.Len())
	_ = b.WriteByte('z')
	println(b.Len())
	println(string(b.Next(2)))
	r := bytes.NewReader([]byte("abc"))
	println(r.Len(), r.Size())
	r.Reset([]byte("z"))
	println(r.Len())
	println(bytes.MinRead)
	println(bytes.ErrTooLarge.Error())
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synbytes.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	// bytesNames attributes funcdecls to the synthetic bytes package by
	// wire-doc pkgpath. The doc carries the whole transitive dependency
	// closure (see the absent-check below), so bare names alone cannot
	// attribute a declaration to bytes.
	bytesNames := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						names[n] = true
						if p, ok := f["pkgpath"].(string); ok && p == "bytes" {
							bytesNames[n] = true
						}
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
	// Real-Go declarations must be exported as funcdecls (bare names —
	// the wire doc does not qualify synthetic members).
	for _, want := range []string{
		"Contains", "Count", "ToUpper", "TrimSpace", "EqualFold",
		"Split", "Join", "Repeat", "NewBufferString", "String",
		"WriteByte", "Next", "NewReader", "Len", "Size", "Reset",
		"IndexRune", "Map", "Fields", "Clone",
	} {
		if !names[want] {
			t.Errorf("wire doc missing funcdecl %q", want)
		}
	}
	// Multi-result declarations of the bytes package are skipped at export
	// (WS D). They are checked by package, not by bare name: the wire doc
	// now contains the whole transitive real-stdlib closure
	// (fix-disk-sync-methods: synthetic io merges the real sync package,
	// which pulls in internal/race — whose norace build legitimately
	// exports no-op Read/Write funcdecls that sync's bodies call). A
	// bare-name check false-positives on those; the M7 contract under test
	// is that bytes' own multi-result members are never emitted, with call
	// sites failing fast with the named M7 scope error (see
	// TestSyntheticBytesCutFailsFast).
	// Note: Cut, CutPrefix, CutSuffix, CutLast now have real implementations
	// and are pair-transformed via the general userMRFuncs mechanism.
	for _, absent := range []string{
		"Read", "Write", "ReadByte", "ReadRune", "ReadBytes", "ReadString",
		"Peek", "WriteString", "WriteRune", "ReadFrom", "WriteTo", "Seek", "ReadAt",
	} {
		if bytesNames[absent] {
			t.Errorf("wire doc must not contain multi-result funcdecl %q from package bytes", absent)
		}
	}
}

// TestSyntheticBytesCutFailsFast verifies user calls to multi-result
// bytes members fail with the named M7 scope error.
// Note: Cut, CutPrefix, CutSuffix, CutLast now work via the general
// userMRFuncs mechanism and are not in this test.
func TestSyntheticBytesCutFailsFast(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"Buffer.Read", `package main

import "bytes"

func main() {
	b := bytes.NewBufferString("hi")
	p := make([]byte, 4)
	n, _ := b.Read(p)
	println(n)
}
`, "bytes.Buffer.Read"},
		{"Reader.Seek", `package main

import "bytes"

func main() {
	r := bytes.NewReader([]byte("hi"))
	_, _ = r.Seek(0, 0)
}
`, "bytes.Reader.Seek"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "synfail.go")
			if err := os.WriteFile(p, []byte(c.src), 0644); err != nil {
				t.Fatal(err)
			}
			_, err := ExportFile(p)
			if err == nil {
				t.Fatalf("ExportFile succeeded for %s call, want M7 scope error", c.want)
			}
			if !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "M7 scope") {
				t.Fatalf("unexpected error for %s call: %v", c.want, err)
			}
		})
	}
}
