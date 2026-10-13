package main

import (
	"encoding/json"
	"fmt"
)

func main() {
	// Marshal: any-model values (nil, bool, string, numbers, slices, maps,
	// nested combinations, RawMessage, Number).
	vals := []any{
		nil, true, false, "hello", "", "a\nb\tc\"d\\e",
		"héllo 🙂", "<html>&\"'",
		int(42), int8(-8), int64(1 << 62), uint64(1 << 64 - 1),
		float64(1.5), float64(0.1), float64(1e21),
		[]any{}, []any{1, "two", 3.0, nil, true},
		map[string]any{}, map[string]any{"b": 2, "a": 1, "c": nil},
		map[string]any{"nested": map[string]any{"x": []any{1, 2}}},
		json.RawMessage(`{"raw":true}`),
		json.Number("123.456"),
	}
	for _, v := range vals {
		b, err := json.Marshal(v)
		fmt.Printf("Marshal(%T) = %s, err=%v\n", v, string(b), err != nil)
	}

	// MarshalIndent.
	b, err := json.MarshalIndent(map[string]any{"a": 1, "b": []any{1, 2}}, "", "  ")
	fmt.Printf("MarshalIndent = %s, err=%v\n", string(b), err != nil)
	b, err = json.MarshalIndent(map[string]any{"a": 1}, ">>", "\t")
	fmt.Printf("MarshalIndent prefix/tab = %s, err=%v\n", string(b), err != nil)

	// HTML escaping on/off via Encoder is unreachable; Marshal always escapes.
	b, _ = json.Marshal("<>&")
	fmt.Printf("HTML escape: %s\n", string(b))

	// Unsupported values.
	for _, v := range []any{complex(1, 2), func() {}, make(chan int)} {
		_, err := json.Marshal(v)
		fmt.Printf("Marshal(%T) err=%v\n", v, err != nil)
		if ute, ok := err.(*json.UnsupportedValueError); ok {
			fmt.Printf("  UnsupportedValueError: %v\n", ute)
		}
	}

	// Number methods.
	n := json.Number("123.456")
	f, err := n.Float64()
	fmt.Printf("Number.Float64 = %v, err=%v\n", f, err != nil)
	i, err := n.Int64()
	fmt.Printf("Number.Int64 = %v, err=%v\n", i, err != nil)
	n2 := json.Number("abc")
	_, err = n2.Float64()
	fmt.Printf("Number(abc).Float64 err=%v\n", err != nil)
	fmt.Printf("Number.String = %q\n", n.String())

	// Delim.
	d := json.Delim('{')
	fmt.Printf("Delim = %q\n", d.String())
}
