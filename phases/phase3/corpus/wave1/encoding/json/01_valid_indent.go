package main

import (
	"encoding/json"
	"fmt"
)

func main() {
	// Valid: valid and invalid inputs, edge cases.
	for _, s := range []string{
		``, `null`, `true`, `false`, `123`, `-123.456e-7`, `"hello"`,
		`""`, `"a\nb"`, `"a\u00e9b"`, `[]`, `[1,2,3]`, `[1,]`, `[,]`,
		`{}`, `{"a":1}`, `{"a":}`, `{a:1}`, `{"a":1,}`,
		` `, "\n\t ", `{"a":[1,{"b":null}]}`, `{"a":01}`, `{"a":+1}`,
		`{"a":1e}`, `{"a":--1}`, `"unterminated`, `{"a":"b"`, `[1,2`,
		`"a\xb"`, "\"\\ud800\"", "\"\\udc00\"",
	} {
		fmt.Printf("Valid(%q) = %v\n", s, json.Valid([]byte(s)))
	}

	// Indent is not in M7 scope (needs *bytes.Buffer via &buf, which v1
	// cannot emit); the synthetic signature matches gc 1.27.1 exactly
	// (verified by the drift test), but it cannot be called from v1 code.
	// RawMessage.MarshalJSON.
	rm := json.RawMessage(`{"x":1}`)
	b, err := rm.MarshalJSON()
	fmt.Printf("RawMessage.MarshalJSON = %s, err=%v\n", string(b), err != nil)
	rm2 := json.RawMessage{}
	b, err = rm2.MarshalJSON()
	fmt.Printf("Empty RawMessage = %s, err=%v\n", string(b), err != nil)
}
