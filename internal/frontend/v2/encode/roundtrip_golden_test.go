package encode

import (
	"bytes"
	"encoding/binary"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/parser"
)

// updateRoundTripGoldens regenerates the checked-in .bin goldens:
//
//	cd internal/frontend/v2 && go test ./encode/ -run TestRoundTripGoldens -update
//
// The goldens live in the Truffle test-resources tree so the Java
// V2RoundTripTest can load them from the classpath:
// internal/backend/truffle/src/test/resources/graalgo/frontend/v2/roundtrip/
var updateRoundTripGoldens = flag.Bool("update", false, "regenerate v2 round-trip golden .bin files")

// roundTripCorpus is the cross-language round-trip corpus: each program
// runs through the full Go pipeline (parser → checker → emitter → encode)
// and the resulting .bin is fed through Java NodeFactoryV2.buildProgram by
// V2RoundTripTest. The struct program comes first: it covers the
// named-struct selector path (review finding: selector on named struct
// types). The alias program pins the *types.Alias emitter case.
var roundTripCorpus = []struct{ name, src string }{
	{"struct", `package main

type Point struct {
	X int
	Y int
}

func main() {
	var p Point
	p.X = 3
	p.Y = 4
	println(p.X + p.Y)
}
`},
	{"alias", `package main

type MyInt = int

func main() {
	var x MyInt = 41
	println(x + 1)
}
`},
	{"hello", `package main

func main() {
	x := 42
	println(x)
}
`},
}

// roundTripGoldenDir resolves the checked-in golden directory from this
// test file's location: <repo>/internal/backend/truffle/src/test/resources/
// graalgo/frontend/v2/roundtrip. It fails loudly if the directory is
// missing (e.g. the repo layout moved) rather than silently writing
// elsewhere.
func roundTripGoldenDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile = <repo>/internal/frontend/v2/encode/roundtrip_golden_test.go
	d := thisFile
	for i := 0; i < 5; i++ {
		d = filepath.Dir(d)
	}
	repoRoot := d
	dir := filepath.Join(repoRoot, "internal", "backend", "truffle",
		"src", "test", "resources", "graalgo", "frontend", "v2", "roundtrip")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("golden dir missing: %s (repo layout moved?)", dir)
	}
	return dir
}

// encodeRoundTripSource runs one corpus program through the full Go v2
// pipeline and returns the binary document.
func encodeRoundTripSource(t *testing.T, src string) []byte {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "main.go")
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
	doc, err := emit.Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	bin, err := Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(bin) == 0 {
		t.Fatal("encoded output is empty")
	}
	return bin
}

// decodeRoundTripDoc decodes a golden .bin (past the magic+version header)
// with the test-only Go decoder and returns the top-level document map.
func decodeRoundTripDoc(t *testing.T, bin []byte) map[string]any {
	t.Helper()
	if string(bin[:len(formatMagic)]) != formatMagic {
		t.Fatalf("missing magic header: %x", bin[:min(4, len(bin))])
	}
	rest := bin[len(formatMagic):]
	ver, n := binary.Uvarint(rest)
	if n <= 0 {
		t.Fatal("truncated version varint")
	}
	if ver != formatVersion {
		t.Fatalf("version = %d, want %d", ver, formatVersion)
	}
	v, r := decodeTestValue(t, rest[n:])
	if len(r) != 0 {
		t.Fatalf("trailing bytes after document: %d", len(r))
	}
	doc, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("top-level document is %T, want map", v)
	}
	return doc
}

// TestRoundTripGoldens pins the Go side of the cross-language contract:
// the exact bytes the Java V2RoundTripTest consumes. Any pipeline change
// that alters the wire format fails here until the goldens are
// deliberately regenerated with -update (and the Java side reviewed).
func TestRoundTripGoldens(t *testing.T) {
	dir := roundTripGoldenDir(t)
	for _, c := range roundTripCorpus {
		t.Run(c.name, func(t *testing.T) {
			bin := encodeRoundTripSource(t, c.src)
			golden := filepath.Join(dir, c.name+".bin")
			if *updateRoundTripGoldens {
				if err := os.WriteFile(golden, bin, 0644); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s (%d bytes)", golden, len(bin))
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden: %v (regenerate with -update)", err)
			}
			if !bytes.Equal(bin, want) {
				t.Fatalf("golden mismatch for %q: got %d bytes, want %d; "+
					"if the pipeline change is deliberate, regenerate with -update "+
					"and review the Java side", c.name, len(bin), len(want))
			}
		})
	}
}

// roundTripTypes returns the decoded type-table entries of a document.
func roundTripTypes(t *testing.T, doc map[string]any) []any {
	t.Helper()
	raw, ok := doc["types"].([]any)
	if !ok {
		t.Fatalf("document types is %T, want list", doc["types"])
	}
	return raw
}

// TestRoundTripWireContract asserts structural properties of the decoded
// goldens that the Java buildProgram path depends on. In particular it
// pins the *types.Alias fix: an alias must resolve to its right-hand-side
// type on the wire, never to {"kind":"basic","class":"unknown"} (which
// made the Java backend throw).
func TestRoundTripWireContract(t *testing.T) {
	dir := roundTripGoldenDir(t)
	for _, c := range roundTripCorpus {
		t.Run(c.name, func(t *testing.T) {
			bin, err := os.ReadFile(filepath.Join(dir, c.name+".bin"))
			if err != nil {
				t.Fatalf("read golden: %v (run -update first)", err)
			}
			doc := decodeRoundTripDoc(t, bin)
			if doc["frontend"] != "v2" {
				t.Fatalf("frontend = %v, want v2", doc["frontend"])
			}
			funcs, ok := doc["funcs"].([]any)
			if !ok || len(funcs) == 0 {
				t.Fatalf("funcs has no entries")
			}
			// No type entry may carry class "unknown": that is the
			// signature of the missing-alias-case bug on the wire.
			for i, te := range roundTripTypes(t, doc) {
				entry, ok := te.(map[string]any)
				if !ok {
					t.Fatalf("types[%d] is %T, want map", i, te)
				}
				if entry["kind"] == "basic" {
					detail, _ := entry["detail"].(map[string]any)
					if detail["class"] == "unknown" {
						t.Fatalf("types[%d] has class unknown (alias not resolved?): %v", i, entry)
					}
				}
			}
		})
	}

	t.Run("struct/named-type-shape", func(t *testing.T) {
		bin, err := os.ReadFile(filepath.Join(dir, "struct.bin"))
		if err != nil {
			t.Fatalf("read golden: %v", err)
		}
		doc := decodeRoundTripDoc(t, bin)
		var sawNamed, sawStruct bool
		for _, te := range roundTripTypes(t, doc) {
			entry := te.(map[string]any)
			switch entry["kind"] {
			case "named":
				detail, _ := entry["detail"].(map[string]any)
				name, _ := detail["name"].(string)
				if name == "" {
					t.Fatal("named type entry has empty name")
				}
				sawNamed = true
			case "struct":
				detail, _ := entry["detail"].(map[string]any)
				fields, _ := detail["fields"].([]any)
				if len(fields) != 2 {
					t.Fatalf("struct has %d fields, want 2 (X, Y)", len(fields))
				}
				sawStruct = true
			}
		}
		if !sawNamed {
			t.Fatal("no named type entry (Point) on the wire")
		}
		if !sawStruct {
			t.Fatal("no struct type entry on the wire")
		}
	})

	t.Run("alias/resolves-to-rhs", func(t *testing.T) {
		bin, err := os.ReadFile(filepath.Join(dir, "alias.bin"))
		if err != nil {
			t.Fatalf("read golden: %v", err)
		}
		doc := decodeRoundTripDoc(t, bin)
		var sawInt bool
		for _, te := range roundTripTypes(t, doc) {
			entry := te.(map[string]any)
			if entry["kind"] != "basic" {
				continue
			}
			detail, _ := entry["detail"].(map[string]any)
			if detail["name"] == "int" && detail["class"] == "int" {
				sawInt = true
			}
		}
		if !sawInt {
			t.Fatal("alias MyInt did not resolve to basic/int on the wire")
		}
	})
}

// TestRoundTripObjectKinds pins the describeObject truthfulness fix on the
// wire: a referenced constant must be kind "const" and a conversion target
// kind "typename" — never the old catch-all "var". This program is Go-side
// only (no golden): the Java factory does not implement constants yet, so
// it is not part of the Java round-trip corpus.
func TestRoundTripObjectKinds(t *testing.T) {
	src := `package main

type Celsius float64

const offset = 2

func main() {
	var c Celsius = Celsius(21 + offset)
	println(c)
}
`
	bin := encodeRoundTripSource(t, src)
	doc := decodeRoundTripDoc(t, bin)
	raw, ok := doc["objects"].([]any)
	if !ok {
		t.Fatalf("document objects is %T, want list", doc["objects"])
	}
	kinds := map[string]string{}
	for i, oe := range raw {
		entry, ok := oe.(map[string]any)
		if !ok {
			t.Fatalf("objects[%d] is %T, want map", i, oe)
		}
		kind, _ := entry["kind"].(string)
		name, _ := entry["name"].(string)
		kinds[name] = kind
	}
	if kinds["offset"] != "const" {
		t.Fatalf("object offset has kind %q, want const", kinds["offset"])
	}
	if kinds["Celsius"] != "typename" {
		t.Fatalf("object Celsius has kind %q, want typename", kinds["Celsius"])
	}
}
