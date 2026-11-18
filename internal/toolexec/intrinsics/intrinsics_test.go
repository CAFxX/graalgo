package intrinsics

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The checked-in Java dispatch table is what the registry generates.
func TestJavaUpToDate(t *testing.T) {
	got, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(JavaPath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != Registry.Java() {
		t.Errorf("%s is stale: run `go generate ./intrinsics` in internal/toolexec", JavaPath)
	}
}

func TestParse(t *testing.T) {
	tab, err := Parse("version 3\nintrinsic a/b.F C func(int, func(int) (int, error)) (string, bool) # c\nreplace a/b.G C func()\n")
	if err != nil {
		t.Fatal(err)
	}
	if tab.Version != 3 || len(tab.Entries) != 2 {
		t.Fatalf("got %+v", tab)
	}
	e, ok := tab.Lookup("a/b.F")
	if !ok || e.Replace || e.Class != "C" {
		t.Fatalf("Lookup = %+v, %v", e, ok)
	}
	params, results, err := SplitSig(e.Sig)
	if err != nil || !reflect.DeepEqual(params, []string{"int", "func(int) (int, error)"}) || !reflect.DeepEqual(results, []string{"string", "bool"}) {
		t.Errorf("SplitSig = %q, %q, %v", params, results, err)
	}
	if g, _ := tab.Lookup("a/b.G"); !g.Replace {
		t.Errorf("a/b.G is not a replace entry")
	}
	if _, ok := tab.Lookup("a/b.H"); ok {
		t.Errorf("a/b.H found")
	}
	// The hash covers every field of every entry and the version.
	for _, other := range []string{
		"version 4\nintrinsic a/b.F C func(int, func(int) (int, error)) (string, bool)\nreplace a/b.G C func()\n",
		"version 3\nintrinsic a/b.F C func(int, func(int) (int, error)) (string, bool)\nintrinsic a/b.G C func()\n",
		"version 3\nintrinsic a/b.F C func(int, func(int) (int, error)) string\nreplace a/b.G C func()\n",
		"version 3\nintrinsic a/b.F D func(int, func(int) (int, error)) (string, bool)\nreplace a/b.G C func()\n",
	} {
		o, err := Parse(other)
		if err != nil {
			t.Fatal(err)
		}
		if o.Hash == tab.Hash {
			t.Errorf("same hash for a different registry:\n%s", other)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for text, want := range map[string]string{
		"intrinsic a.F C func()\n":                                        "no version",
		"version 1\nversion 2\n":                                          "exactly one",
		"version 1\nintrinsic a.F C func()\nreplace a.F C func()\n":       "registered twice",
		"version 1\nintrinsic a.b.F C func()\nintrinsic a/b.F C func()\n": "same Java method",
		"version 1\nintrinsic F C func()\n":                               "no package",
		"version 1\nintrinsic a.F C int\n":                                "not a function type key",
		"version 1\nintrinsic a.F 9C func()\n":                            "not a Java identifier",
		"version 1\nbogus a.F C func()\n":                                 "unknown declaration",
	} {
		if _, err := Parse(text); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %v, want error containing %q", text, err, want)
		}
	}
}
