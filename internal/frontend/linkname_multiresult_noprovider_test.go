package main

// fix-954b758b (PKG-079/080 root cause: allow multi-result //go:linkname
// without provider): exportLinknameDecl errored on any bodyless
// //go:linkname function with >1 result *before* checking for a
// provider, failing the entire package export. This broke reflect's
// badlinkname_rtype_FieldByName (2 results), which blocked the whole
// reflect export and — since fmt transitively imports reflect — all 38
// corpus programs. The general fix does the provider lookup first via
// synthFuncIDs: with a provider the decl aliases to it (multi-result
// works via the direct provider call); without one it is emitted as
// "unimplemented" (an honest backend panic if ever invoked, matching the
// bodyless-no-linkname path) instead of failing the export.
//
// Red-green: before the fix, TestLinknameMultiResultNoProvider errors
// with "at most one result is supported in frontend/v1 scope (WS D)";
// after, it exports an unimplemented funcdecl.

import (
	"testing"
)

// TestLinknameMultiResultNoProvider pins that a bodyless multi-result
// //go:linkname whose symbol has no provider exports as unimplemented
// (linkname field dropped) instead of failing the export.
func TestLinknameMultiResultNoProvider(t *testing.T) {
	src := `package main

import _ "unsafe"

//go:linkname multiNoProvider nosuchpkg.nosuchsym
func multiNoProvider() (int, bool)

func main() {}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with multi-result linkname without provider: %v", err)
	}
	doc := decodeExport(t, out)
	found := false
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != "multiNoProvider" {
			continue
		}
		found = true
		if fl["unimplemented"] != true {
			t.Errorf("multiNoProvider funcdecl missing \"unimplemented\" marker: %v", fl)
		}
		if ln, ok := fl["linkname"]; ok {
			t.Errorf("multiNoProvider funcdecl must drop the linkname field, has %q", ln)
		}
	}
	if !found {
		t.Fatal("no funcdecl for multiNoProvider in export")
	}
}

// TestLinknameMultiResultNoProviderOneField: the single-field linkname
// form (symbol defaults to <pkgpath>.<local>, which never has a
// provider) takes the same unimplemented path for multi-result decls.
func TestLinknameMultiResultNoProviderOneField(t *testing.T) {
	src := `package main

import _ "unsafe"

//go:linkname singleFieldNoProvider
func singleFieldNoProvider() (int, bool)

func main() {}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with 1-field multi-result linkname: %v", err)
	}
	doc := decodeExport(t, out)
	found := false
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != "singleFieldNoProvider" {
			continue
		}
		found = true
		if fl["unimplemented"] != true {
			t.Errorf("singleFieldNoProvider funcdecl missing \"unimplemented\" marker: %v", fl)
		}
	}
	if !found {
		t.Fatal("no funcdecl for singleFieldNoProvider in export")
	}
}
