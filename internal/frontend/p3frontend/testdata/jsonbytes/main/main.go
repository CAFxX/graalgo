package main

// P3FE-2 regression fixture: json.Compact/HTMLEscape/Indent must accept
// a *bytes.Buffer from the importer's cached synthetic bytes package.
// Before the fix, the synthetic json package fabricated its own
// throwaway bytes.Buffer named type, so this program failed type-check
// with "cannot use &buf (value of type *bytes.Buffer) as *bytes.Buffer
// value" — identically-rendered signatures, different type identity.

import (
	"bytes"
	"encoding/json"
)

func main() {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte("{\"a\": 1}")); err != nil {
		panic(err)
	}
	var esc bytes.Buffer
	json.HTMLEscape(&esc, []byte("<p>x</p>"))
	var ind bytes.Buffer
	if err := json.Indent(&ind, []byte("{\"a\": 1}"), "", "  "); err != nil {
		panic(err)
	}
	if buf.Len() == 0 && esc.Len() == 0 && ind.Len() == 0 {
		panic("unreachable")
	}
}
