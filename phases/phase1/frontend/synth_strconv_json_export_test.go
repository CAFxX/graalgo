package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyntheticStrconvM7 verifies the P3-M7 wave-1 synthetic strconv
// package: a program importing "strconv" exports cleanly; the real-Go
// declarations become funcdecls in the wire doc; the multi-result
// Parse*/Atoi/Unquote* declarations are skipped at export (WS D —
// 2-target call sites desugar, every other position fails fast); the
// errors dependency is pulled in automatically.
func TestSyntheticStrconvM7(t *testing.T) {
	src := `package main

import "strconv"

func main() {
	println(strconv.Itoa(42))
	println(strconv.FormatInt(-17, 16))
	println(strconv.FormatUint(255, 2))
	println(strconv.FormatBool(true))
	println(strconv.FormatFloat(1.5, 'f', -1, 64))
	v, err := strconv.Atoi("123")
	println(v, err == nil)
	u, err := strconv.ParseUint("ff", 16, 64)
	println(u, err == nil)
	i, err := strconv.ParseInt("-42", 10, 64)
	println(i, err == nil)
	f, err := strconv.ParseFloat("2.5", 64)
	println(f, err == nil)
	b, err := strconv.ParseBool("true")
	println(b, err == nil)
	c, err := strconv.ParseComplex("(1+2i)", 128)
	println(real(c), imag(c), err == nil)
	s, err := strconv.Unquote("\"hi\"")
	println(s, err == nil)
	q, err := strconv.QuotedPrefix("\"ab\"")
	println(q, err == nil)
	r, mb, tail, err := strconv.UnquoteChar("a", '"')
	println(int(r), mb, tail, err == nil)
	println(strconv.Quote("a\nb"))
	println(strconv.CanBackquote("ok"))
	println(strconv.IsPrint('a'))
	println(strconv.IntSize)
	println(strconv.ErrSyntax == nil)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synstrconv.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						names[n] = true
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(raw)
	// Real-Go declarations must be exported as funcdecls (wire-doc
	// funcdecl names are bare — no package qualifier), including the
	// single-result __-helpers the multi-result call sites desugar to.
	for _, want := range []string{
		"Itoa", "FormatInt", "FormatUint", "FormatBool", "FormatFloat",
		"Quote", "CanBackquote", "IsPrint",
		"__atoiPub", "__parseUintPub", "__parseIntPub", "__parseBoolPub",
		"__parseFloat", "__parseComplexPub", "__unquotePub",
		"__quotedPrefix", "__unquoteChar",
		// The errors dependency comes along.
		"New",
	} {
		if !names[want] {
			t.Errorf("wire doc missing funcdecl %q", want)
		}
	}
	// Multi-result declarations are skipped at export (WS D).
	for _, absent := range []string{
		"Atoi", "ParseUint", "ParseInt", "ParseBool", "ParseFloat",
		"ParseComplex", "Unquote", "QuotedPrefix", "UnquoteChar",
	} {
		if names[absent] {
			t.Errorf("wire doc must not contain multi-result funcdecl %q", absent)
		}
	}
	// The 2-target desugar leaves a __p5mr_ temp vardecl in main.
	if !strings.Contains(out, "__p5mr_") {
		t.Errorf("wire doc has no __p5mr_ desugar temp (multi-result desugar did not fire)")
	}
}

// TestSyntheticStrconvArityFailsFast verifies a multi-result strconv
// call in a non-desugarable position (a bare call statement — results
// discarded) fails with the named M7 scope error instead of silently
// half-compiling. (A 1-target := is rejected earlier by go/types'
// assignment check — also not silent.)
func TestSyntheticStrconvArityFailsFast(t *testing.T) {
	src := `package main

import "strconv"

func main() {
	strconv.Atoi("123")
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synstrconv1.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ExportFile(p)
	if err == nil {
		t.Fatalf("ExportFile succeeded on 1-target strconv.Atoi, want M7 scope error")
	}
	if !strings.Contains(err.Error(), "M7") {
		t.Fatalf("error %q does not mention M7", err.Error())
	}
}

// TestSyntheticJsonM7 verifies the P3-M7 wave-1 synthetic encoding/json
// package: a program importing "encoding/json" exports cleanly; the
// real-Go declarations become funcdecls; the multi-result Marshal/
// MarshalIndent/Indent/Decoder.Token/Number.Float64/Int64 declarations
// are skipped at export (WS D — call sites desugar).
func TestSyntheticJsonM7(t *testing.T) {
	src := `package main

import "encoding/json"

func main() {
	// NB: json.Unmarshal is fail-fast in M7 scope (needs
	// store-through-pointer; see synth_json.go) — exercised below via
	// the fail-fast funcdecl check, not called here.
	b, err := json.Marshal("hello")
	println(len(b), err == nil)
	b2, err := json.MarshalIndent("hello", "", "  ")
	println(len(b2), err == nil)
	// NB: json.Indent needs *bytes.Buffer (unreachable in v1: needs &buf);
	// it is verified as an exported fail-fast funcdecl below.
	println(json.Valid(b))
	n := json.Number("42")
	f, err := n.Float64()
	println(f, err == nil)
	iv, err := n.Int64()
	println(iv, err == nil)
	// NB: json.NewDecoder/NewEncoder need a user-defined Read/Write
	// method (2 results), which v1 cannot emit (WS D) — the streaming
	// API is not reachable from user code in M7 scope. Decoder.Token's
	// desugar is still exercised statically on a *Decoder value.
	var dec *json.Decoder
	tok, err := dec.Token()
	println(tok == nil, err == nil)
	println(json.Delim('x').String())
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synjson.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						names[n] = true
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(raw)
	for _, want := range []string{
		"Valid", "NewDecoder", "NewEncoder", "Encode",
		"__marshal", "__marshalIndent", "__decoderToken",
		"__numberFloat64", "__numberInt64",
		// Fail-fast single-result members are exported (panic bodies).
		"Unmarshal", "Indent",
	} {
		if !names[want] {
			t.Errorf("wire doc missing funcdecl %q", want)
		}
	}
	for _, absent := range []string{
		"Marshal", "MarshalIndent",
	} {
		if names[absent] {
			t.Errorf("wire doc must not contain multi-result funcdecl %q", absent)
		}
	}
	if !strings.Contains(out, "__p5mr_") {
		t.Errorf("wire doc has no __p5mr_ desugar temp (multi-result desugar did not fire)")
	}
}

// TestSyntheticJsonArityFailsFast verifies a multi-result json call in a
// non-desugarable position (a bare call statement — results discarded)
// fails with the named M7 scope error instead of silently half-compiling.
func TestSyntheticJsonArityFailsFast(t *testing.T) {
	src := `package main

import "encoding/json"

func main() {
	json.Marshal(42)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synjson1.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ExportFile(p)
	if err == nil {
		t.Fatalf("ExportFile succeeded on 1-target json.Marshal, want M7 scope error")
	}
	if !strings.Contains(err.Error(), "M7") {
		t.Fatalf("error %q does not mention M7", err.Error())
	}
}
