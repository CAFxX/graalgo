package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestErrorsDiskExport verifies the synthetic errors retirement
// (rem-review-20261031-p1-1): the syntheticSources entry is gone, and
// a program exercising the full public API — New, Is, Unwrap, Join, As,
// AsType (generic, multi-result), ErrUnsupported — exports with all
// merged funcdecls present and no raw multi-result AsType funcdecl in
// the errors package (the disk transform's __AsType helper carries the
// body; the stub is skipped). Negative control: restoring the entry
// (plus the deleted source file, which the entry check precedes) makes
// this test FAIL on the entry assertion.
func TestErrorsDiskExport(t *testing.T) {
	if _, ok := syntheticSources["errors"]; ok {
		t.Fatal("syntheticSources still has an \"errors\" entry")
	}
	src := `package main

import "errors"

type timeoutErr struct{ msg string }

func (e *timeoutErr) Error() string { return e.msg }

type wrapErr struct {
	msg string
	err error
}

func (e *wrapErr) Error() string { return e.msg }
func (e *wrapErr) Unwrap() error { return e.err }

func main() {
	err1 := errors.New("boom")
	err2 := errors.New("bang")
	joined := errors.Join(err1, nil, err2)
	println(joined.Error())
	println(errors.Is(joined, err1))
	println(errors.Unwrap(err1) == nil)
	println(errors.ErrUnsupported.Error())
	var te *timeoutErr
	println(errors.As(&wrapErr{msg: "w", err: &timeoutErr{msg: "t"}}, &te))
	if e, ok := errors.AsType[*timeoutErr](&wrapErr{msg: "w", err: &timeoutErr{msg: "t2"}}); ok {
		println(e.Error())
	}
	if _, ok := errors.AsType[*timeoutErr](nil); !ok {
		println("nil-ok")
	}
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "errdisk.go")
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
	pkgResults := map[string]map[string]int{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						names[n] = true
						pp, _ := f["pkgpath"].(string)
						nr := 0
						if rs, ok := f["results"].([]any); ok {
							nr = len(rs)
						}
						if pkgResults[pp] == nil {
							pkgResults[pp] = map[string]int{}
						}
						if nr > pkgResults[pp][n] {
							pkgResults[pp][n] = nr
						}
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
	for _, want := range []string{"New", "Is", "Unwrap", "Join", "As"} {
		if !names[want] {
			t.Errorf("wire doc missing funcdecl %q", want)
		}
	}
	// The disk transform's helper carries AsType's body; the stub is
	// skipped, so no raw 2-result AsType funcdecl may appear in the
	// errors package. The helper is generic, so the wire doc carries
	// its instantiation (errors.__AsType[*timeoutErr]), never a bare
	// "__AsType" funcdecl; and the stub instantiation
	// (errors.AsType[...], panic body) must not appear either — call
	// sites desugar to the helper, so the stub is never referenced.
	if nr := pkgResults["errors"]["AsType"]; nr > 1 {
		t.Errorf("wire doc contains raw multi-result errors funcdecl %q (%d results)", "AsType", nr)
	}
	helperFound := false
	for n := range names {
		if strings.Contains(n, "__AsType[") {
			helperFound = true
		}
		if strings.HasPrefix(n, "errors.AsType[") {
			t.Errorf("wire doc contains disk-transform stub instantiation %q (panic body)", n)
		}
	}
	if !helperFound {
		t.Errorf("wire doc missing disk-transform helper instantiation %q", "__AsType[...]")
	}
}
