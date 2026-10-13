package main

import (
	"encoding/gob"
	"fmt"
)

func main() {
	// In-scope constructors: NewBufferEncoder / NewBufferDecoder.
	// (gc's NewEncoder/NewDecoder take io.Writer/io.Reader; io is out
	// of M7 scope by design.)
	//
	// NOTE: the synthetic gob encodes only scalar values, []byte, []any
	// and map[string]any -- struct/array encoding needs reflect, which
	// is out of M7 scope. Re-test trigger: when reflect lands (P3-M8),
	// add a struct round-trip (the original Point-struct program).

	// Basic encode/decode round-trip: int, string, bool, float64.
	enc := gob.NewBufferEncoder()
	mustEncode(enc, 42)
	mustEncode(enc, "hello")
	mustEncode(enc, true)
	mustEncode(enc, 3.25)
	data := enc.Bytes()
	fmt.Printf("Encoded %d bytes\n", len(data))

	dec := gob.NewBufferDecoder(data)
	var i int
	var s string
	var b bool
	var f float64
	mustDecode(dec, &i)
	mustDecode(dec, &s)
	mustDecode(dec, &b)
	mustDecode(dec, &f)
	fmt.Printf("Decoded: %d %q %v %v\n", i, s, b, f)

	// Composite values: []any and map[string]any round-trip.
	enc2 := gob.NewBufferEncoder()
	mustEncode(enc2, []any{1, "two", false})
	mustEncode(enc2, map[string]any{"a": 1, "b": "two"})
	dec2 := gob.NewBufferDecoder(enc2.Bytes())
	var sl []any
	var m map[string]any
	mustDecode(dec2, &sl)
	mustDecode(dec2, &m)
	fmt.Printf("Slice: %v\n", sl)
	fmt.Printf("Map: %v %v\n", m["a"], m["b"])

	// Adversarial: negative ints, empty string, empty composites,
	// max uint64, []byte with binary data.
	enc3 := gob.NewBufferEncoder()
	mustEncode(enc3, -12345)
	mustEncode(enc3, "")
	mustEncode(enc3, []any{})
	mustEncode(enc3, map[string]any{})
	mustEncode(enc3, uint64(18446744073709551615))
	mustEncode(enc3, []byte{0, 1, 2, 255})
	dec3 := gob.NewBufferDecoder(enc3.Bytes())
	var n int
	var e string
	var z []any
	var zm map[string]any
	var u uint64
	var by []byte
	mustDecode(dec3, &n)
	mustDecode(dec3, &e)
	mustDecode(dec3, &z)
	mustDecode(dec3, &zm)
	mustDecode(dec3, &u)
	mustDecode(dec3, &by)
	fmt.Printf("Adversarial: %d %q len(z)=%d len(zm)=%d %d %v\n",
		n, e, len(z), len(zm), u, by)
}

func mustEncode(enc *gob.Encoder, v any) {
	if err := enc.Encode(v); err != nil {
		fmt.Printf("Encode err: %v\n", err)
		panic("encode failed")
	}
}

func mustDecode(dec *gob.Decoder, e any) {
	if err := dec.Decode(e); err != nil {
		fmt.Printf("Decode err: %v\n", err)
		panic("decode failed")
	}
}
