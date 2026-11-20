package main

// L11 (REVIEW-2026-11-18-05): encoding/json/v2 — zero corpus imports
// before this program. Semantic JSON processing: Marshal/Unmarshal of
// structs, maps, and slices; field tags; unknown-member rejection;
// MarshalWrite/UnmarshalRead; and the error paths.

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
)

type Item struct {
	Name  string   `json:"name"`
	Count int      `json:"count,omitempty"`
	Tags  []string `json:"tags"`
	Skip  string   `json:"-"`
	priv  string
}

type Wrapper struct {
	Items []Item         `json:"items"`
	Meta  map[string]int `json:"meta"`
}

func main() {
	w := Wrapper{
		Items: []Item{{Name: "a", Count: 3, Tags: []string{"x", "y"}}, {Name: "b"}},
		Meta:  map[string]int{"total": 2},
	}
	out, err := json.Marshal(w)
	if err != nil {
		fmt.Printf("marshal-err=%v\n", err != nil)
		return
	}
	fmt.Printf("marshaled=%s\n", string(out))
	var back Wrapper
	if err := json.Unmarshal(out, &back); err != nil {
		fmt.Printf("unmarshal-err=%v\n", err != nil)
		return
	}
	fmt.Printf("roundtrip items=%d meta-total=%d first=%s\n", len(back.Items), back.Meta["total"], back.Items[0].Name)
	// omitempty: Count 0 is dropped; json:"-" is dropped; unexported dropped.
	one, _ := json.Marshal(Item{Name: "solo"})
	fmt.Printf("omitempty=%s\n", string(one))
	// Unknown members are ignored by default, rejected with the option.
	var m map[string]any
	if err := json.Unmarshal([]byte(`{"a":1,"b":2}`), &m); err != nil {
		fmt.Printf("map-unmarshal-err=%v\n", err != nil)
		return
	}
	fmt.Printf("map=%v\n", m)
	var s struct {
		A int `json:"a"`
	}
	err = json.Unmarshal([]byte(`{"a":1,"zz":9}`), &s)
	fmt.Printf("unknown-ignored-err=%v a=%d\n", err != nil, s.A)
	err = json.Unmarshal([]byte(`{"a":1,"zz":9}`), &s, json.RejectUnknownMembers(true))
	fmt.Printf("unknown-rejected-err=%v\n", err != nil)
	// MarshalWrite / UnmarshalRead.
	var buf bytes.Buffer
	if err := json.MarshalWrite(&buf, w); err != nil {
		fmt.Printf("marshalwrite-err=%v\n", err != nil)
		return
	}
	fmt.Printf("marshalwrite-bytes=%d\n", buf.Len())
	var back2 Wrapper
	if err := json.UnmarshalRead(&buf, &back2); err != nil {
		fmt.Printf("unmarshalread-err=%v\n", err != nil)
		return
	}
	fmt.Printf("read-roundtrip items=%d\n", len(back2.Items))
	// Error paths.
	var bad any
	err = json.Unmarshal([]byte(`{invalid`), &bad)
	fmt.Printf("syntax-err=%v\n", err != nil)
	_, err = json.Marshal(make(chan int))
	fmt.Printf("marshal-chan-err=%v\n", err != nil)
	var n int
	err = json.Unmarshal([]byte(`"str"`), &n)
	fmt.Printf("type-mismatch-err=%v\n", err != nil)
}
