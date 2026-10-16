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
	// pkgPaths records the declaring package path per bare funcdecl name,
	// so the multi-result absence check below can be scoped to the bytes
	// package: transitively imported real packages (e.g. internal/race,
	// via real sync) legitimately contribute same-named single-result
	// funcdecls (internal/race.Read/Write), which must not trip the check.
	pkgPaths := map[string]string{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						names[n] = true
						if pp, ok := f["pkgpath"].(string); ok {
							pkgPaths[n] = pp
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
	// (WS D). The check is scoped to pkgpath "bytes": transitively
	// imported real packages legitimately export same-named funcdecls
	// (internal/race.Read/Write are single-result; they must not trip it).
	// Note: Cut, CutPrefix, CutSuffix, CutLast now have real implementations
	// and are pair-transformed via the general userMRFuncs mechanism.
	for _, absent := range []string{
		"Read", "Write", "ReadByte", "ReadRune", "ReadBytes", "ReadString",
		"Peek", "WriteString", "WriteRune", "ReadFrom", "WriteTo", "Seek", "ReadAt",
	} {
		if names[absent] && pkgPaths[absent] == "bytes" {
			t.Errorf("wire doc must not contain multi-result funcdecl %q", absent)
		}
	}
}

// TestSyntheticBytesCutFailsFast verifies user calls to multi-result
// bytes members fail with the named M7 scope error.
// Note: Cut, CutPrefix, CutSuffix, CutLast now work via the general
// userMRFuncs mechanism and are not in this test.
// Note: Buffer.Read was retired from this test — its M7 fail-fast stub's
// "re-test when WS D lands" condition is met, so it is now table-driven
// via __bytesBufferRead (see TestSyntheticBytesBufferReadDesugar).
func TestSyntheticBytesCutFailsFast(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
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

// TestSyntheticBytesBufferReadDesugar verifies the retired M7 fail-fast
// stub for (*bytes.Buffer).Read: the m7 table now drives it through the
// real __bytesBufferRead helper (gc 1.27.1 semantics), and the
// bytes.Buffer method table maps Read to that helper so interface
// dispatch (e.g. io.ReadAll over a *bytes.Buffer) resolves.
//
// Red-green: pre-fix, the direct call below fails with the named M7 scope
// error, the wire has no __bytesBufferRead funcdecl, and the bytes.Buffer
// type-table entry carries no methods.
func TestSyntheticBytesBufferReadDesugar(t *testing.T) {
	src := `package main

import (
	"bytes"
	"io"
)

func main() {
	b := bytes.NewBufferString("hi")
	p := make([]byte, 4)
	n, _ := b.Read(p)
	println(n)
	var r io.Reader = b
	m, _ := r.Read(p)
	println(m)
}
`
	doc := decodeExport(t, mustExport(t, src))

	// The helper funcdecl is emitted.
	helperObj := -1
	for _, f := range findAllKind(doc, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		if fl["name"] == "__bytesBufferRead" {
			o, _ := fl["obj"].(float64)
			helperObj = int(o)
		}
	}
	if helperObj < 0 {
		t.Fatal("wire doc missing funcdecl __bytesBufferRead")
	}

	// The *bytes.Buffer pointer entry maps Read to the helper's obj id.
	// (Pointer entries carry the method set; the named bytes.Buffer
	// entry's value method set is empty — all its methods have pointer
	// receivers. Pointer entries have no "name" field; they are found
	// via "elem".)
	bufID := -1
	for i, te := range doc["types"].([]any) {
		tm, _ := te.(map[string]any)
		if tm["kind"] != "named" {
			continue
		}
		det, _ := tm["detail"].(map[string]any)
		if det["name"] == "bytes.Buffer" {
			bufID = i
		}
	}
	if bufID < 0 {
		t.Fatal("no named bytes.Buffer type-table entry")
	}
	found := false
	for _, te := range doc["types"].([]any) {
		tm, _ := te.(map[string]any)
		if tm["kind"] != "pointer" {
			continue
		}
		det, _ := tm["detail"].(map[string]any)
		elem, _ := det["elem"].(float64)
		if int(elem) != bufID {
			continue
		}
		for _, m := range det["methods"].([]any) {
			mm, _ := m.(map[string]any)
			if mm["name"] == "Read" {
				found = true
				o, _ := mm["obj"].(float64)
				if int(o) != helperObj {
					t.Fatalf("*bytes.Buffer Read obj %d, want __bytesBufferRead obj %d", int(o), helperObj)
				}
			}
		}
	}
	if !found {
		t.Fatal("*bytes.Buffer type-table entry has no Read method")
	}

	// The interface call desugars to an ifacemethod dispatch node (not a
	// fail-fast): the itab target above is what it resolves to at runtime.
	ifacemethods := findAllKind(doc, "ifacemethod")
	sawRead := false
	for _, im := range ifacemethods {
		fl, _ := im["fields"].(map[string]any)
		if fl["method"] == "Read" {
			sawRead = true
		}
	}
	if !sawRead {
		t.Fatal("no ifacemethod dispatch node for the r.Read(p) interface call")
	}
}
