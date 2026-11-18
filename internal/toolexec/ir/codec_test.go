package ir

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the malformed vectors testdata/bad-*.gir")

// minimal is a small valid file with one function body that uses every
// body-level table (positions, locals, labels).
func minimal() *File {
	return &File{
		GoVersion: "go1.27", GOOS: "linux", GOARCH: "amd64",
		Pkg:     Package{Path: "example.com/m", Name: "m", Lang: "go1.27", Files: []string{"m.go"}},
		Types:   []TypeEntry{{Key: "int", Type: &BasicType{Kind: BasicKindInt}}, {Key: "func(int) int", Type: &SignatureType{Params: []TypeID{1}, Results: []TypeID{1}}}},
		Objects: []Object{{Name: "F", Kind: SymKindFunc, Type: 2, Fn: 0}},
		Init:    Init{VarInit: -1},
		Funcs:   []Func{{Name: "F", Sig: 2, File: "m.go", Line: 3, Objs: []uint32{0}}},
		Bodies: []*FuncBody{{
			Positions: []Pos{{File: "m.go", Line: 3, Col: 1, Len: 10}},
			Locals:    []Local{{Name: "x", Type: 1, Pos: 1, Flags: 1}},
			Labels:    []string{"L"},
			Params:    []uint32{0},
			Recv:      -1,
			Body: []Stmt{&LabeledStmt{Pos: 1, Label: 0, Stmt: &ReturnStmt{Pos: 1, Results: []Expr{
				&BinaryExpr{Type: 1, Pos: 1, Op: BinOpAdd, X: &LocalExpr{Type: 1, Local: 0}, Y: &ConstExpr{Type: 1, Value: ConstValue{Kind: ConstKindInt, Text: "1"}}},
			}}}},
		}},
	}
}

func mustEncode(t *testing.T, f *File) []byte {
	t.Helper()
	data, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMinimalRoundTrip(t *testing.T) {
	data := mustEncode(t, minimal())
	f, err := DecodeValidate(data)
	if err != nil {
		t.Fatal(err)
	}
	if again := mustEncode(t, f); !bytes.Equal(again, data) {
		t.Fatal("round trip changes the bytes")
	}
	b, _ := f.Body(0)
	ret := b.Body[0].(*LabeledStmt).Stmt.(*ReturnStmt)
	if ret.Results[0].(*BinaryExpr).Y.(*ConstExpr).Value.Text != "1" || b.Labels[0] != "L" {
		t.Errorf("decoded body = %+v", b)
	}
}

// badVectors are malformed files with the error each decoder must report.
// They are also written to testdata/bad-*.gir for the Java decoder.
func badVectors(t *testing.T) map[string]struct {
	data []byte
	want string
} {
	good := mustEncode(t, minimal())
	enc := func(mut func(f *File)) []byte {
		f := minimal()
		mut(f)
		return mustEncode(t, f)
	}
	deep := func(n int) Expr {
		var x Expr = &LocalExpr{Type: 1, Local: 0}
		for range n {
			x = &UnaryExpr{Type: 1, Op: UnOpNeg, X: x}
		}
		return x
	}
	patch := func(data []byte, old, new []byte) []byte {
		i := bytes.Index(data, old)
		if i < 0 {
			t.Fatalf("patch: %q not found", old)
		}
		return append(append(append([]byte{}, data[:i]...), new...), data[i+len(old):]...)
	}
	return map[string]struct {
		data []byte
		want string
	}{
		"magic":     {append([]byte("GIR0"), good[4:]...), "bad magic"},
		"version":   {append([]byte("GIR1\x63"), good[5:]...), "GIR version 99"},
		"hash":      {patch(good, SchemaHash[:], make([]byte, 8)), "schema hash mismatch"},
		"truncated": {good[:len(good)-3], "exceeds the input"},
		"trailing":  {append(append([]byte{}, good...), 0), "trailing bytes after the last section"},
		"local":     {enc(func(f *File) { f.Bodies[0].Params = []uint32{7} }), "local 7 out of range"},
		"label":     {enc(func(f *File) { f.Bodies[0].Body[0].(*LabeledStmt).Label = 3 }), "label 3 out of range"},
		"pos":       {enc(func(f *File) { f.Bodies[0].Locals[0].Pos = 9 }), "position 9 out of range"},
		"type":      {enc(func(f *File) { f.Objects[0].Type = 3 }), "type id 3 out of range"},
		"obj":       {enc(func(f *File) { f.Funcs[0].Objs = []uint32{4} }), "obj index 4 out of range"},
		"fnref":     {enc(func(f *File) { f.Objects[0].Fn = 5 }), "optional fn index 5 out of range"},
		"enum":      {enc(func(f *File) { f.Types[0].Type = &BasicType{Kind: 99} }), "bad BasicKind 99"},
		"depth":     {deepVector(func() []byte { return enc(func(f *File) { f.Bodies[0].Body = []Stmt{&ExprStmt{X: deep(MaxDepth)}} }) }), "nesting deeper than"},
		"bodyless":  {enc(func(f *File) { f.Funcs[0].Flags = 1 }), "bodyless flag disagrees"},
		"string0":   {patch(good, []byte("\x00\x04m.go"), []byte("\x01m\x03.go")), "string 0 must be the empty string"},
	}
}

// deepVector runs mk with the encoder's depth limit lifted.
func deepVector(mk func() []byte) []byte {
	defer func(d int) { encodeMaxDepth = d }(encodeMaxDepth)
	encodeMaxDepth = 1 << 30
	return mk()
}

func TestMalformedRejected(t *testing.T) {
	for name, v := range badVectors(t) {
		path := filepath.Join("testdata", "bad-"+name+".gir")
		if *update {
			if err := os.WriteFile(path, v.data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		onDisk, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run go test ./ir -run TestMalformedRejected -update)", err)
		}
		if !bytes.Equal(onDisk, v.data) {
			t.Errorf("%s differs from the generated vector; rerun with -update", path)
		}
		_, err = DecodeValidate(v.data)
		var fe *FormatError
		if err == nil || (!errors.As(err, &fe) && !strings.HasPrefix(err.Error(), "gir:")) || !strings.Contains(err.Error(), v.want) {
			t.Errorf("%s: err = %v, want %q", name, err, v.want)
		}
	}
}

func TestDepthLimitIsExact(t *testing.T) {
	f := minimal()
	var x Expr = &LocalExpr{Type: 1, Local: 0}
	for range MaxDepth - 2 { // ExprStmt and the innermost LocalExpr also count
		x = &UnaryExpr{Type: 1, Op: UnOpNeg, X: x}
	}
	f.Bodies[0].Body = []Stmt{&ExprStmt{X: x}}
	if _, err := DecodeValidate(mustEncode(t, f)); err != nil {
		t.Errorf("nesting of exactly MaxDepth rejected: %v", err)
	}
}

func TestVarints(t *testing.T) {
	for _, tc := range []struct {
		in   []byte
		want string
	}{
		{[]byte{0x80, 0x00}, "non-minimal"},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02}, "overflows"},
		{[]byte{0x80}, "truncated"},
	} {
		r := &Reader{data: tc.in}
		var err error
		func() {
			defer catch(&err)
			r.uvarint()
		}()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%x: err = %v, want %q", tc.in, err, tc.want)
		}
	}
	w := newWriter()
	for _, v := range []int64{0, 1, -1, 63, -64, 1 << 62, -1 << 63, 1<<63 - 1} {
		w.varint(v)
		r := &Reader{data: w.take()}
		if got := r.varint(); got != v {
			t.Errorf("varint %d -> %d", v, got)
		}
	}
	w.uvarint(1<<64 - 1)
	r := &Reader{data: w.take()}
	if got := r.uvarint(); got != 1<<64-1 {
		t.Errorf("uvarint max -> %d", got)
	}
}

// FuzzDecode: no input makes the decoder panic, and an accepted input is
// a fixed point of encode∘decode after one normalization.
func FuzzDecode(f *testing.F) {
	for _, pattern := range []string{"testdata/*.gir"} {
		files, _ := filepath.Glob(pattern)
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err == nil {
				f.Add(data)
			}
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		g, err := DecodeValidate(data)
		if err != nil {
			return
		}
		once, err := Encode(g)
		if err != nil {
			t.Fatalf("re-encode of an accepted file: %v", err)
		}
		h, err := DecodeValidate(once)
		if err != nil {
			t.Fatalf("re-encoded file rejected: %v", err)
		}
		twice, err := Encode(h)
		if err != nil || !bytes.Equal(once, twice) {
			t.Fatalf("encode∘decode is not idempotent")
		}
	})
}

// The encoder refuses nesting the decoders would reject (W2 encoded it and
// only the decoders failed — at run time, after the archive was cached).
func TestEncodeDepthLimit(t *testing.T) {
	mk := func(n int) *File {
		var x Expr = &ConstExpr{Type: 1, Value: ConstValue{Kind: ConstKindInt, Text: "1"}}
		for range n {
			x = &UnaryExpr{Type: 1, Op: UnOpNeg, X: x}
		}
		return &File{Pkg: Package{Path: "p"}, Types: []TypeEntry{{Key: "int", Type: &BasicType{Kind: BasicKindInt}}},
			Funcs: []Func{{Name: "f"}}, Bodies: []*FuncBody{{Recv: -1, Body: []Stmt{&ExprStmt{X: x}}}}}
	}
	// The ExprStmt is one level: n unary nodes + the const + the stmt.
	ok := mk(MaxDepth - 2)
	data, err := Encode(ok)
	if err != nil {
		t.Fatalf("depth %d: %v", MaxDepth, err)
	}
	if _, err := DecodeValidate(data); err != nil {
		t.Fatalf("decoders reject what the encoder accepts: %v", err)
	}
	if _, err := Encode(mk(MaxDepth - 1)); err == nil {
		t.Fatal("encoder accepts nesting deeper than MaxDepth")
	}
}
