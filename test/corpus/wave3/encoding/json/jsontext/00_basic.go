package main

// L11 (REVIEW-2026-11-18-05): encoding/json/jsontext — zero corpus
// imports before this program. Syntactic JSON processing: encoding a
// token stream, decoding it back token by token, whole-value
// round-trips, and the error paths on malformed input.

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"io"
)

func main() {
	// Encode a token stream: {"name":"demo","n":42,"ok":true,"xs":[1,2.5]}.
	var buf bytes.Buffer
	enc := jsontext.NewEncoder(&buf)
	toks := []jsontext.Token{
		jsontext.BeginObject,
		jsontext.String("name"), jsontext.String("demo"),
		jsontext.String("n"), jsontext.Int(42),
		jsontext.String("ok"), jsontext.Bool(true),
		jsontext.String("xs"), jsontext.BeginArray,
		jsontext.Int(1), jsontext.Float(2.5),
		jsontext.EndArray,
		jsontext.EndObject,
	}
	for _, t := range toks {
		if err := enc.WriteToken(t); err != nil {
			fmt.Printf("writetoken-err=%v\n", err != nil)
			return
		}
	}
	fmt.Printf("encoded=%s\n", buf.String())
	// Decode it back token by token.
	dec := jsontext.NewDecoder(&buf)
	for {
		t, err := dec.ReadToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Printf("readtoken-err=%v\n", err != nil)
			return
		}
		fmt.Printf("tok kind=%v val=%q\n", t.Kind(), t.String())
	}
	// Whole-value round-trip.
	var buf2 bytes.Buffer
	enc2 := jsontext.NewEncoder(&buf2)
	if err := enc2.WriteValue(jsontext.Value(`[1,{"a":null}]`)); err != nil {
		fmt.Printf("writevalue-err=%v\n", err != nil)
		return
	}
	dec2 := jsontext.NewDecoder(&buf2)
	v, err := dec2.ReadValue()
	fmt.Printf("value-err=%v value=%s\n", err != nil, string(v))
	// Error paths: a truncated object, and a bare invalid token.
	dec3 := jsontext.NewDecoder(bytes.NewReader([]byte(`{"a":`)))
	_, err = dec3.ReadToken()
	fmt.Printf("truncated-first-err=%v\n", err != nil)
	_, err = dec3.ReadToken()
	fmt.Printf("truncated-second-err=%v\n", err != nil)
	dec4 := jsontext.NewDecoder(bytes.NewReader([]byte(`tru`)))
	_, err = dec4.ReadToken()
	fmt.Printf("invalid-err=%v\n", err != nil)
	// Token value accessors.
	s := jsontext.String("x")
	fmt.Printf("string-tok=%q kind=%v\n", s.String(), s.Kind())
	n := jsontext.Int(-7)
	iv, err := n.Int()
	fmt.Printf("int-tok=%d err=%v\n", iv, err != nil)
}
