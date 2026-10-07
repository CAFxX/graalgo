package p3frontend

// P3-M7 (wave 1): p3frontend drift tests for the synthetic "encoding/csv"
// and "encoding/gob" packages. The importer reimplements the frozen
// exporter's synthetic signatures (it cannot reuse them — unexported);
// these tests pin the mirror against the REAL phase-1 synthetic source
// so drift fails fast.

import (
	"go/types"
	"testing"
)

// TestCsvSyntheticSignatures pins the mirrored synthetic csv package.
func TestCsvSyntheticSignatures(t *testing.T) {
	pkg := syntheticCsvPackage()
	if pkg.Name() != "csv" || pkg.Path() != "encoding/csv" {
		t.Fatalf("package name/path = %s/%s, want csv/encoding/csv", pkg.Name(), pkg.Path())
	}

	// Reader type with fields.
	rObj := pkg.Scope().Lookup("Reader")
	if rObj == nil {
		t.Fatal("csv.Reader missing")
	}
	rNamed, ok := rObj.Type().(*types.Named)
	if !ok {
		t.Fatalf("csv.Reader is %T, want *types.Named", rObj.Type())
	}
	// Reader methods.
	for _, name := range []string{"Read", "ReadAll", "FieldPos", "InputOffset"} {
		found := false
		for i := 0; i < rNamed.NumMethods(); i++ {
			if rNamed.Method(i).Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("csv.Reader.%s missing", name)
		}
	}

	// Writer type with methods.
	wObj := pkg.Scope().Lookup("Writer")
	if wObj == nil {
		t.Fatal("csv.Writer missing")
	}
	wNamed, ok := wObj.Type().(*types.Named)
	if !ok {
		t.Fatalf("csv.Writer is %T, want *types.Named", wObj.Type())
	}
	for _, name := range []string{"Write", "WriteAll", "Flush", "Error", "Bytes"} {
		found := false
		for i := 0; i < wNamed.NumMethods(); i++ {
			if wNamed.Method(i).Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("csv.Writer.%s missing", name)
		}
	}

	// ParseError type.
	if pkg.Scope().Lookup("ParseError") == nil {
		t.Error("csv.ParseError missing")
	}

	// Package-level functions.
	for _, name := range []string{"NewReaderString", "NewBufferWriter"} {
		if pkg.Scope().Lookup(name) == nil {
			t.Errorf("csv.%s missing", name)
		}
	}
}

// TestGobSyntheticSignatures pins the mirrored synthetic gob package.
func TestGobSyntheticSignatures(t *testing.T) {
	pkg := syntheticGobPackage()
	if pkg.Name() != "gob" || pkg.Path() != "encoding/gob" {
		t.Fatalf("package name/path = %s/%s, want gob/encoding/gob", pkg.Name(), pkg.Path())
	}

	// Encoder type with methods.
	encObj := pkg.Scope().Lookup("Encoder")
	if encObj == nil {
		t.Fatal("gob.Encoder missing")
	}
	encNamed, ok := encObj.Type().(*types.Named)
	if !ok {
		t.Fatalf("gob.Encoder is %T, want *types.Named", encObj.Type())
	}
	for _, name := range []string{"Encode", "Bytes"} {
		found := false
		for i := 0; i < encNamed.NumMethods(); i++ {
			if encNamed.Method(i).Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("gob.Encoder.%s missing", name)
		}
	}

	// Decoder type with Decode method.
	decObj := pkg.Scope().Lookup("Decoder")
	if decObj == nil {
		t.Fatal("gob.Decoder missing")
	}
	decNamed, ok := decObj.Type().(*types.Named)
	if !ok {
		t.Fatalf("gob.Decoder is %T, want *types.Named", decObj.Type())
	}
	found := false
	for i := 0; i < decNamed.NumMethods(); i++ {
		if decNamed.Method(i).Name() == "Decode" {
			found = true
			break
		}
	}
	if !found {
		t.Error("gob.Decoder.Decode missing")
	}

	// Package-level functions.
	for _, name := range []string{"NewBufferEncoder", "NewBufferDecoder", "Register", "RegisterName"} {
		if pkg.Scope().Lookup(name) == nil {
			t.Errorf("gob.%s missing", name)
		}
	}
}
