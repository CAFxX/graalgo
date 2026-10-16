package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// TestBasicClassWireContract pins the (name, class) pairs the emitter puts on
// every basic type descriptor against the vocabulary the Java backend
// accepts in GoKind.fromBasic
// (internal/backend/truffle/src/main/java/graalgo/values/GoKind.java).
//
// Regression test for V2-1: basicClass used to mask b.Info() with
// types.IsNumeric, which (in go1.27's go/types) is IsInteger|IsFloat|IsComplex
// — it contains neither IsUnsigned nor IsBoolean. The "uint" case was dead
// code (every unsigned type emitted class "int") and bool emitted class
// "unknown"; GoKind.fromBasic throws on both, so ALL unsigned and boolean
// code failed end-to-end.
//
// Keep this table in sync with GoKind.fromBasic: any (name, class) pair the
// emitter produces must be accepted there, and vice versa any pair the
// factory accepts for a basic type should be reachable from the emitter.
func TestBasicClassWireContract(t *testing.T) {
	// spell -> wantClass. Every basic type in the go/types universe plus the
	// byte/rune aliases, plus untyped constants (const decls force the
	// untyped basic into the type table).
	cases := []struct {
		spell string
		class string
	}{
		{"bool", "bool"},
		{"int", "int"},
		{"int8", "int"},
		{"int16", "int"},
		{"int32", "int"},
		{"int64", "int"},
		{"rune", "int"}, // alias for int32; Java accepts "rune" under "int"
		{"uint", "uint"},
		{"uint8", "uint"},
		{"uint16", "uint"},
		{"uint32", "uint"},
		{"uint64", "uint"},
		{"uintptr", "uint"},
		{"byte", "uint"}, // alias for uint8; Java accepts "byte" under "uint"
		{"float32", "float"},
		{"float64", "float"},
		{"complex64", "complex"},
		{"complex128", "complex"},
		{"string", "string"},
	}

	src := "package main\n\nfunc sink("
	for i, c := range cases {
		if i > 0 {
			src += ", "
		}
		src += "p" + itoa(i) + " " + c.spell
	}
	src += ") {}\n"
	// Untyped constants: the emitter must give them the class of their
	// family, because GoKind.fromBasic only accepts untyped names ("untyped
	// int", "untyped bool", ...) under the family class — there is no
	// "untyped" class on the Java side.
	src += `
const cuInt = 42
const cuBool = true
const cuFloat = 1.5
const cuComplex = 1 + 2i
const cuString = "s"
const cuRune = 'a'

func main() {
	sink(false, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, "")
	_, _, _, _, _, _ = cuInt, cuBool, cuFloat, cuComplex, cuString, cuRune
}
`

	dir := t.TempDir()
	fn := filepath.Join(dir, "wire.go")
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

	got := map[string]string{} // emitted basic-type name -> class
	for _, td := range doc.Types {
		m, ok := td.(map[string]any)
		if !ok || m["kind"] != "basic" {
			continue
		}
		d := m["detail"].(map[string]any)
		name, _ := d["name"].(string)
		class, _ := d["class"].(string)
		got[name] = class
	}

	// Every spelling must appear with the Java-accepted class.
	for _, c := range cases {
		class, ok := got[c.spell]
		if !ok {
			t.Errorf("type %q: no basic descriptor emitted", c.spell)
			continue
		}
		if class != c.class {
			t.Errorf("type %q: emitted class %q, want %q (GoKind.fromBasic rejects %q)",
				c.spell, class, c.class, class)
		}
	}

	// Untyped constants ride the family class.
	untyped := map[string]string{
		"untyped int":     "int",
		"untyped bool":    "bool",
		"untyped float":   "float",
		"untyped complex": "complex",
		"untyped string":  "string",
		"untyped rune":    "int",
	}
	for name, wantClass := range untyped {
		class, ok := got[name]
		if !ok {
			t.Errorf("type %q: no basic descriptor emitted", name)
			continue
		}
		if class != wantClass {
			t.Errorf("type %q: emitted class %q, want %q (GoKind.fromBasic has no %q class)",
				name, class, wantClass, class)
		}
	}

	// No KNOWN basic descriptor may carry a class the Java factory cannot
	// map. (GoKind.fromBasic switches on exactly these classes. Stray
	// descriptors such as the "()" tuple from the default branch are a
	// separate quirk, out of scope here.)
	known := map[string]bool{}
	for _, c := range cases {
		known[c.spell] = true
	}
	for name := range untyped {
		known[name] = true
	}
	javaClasses := map[string]bool{
		"int": true, "uint": true, "float": true, "complex": true,
		"bool": true, "string": true, "pointer": true,
	}
	for name, class := range got {
		if known[name] && !javaClasses[class] {
			t.Errorf("type %q: emitted class %q is not accepted by GoKind.fromBasic", name, class)
		}
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return digits[i : i+1]
	}
	return itoa(i/10) + digits[i%10:i%10+1]
}
