package schema

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The checked-in generated codecs (Go and Java) are exactly what
// schema.txt produces: a field added on one side cannot be missed on the
// other (design §3.2).
func TestGeneratedUpToDate(t *testing.T) {
	outs, err := Outputs("..")
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range outs {
		got, err := os.ReadFile(filepath.Join("..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale: run `go generate ./ir` in internal/toolexec", rel)
		}
	}
}

func TestSchemaErrors(t *testing.T) {
	base := "version 1\nmaxdepth 10\nrecord FuncBody scope positions:[]Pos locals:[]uint labels:[]str body:[]uint\nrecord Pos file:str\n" +
		"record L x:uint\nsection 1 STRINGS strings raw\nsection 3 TYPES types []L\nsection 4 SYMREFS symRefs []L\n" +
		"section 5 OBJECTS objects []L\nsection 6 FUNCINDEX funcs []L\nsection 7 BODIES bodies raw\nsection 8 DATA data raw\n"
	if _, err := Parse([]byte(base)); err != nil {
		t.Fatalf("base schema: %v", err)
	}
	for _, tc := range []struct{ add, want string }{
		{"record L y:uint\n", "declared twice"},
		{"record M x:Nope\n", "undefined type Nope"},
		{"union U\n  1 A x:uint\n  1 B y:uint\n", "duplicate kind"},
		{"union U\n  1 A unionTag:uint\n", "reserved"},
		{"enum E\n  0 Zero\n", "bad or duplicate value"},
		{"record M x:type?\n", "already optional"},
		{"section 2 PKG pkg L\n", "ids must ascend"},
		{"bogus\n", "unknown declaration"},
	} {
		_, err := Parse([]byte(base + tc.add))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want %q", tc.add, err, tc.want)
		}
	}
	// The hash covers content, not formatting or comments.
	a, _ := Parse([]byte(base))
	b, _ := Parse([]byte("# comment\n" + strings.ReplaceAll(base, " x:uint", "   x:uint   # c")))
	c, _ := Parse([]byte(strings.ReplaceAll(base, " x:uint", " x:int")))
	if a.Hash != b.Hash || a.Hash == c.Hash {
		t.Error("schema hash must ignore formatting and track content")
	}
}
