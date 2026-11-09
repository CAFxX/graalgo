package jsonroundtrip

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Regression test for R08-S-01 (review-08-synth-json): encoding/json
// could not decode anything and could not marshal named structs —
// Unmarshal, Decoder.Decode and RawMessage.UnmarshalJSON were
// unconditional fail-fast panics ("needs store-through-pointer;
// re-test when WS D lands") and Marshal panicked on any named struct
// type ("not in M7 scope (needs reflect, P3-M8)"). The stated
// blockers have landed (reflect is real, pointer stores work in the
// disk path), so the synthetic package was retired and real
// encoding/json now compiles from GOROOT via the disk importer.
//
// The test proves the round-trip on a named struct (tags, omitempty,
// nested struct, slice, map) plus the decode entry points, and pins
// the R08-S-09 unsupported-type error strings (they printed
// "json: unsupported type: <nil>" under the synthetic package because
// the error carried a nil Type).
//
// Pre-fix, every test here fails: Marshal/Unmarshal/Decode panic at
// guest runtime. The test runs under `graalgo test` (test-regression
// gate); it also passes under stock gc.

type Item struct {
	Name  string         `json:"name"`
	Price float64        `json:"price"`
	Tags  []string       `json:"tags,omitempty"`
	Meta  map[string]int `json:"meta"`
	Inner *Inner         `json:"inner,omitempty"`
}

type Inner struct {
	Count int    `json:"count"`
	Note  string `json:"note"`
}

func TestJsonMarshalNamedStruct(t *testing.T) {
	it := Item{Name: "widget", Price: 9.99, Tags: []string{"a", "b"},
		Meta: map[string]int{"b": 2, "a": 1}, Inner: &Inner{Count: 3, Note: "n"}}
	data, err := json.Marshal(it)
	if err != nil {
		t.Fatalf("Marshal(named struct): %v", err)
	}
	// Map keys sort; omitempty omits nothing here (Tags non-empty).
	want := `{"name":"widget","price":9.99,"tags":["a","b"],"meta":{"a":1,"b":2},"inner":{"count":3,"note":"n"}}`
	if string(data) != want {
		t.Fatalf("Marshal(named struct) = %s, want %s", data, want)
	}
	// omitempty: empty Tags and nil Inner are omitted.
	it2 := Item{Name: "x", Price: 1, Meta: map[string]int{}}
	data, err = json.Marshal(it2)
	if err != nil {
		t.Fatalf("Marshal(omitempty): %v", err)
	}
	if want := `{"name":"x","price":1,"meta":{}}`; string(data) != want {
		t.Fatalf("Marshal(omitempty) = %s, want %s", data, want)
	}
}

func TestJsonUnmarshalNamedStructRoundTrip(t *testing.T) {
	data := []byte(`{"name":"widget","price":9.99,"tags":["a","b"],"meta":{"a":1,"b":2},"inner":{"count":3,"note":"n"}}`)
	var back Item
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal(named struct): %v", err)
	}
	if back.Name != "widget" || back.Price != 9.99 || len(back.Tags) != 2 ||
		back.Tags[0] != "a" || back.Meta["a"] != 1 || back.Meta["b"] != 2 ||
		back.Inner == nil || back.Inner.Count != 3 || back.Inner.Note != "n" {
		t.Fatalf("Unmarshal(named struct) = %+v, want the widget", back)
	}
	// Marshal(Unmarshal(x)) is the identity.
	data2, err := json.Marshal(back)
	if err != nil {
		t.Fatalf("re-Marshal: %v", err)
	}
	if string(data2) != string(data) {
		t.Fatalf("Marshal(Unmarshal(x)) = %s, want %s", data2, data)
	}
}

func TestJsonDecoderDecode(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(`{"name":"widget","price":9.99,"meta":{"a":1}} {"name":"second","price":2}`))
	var first, second Item
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("Decoder.Decode #1: %v", err)
	}
	if first.Name != "widget" || first.Meta["a"] != 1 {
		t.Fatalf("Decoder.Decode #1 = %+v, want the widget", first)
	}
	if err := dec.Decode(&second); err != nil {
		t.Fatalf("Decoder.Decode #2: %v", err)
	}
	if second.Name != "second" || second.Price != 2 {
		t.Fatalf("Decoder.Decode #2 = %+v, want second", second)
	}
}

func TestJsonRawMessageUnmarshalJSON(t *testing.T) {
	var rm json.RawMessage
	if err := rm.UnmarshalJSON([]byte(`{"x":1}`)); err != nil {
		t.Fatalf("RawMessage.UnmarshalJSON: %v", err)
	}
	if string(rm) != `{"x":1}` {
		t.Fatalf("RawMessage after UnmarshalJSON = %s, want {\"x\":1}", rm)
	}
	b, err := rm.MarshalJSON()
	if err != nil {
		t.Fatalf("RawMessage.MarshalJSON: %v", err)
	}
	if string(b) != `{"x":1}` {
		t.Fatalf("RawMessage.MarshalJSON = %s, want {\"x\":1}", b)
	}
	// Unmarshal into a RawMessage field keeps the raw bytes.
	var holder struct {
		Raw json.RawMessage `json:"raw"`
	}
	if err := json.Unmarshal([]byte(`{"raw":{"nested":[1,2]}}`), &holder); err != nil {
		t.Fatalf("Unmarshal into RawMessage field: %v", err)
	}
	if string(holder.Raw) != `{"nested":[1,2]}` {
		t.Fatalf("RawMessage field = %s, want {\"nested\":[1,2]}", holder.Raw)
	}
}

func TestJsonMarshalIndentNamedStruct(t *testing.T) {
	it := Item{Name: "widget", Price: 9.99, Meta: map[string]int{"a": 1}}
	data, err := json.MarshalIndent(it, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent(named struct): %v", err)
	}
	want := "{\n  \"name\": \"widget\",\n  \"price\": 9.99,\n  \"meta\": {\n    \"a\": 1\n  }\n}"
	if string(data) != want {
		t.Fatalf("MarshalIndent(named struct) = %s, want %s", data, want)
	}
}

func TestJsonEncoderDecoderStream(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(Item{Name: "widget", Price: 9.99, Meta: map[string]int{"a": 1}}); err != nil {
		t.Fatalf("Encoder.Encode: %v", err)
	}
	var back Item
	if err := json.NewDecoder(&buf).Decode(&back); err != nil {
		t.Fatalf("Decoder.Decode after Encode: %v", err)
	}
	if back.Name != "widget" || back.Meta["a"] != 1 {
		t.Fatalf("Encode/Decode round-trip = %+v, want the widget", back)
	}
}

func TestJsonUnmarshalAnyModel(t *testing.T) {
	// The any-model (the only shape the synthetic package handled)
	// still works through the real package.
	var v any
	if err := json.Unmarshal([]byte(`{"a":[1,2.5,"x",null,true]}`), &v); err != nil {
		t.Fatalf("Unmarshal into any: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("Unmarshal into any = %T, want map[string]any", v)
	}
	arr, ok := m["a"].([]any)
	if !ok || len(arr) != 5 || arr[0] != float64(1) || arr[2] != "x" || arr[3] != nil || arr[4] != true {
		t.Fatalf("Unmarshal into any: a = %v, want [1 2.5 x nil true]", m["a"])
	}
	if !json.Valid([]byte(`{"a":1}`)) || json.Valid([]byte(`{"a":}`)) {
		t.Fatal("json.Valid disagrees with gc on valid/invalid input")
	}
}

// TestJsonUnsupportedTypeErrorStrings pins R08-S-09: the synthetic
// package returned &UnsupportedTypeError{Type: nil} for func/chan/
// complex values, so the error printed "json: unsupported type:
// <nil>" instead of the type name. The real package populates Type
// via reflect, so the message names the type.
func TestJsonUnsupportedTypeErrorStrings(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{func() {}, "json: unsupported type: func()"},
		{complex64(1 + 2i), "json: unsupported type: complex64"},
		{make(chan int), "json: unsupported type: chan int"},
	}
	for _, c := range cases {
		_, err := json.Marshal(c.v)
		if err == nil {
			t.Fatalf("Marshal(%T): expected an error, got nil", c.v)
		}
		if err.Error() != c.want {
			t.Fatalf("Marshal(%T) error = %q, want %q", c.v, err.Error(), c.want)
		}
		if _, ok := err.(*json.UnsupportedTypeError); !ok {
			t.Fatalf("Marshal(%T) error is %T, want *json.UnsupportedTypeError", c.v, err)
		}
	}
}

func TestJsonUnmarshalErrors(t *testing.T) {
	// Wrong-type value: *UnmarshalTypeError naming the field.
	var it Item
	err := json.Unmarshal([]byte(`{"price":"not a number"}`), &it)
	if err == nil {
		t.Fatal("Unmarshal(price as string): expected an error, got nil")
	}
	if _, ok := err.(*json.UnmarshalTypeError); !ok {
		t.Fatalf("Unmarshal(price as string) error is %T, want *json.UnmarshalTypeError", err)
	}
	// Non-pointer destination: *InvalidUnmarshalError.
	err = json.Unmarshal([]byte(`{"name":"x"}`), 42)
	if err == nil {
		t.Fatal("Unmarshal into non-pointer: expected an error, got nil")
	}
	if _, ok := err.(*json.InvalidUnmarshalError); !ok {
		t.Fatalf("Unmarshal into non-pointer error is %T, want *json.InvalidUnmarshalError", err)
	}
	// Syntax error.
	var v any
	if err := json.Unmarshal([]byte(`{"a":}`), &v); err == nil {
		t.Fatal("Unmarshal of invalid JSON: expected an error, got nil")
	} else if _, ok := err.(*json.SyntaxError); !ok {
		t.Fatalf("Unmarshal of invalid JSON error is %T, want *json.SyntaxError", err)
	}
}
