package encode

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/parser"
)

func TestEncodeHelloWorld(t *testing.T) {
	src := `package main

func main() {
	x := 42
	println(x)
}
`
	dir := t.TempDir()
	fn := filepath.Join(dir, "hello.go")
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

	// Encode to binary.
	bin, err := Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(bin) == 0 {
		t.Fatal("encoded output is empty")
	}
	t.Logf("encoded %d bytes", len(bin))

	// The output must start with the magic + version header, then the
	// top-level map tag. This is the real "not JSON" check: a JSON
	// encoding would start with '{', ours starts with "g2bi".
	if !strings.HasPrefix(string(bin), formatMagic) {
		t.Fatalf("missing magic header: first bytes = %x", bin[:min(4, len(bin))])
	}
	rest := bin[len(formatMagic):]
	ver, n := binary.Uvarint(rest)
	if n <= 0 {
		t.Fatalf("truncated version header: %x", rest[:min(4, len(rest))])
	}
	if ver != formatVersion {
		t.Fatalf("version = %d, want %d", ver, formatVersion)
	}
	if rest[n] != tagMap {
		t.Fatalf("first value tag = 0x%02x, want map tag 0x%02x", rest[n], tagMap)
	}
}

func TestEncodeRoundTripTypes(t *testing.T) {
	// Exercise all supported Go types through the encoder, then decode
	// with the test-only decoder below and compare. This is the Go-side
	// round-trip: it would have caught R5-P1-1 (unsigned widths written
	// raw while the decoder un-zigzags).
	m := map[string]any{
		"null":   nil,
		"bool_t": true,
		"bool_f": false,
		"int":    42,
		"neg":    -17,
		"int64":  int64(1 << 60),
		"u64big": uint64(1) << 63,
		"u64max": uint64(math.MaxUint64),
		"str":    "hello",
		"list":   []any{1, "two", nil},
		"nested": map[string]any{"a": 1},
	}
	var buf bytes.Buffer
	if err := encodeValue(&buf, m); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, rest := decodeTestValue(t, buf.Bytes())
	if len(rest) != 0 {
		t.Fatalf("%d trailing bytes after decode", len(rest))
	}
	// Wire integers are 64-bit patterns; compare by pattern.
	want := map[string]any{
		"null":   nil,
		"bool_t": true,
		"bool_f": false,
		"int":    intPattern(42),
		"neg":    intPattern(-17),
		"int64":  intPattern(int64(1 << 60)),
		"u64big": intPattern(uint64(1) << 63),
		"u64max": intPattern(uint64(math.MaxUint64)),
		"str":    "hello",
		"list":   []any{intPattern(1), "two", nil},
		"nested": map[string]any{"a": intPattern(1)},
	}
	if !testValuesEqual(got, want) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, want)
	}
}

func TestEncodeIntContract(t *testing.T) {
	// Byte-exact wire contract for TAG_INT. EVERY integer width must be
	// zigzag-encoded: the Java decoder un-zigzags every TAG_INT
	// unconditionally. (R5-P1-1: unsigned widths were written raw and
	// silently misdecoded, e.g. uint64(5) → -3, uint64(1<<63) →
	// 4611686018427387904.)
	cases := []struct {
		name string
		val  any
		want string // hex of tag + varint payload
	}{
		{"int_zero", int(0), "0200"},
		{"int_one", int(1), "0202"},
		{"int_neg1", int(-1), "0201"},
		{"int_42", int(42), "0254"},
		{"int_neg17", int(-17), "0221"},
		{"int8_min", int8(math.MinInt8), "02ff01"},
		{"int16_neg1000", int16(-1000), "02cf0f"},
		{"int32_min", int32(math.MinInt32), "02ffffffff0f"},
		{"int64_min", int64(math.MinInt64), "02ffffffffffffffffff01"},
		{"int64_max", int64(math.MaxInt64), "02feffffffffffffffff01"},
		{"uint_zero", uint(0), "0200"},
		{"uint8_max", uint8(math.MaxUint8), "02fe03"},
		{"uint16_max", uint16(math.MaxUint16), "02feff07"},
		{"uint32_max", uint32(math.MaxUint32), "02feffffff1f"},
		{"uint64_5", uint64(5), "020a"},
		{"uint64_1shift63", uint64(1) << 63, "02ffffffffffffffffff01"},
		{"uint64_max", uint64(math.MaxUint64), "0201"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := encodeValue(&buf, c.val); err != nil {
			t.Fatalf("%s: encode: %v", c.name, err)
		}
		if got := hex.EncodeToString(buf.Bytes()); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestEncodeGoldenDoc(t *testing.T) {
	// Byte-exact golden document: magic + version + one map exercising
	// every value kind, including the R5-P1-1 regression widths. The hex
	// is mirrored by BinaryDecoderTest on the Java side (checked-in
	// cross-language golden); keep the two in lockstep.
	doc := map[string]any{
		"neg": int64(-5),
		"big": uint64(1) << 63,
		"s":   "hi",
		"l":   []any{1, "two"},
	}
	var buf bytes.Buffer
	buf.WriteString(formatMagic)
	writeVarint(&buf, formatVersion)
	if err := encodeValue(&buf, doc); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := hex.EncodeToString(buf.Bytes())
	t.Logf("golden doc hex: %s", got)
	// Keys sort: "big" < "l" < "neg" < "s".
	want := "67326269" + // magic "g2bi"
		"01" + // version 1
		"0504" + // map, 4 entries
		"03626967" + "02ffffffffffffffffff01" + // "big": uint64(1)<<63 → zigzag 0xFF..FF
		"016c" + "0402" + "0202" + "030374776f" + // "l": [1, "two"]
		"036e6567" + "0209" + // "neg": int64(-5) → zigzag 9
		"0173" + "03026869" // "s": "hi"
	if got != want {
		t.Errorf("golden doc mismatch:\n got %s\nwant %s", got, want)
	}
}

// intPattern returns the 64-bit pattern of any Go integer width, for
// comparing decoded wire values.
func intPattern(v any) uint64 {
	switch x := v.(type) {
	case int:
		return uint64(int64(x))
	case int8:
		return uint64(int64(x))
	case int16:
		return uint64(int64(x))
	case int32:
		return uint64(int64(x))
	case int64:
		return uint64(x)
	case uint:
		return uint64(x)
	case uint8:
		return uint64(x)
	case uint16:
		return uint64(x)
	case uint32:
		return uint64(x)
	case uint64:
		return x
	}
	panic("intPattern: not an integer")
}

// testValuesEqual compares decoded values, where wire integers surface as
// uint64 patterns.
func testValuesEqual(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case uint64:
		y, ok := b.(uint64)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !testValuesEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !testValuesEqual(v, w) {
				return false
			}
		}
		return true
	}
	return false
}

// decodeTestValue is a test-only inverse of encodeValue for the value
// layer (it does not consume the magic/version header). Decoded integers
// surface as uint64 64-bit patterns.
func decodeTestValue(t *testing.T, data []byte) (any, []byte) {
	t.Helper()
	if len(data) == 0 {
		t.Fatal("truncated value")
	}
	tag, rest := data[0], data[1:]
	readVarint := func() uint64 {
		n, nlen := binary.Uvarint(rest)
		if nlen <= 0 {
			t.Fatal("truncated varint")
		}
		rest = rest[nlen:]
		return n
	}
	switch tag {
	case tagNull:
		return nil, rest
	case tagBool:
		if len(rest) == 0 {
			t.Fatal("truncated bool")
		}
		return rest[0] != 0, rest[1:]
	case tagInt:
		n := readVarint()
		// Invert zigzag: (n >>> 1) ^ -(n & 1).
		return (n >> 1) ^ uint64(-(int64(n & 1))), rest
	case tagString:
		ln := readVarint()
		if uint64(len(rest)) < ln {
			t.Fatal("truncated string")
		}
		return string(rest[:ln]), rest[ln:]
	case tagList:
		count := readVarint()
		out := make([]any, 0, count)
		for i := uint64(0); i < count; i++ {
			var v any
			v, rest = decodeTestValue(t, rest)
			out = append(out, v)
		}
		return out, rest
	case tagMap:
		count := readVarint()
		out := make(map[string]any, count)
		for i := uint64(0); i < count; i++ {
			klen := readVarint()
			if uint64(len(rest)) < klen {
				t.Fatal("truncated map key")
			}
			k := string(rest[:klen])
			rest = rest[klen:]
			var v any
			v, rest = decodeTestValue(t, rest)
			out[k] = v
		}
		return out, rest
	default:
		t.Fatalf("unknown tag 0x%02x", tag)
		return nil, nil
	}
}
