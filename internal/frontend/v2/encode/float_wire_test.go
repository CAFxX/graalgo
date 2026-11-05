// Wire tests for the P2-19 encode fixes (review-2026-11-05-13,
// A-P2-9 in the review numbering): float64/float32 values must render
// in spellings Java's Double/Float.parseDouble accepts, and Encode must
// carry every Document field (built from the struct, not a literal).
package encode

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"graalgo-frontend-v2/emit"
)

func TestEncodeFloatJavaParseable(t *testing.T) {
	// value → exact wire string. The spellings are the contract with
	// the Java decoder (BinaryDecoder → Double/Float.parseDouble):
	// "Infinity"/"-Infinity"/"NaN" parse; Go's "+Inf"/"-Inf" do not.
	// Finite values use strconv.FormatFloat 'g' (shortest round-trip).
	cases := []struct {
		name string
		val  any
		want string
	}{
		{"float64_pi", float64(3.14), "3.14"},
		{"float64_neg_zero", math.Copysign(0, -1), "-0"},
		{"float64_big", float64(1e21), "1e+21"},
		{"float64_small", float64(1e-7), "1e-07"},
		{"float64_pos_inf", math.Inf(1), "Infinity"},
		{"float64_neg_inf", math.Inf(-1), "-Infinity"},
		{"float64_nan", math.NaN(), "NaN"},
		{"float32_half", float32(1.5), "1.5"},
		{"float32_pos_inf", float32(math.Inf(1)), "Infinity"},
		{"float32_neg_inf", float32(math.Inf(-1)), "-Infinity"},
		{"float32_nan", float32(math.NaN()), "NaN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := encodeValue(&buf, c.val); err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, rest := decodeTestValue(t, buf.Bytes())
			if len(rest) != 0 {
				t.Fatalf("%d trailing bytes after decode", len(rest))
			}
			s, ok := got.(string)
			if !ok {
				t.Fatalf("decoded %T, want string", got)
			}
			if s != c.want {
				t.Errorf("wire string = %q, want %q", s, c.want)
			}
			// The string must not contain a Go-only spelling, and must
			// parse back to the same value on the Go side (a necessary
			// condition for the Java parse to agree).
			if s == "+Inf" || s == "-Inf" {
				t.Errorf("wire string %q uses a Go-only Inf spelling Java cannot parse", s)
			}
			isNaN := false
			switch v := c.val.(type) {
			case float64:
				isNaN = math.IsNaN(v)
			case float32:
				isNaN = math.IsNaN(float64(v))
			}
			if isNaN {
				if s != "NaN" {
					t.Errorf("NaN wire string = %q, want \"NaN\"", s)
				}
				return
			}
			switch v := c.val.(type) {
			case float64:
				back, err := strconv.ParseFloat(s, 64)
				if err != nil {
					t.Fatalf("wire string %q does not parse as float64: %v", s, err)
				}
				if back != v {
					t.Errorf("round-trip: %q parses to %v, want %v", s, back, v)
				}
			case float32:
				back, err := strconv.ParseFloat(s, 32)
				if err != nil {
					t.Fatalf("wire string %q does not parse as float32: %v", s, err)
				}
				if float32(back) != v {
					t.Errorf("round-trip: %q parses to %v, want %v", s, float32(back), v)
				}
			}
		})
	}
}

func TestEncodeDocumentMapCoversAllFields(t *testing.T) {
	// documentMap must expose exactly the Document struct's exported
	// fields (lower-cased) — a future field must reach the wire without
	// anyone remembering to add it to a literal.
	doc := &emit.Document{
		Frontend:  "v2",
		Generator: map[string]any{"tool": "gofrontend-v2"},
		Package:   map[string]any{"name": "main"},
		Types:     []any{"t"},
		Objects:   []any{"o"},
		Funcs:     []any{"f"},
		Globals:   []any{"g"},
	}
	m := documentMap(doc)
	dt := reflect.TypeOf(emit.Document{})
	wantKeys := map[string]bool{}
	for i := 0; i < dt.NumField(); i++ {
		f := dt.Field(i)
		if f.PkgPath != "" {
			continue
		}
		wantKeys[strings.ToLower(f.Name)] = true
	}
	if len(m) != len(wantKeys) {
		t.Fatalf("documentMap has %d keys %v, want %d struct fields %v",
			len(m), keysOf(m), len(wantKeys), keysOf(wantKeys))
	}
	for k := range wantKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("documentMap is missing struct field %q", k)
		}
	}
	// The full Encode path must carry the same keys, with values intact.
	bin, err := Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Strip magic + version, then decode the top-level map.
	rest := bin[len(formatMagic):]
	_, n := binary.Uvarint(rest)
	if n <= 0 {
		t.Fatalf("bad version header: %x", rest)
	}
	rest = rest[n:]
	got, tail := decodeTestValue(t, rest)
	if len(tail) != 0 {
		t.Fatalf("%d trailing bytes after top-level map", len(tail))
	}
	top, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("top-level value is %T, want map", got)
	}
	for k := range wantKeys {
		if _, ok := top[k]; !ok {
			t.Errorf("encoded document is missing top-level key %q", k)
		}
	}
	if top["frontend"] != "v2" {
		t.Errorf("frontend = %#v, want \"v2\"", top["frontend"])
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
