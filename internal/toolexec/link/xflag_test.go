package link

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"testing"

	"graalgo-toolexec/emit"
	"graalgo-toolexec/girinterp"
	"graalgo-toolexec/ir"
)

func TestParseX(t *testing.T) {
	for _, c := range []struct {
		args []string
		want []XSetting
		err  bool
	}{
		{args: []string{"main.v=1.2.3"}, want: []XSetting{{"main", "v", "1.2.3"}}},
		// The first '=' ends the symbol: the value may contain '=' and '.'.
		{args: []string{"main.v=a=b.c"}, want: []XSetting{{"main", "v", "a=b.c"}}},
		{args: []string{"main.v="}, want: []XSetting{{"main", "v", ""}}},
		// The last '.' before the '=' splits: dotted module paths.
		{args: []string{"gopkg.in/yaml.v3.Version=x"}, want: []XSetting{{"gopkg.in/yaml.v3", "Version", "x"}}},
		{args: []string{"example.com/m/vendor/golang.org/x/net/http2.V=x"},
			want: []XSetting{{"example.com/m/vendor/golang.org/x/net/http2", "V", "x"}}},
		// A later setting replaces the value; the first keeps its position.
		{args: []string{"main.a=1", "main.b=2", "main.a=3"}, want: []XSetting{{"main", "a", "3"}, {"main", "b", "2"}}},
		// Empty package path: accepted (it names no symbol of the link).
		{args: []string{".r=x"}, want: []XSetting{{"", "r", "x"}}},
		{args: []string{"mainr"}, err: true},
		{args: []string{"main.r"}, err: true},
		{args: []string{"r=main.x"}, err: true},
	} {
		got, err := ParseX(c.args)
		if (err != nil) != c.err {
			t.Errorf("ParseX(%q) error %v, want error %v", c.args, err, c.err)
			continue
		}
		if err != nil {
			// cmd/link's wording.
			if err.Error() != "-X flag requires argument of the form importpath.name=value" {
				t.Errorf("ParseX(%q) error %q", c.args, err)
			}
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseX(%q) = %+v, want %+v", c.args, got, c.want)
		}
	}
}

func TestPathToPrefix(t *testing.T) {
	for in, want := range map[string]string{
		"main":                "main",
		"example.com/a/b":     "example.com/a/b",
		"gopkg.in/yaml.v3":    "gopkg.in/yaml%2ev3",
		"a b/c\"d%":           "a%20b/c%22d%25",
		"example.com/ét": "example.com/%c3%a9t",
	} {
		if got := PathToPrefix(in); got != want {
			t.Errorf("PathToPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

// emitMain emits package main from one import-free source file.
func emitMain(t *testing.T, src string) *ir.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := emit.NewInfo()
	conf := types.Config{GoVersion: "go1.27", Sizes: types.SizesFor("gc", "amd64")}
	pkg, err := conf.Check("main", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := emit.Emit(emit.Config{Fset: fset, Pkg: pkg, Info: info, Files: []*ast.File{f}, Lang: "go1.27", GOOS: "linux", GOARCH: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	return roundTrip(t, g)
}

func roundTrip(t *testing.T, f *ir.File) *ir.File {
	t.Helper()
	data, err := ir.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	g, err := ir.DecodeValidate(data)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// runX applies -X settings to an emitted program the way the link step
// does (apply, re-encode) and runs it on the reference GIR interpreter.
func runX(t *testing.T, src string, args []string, static map[string]bool) string {
	t.Helper()
	f := emitMain(t, src)
	xs, err := ParseX(args)
	if err != nil {
		t.Fatal(err)
	}
	var asked []string
	changed, err := ApplyX([]*ir.File{f}, xs, func(i int, symbol string) (bool, error) {
		asked = append(asked, symbol)
		s, ok := static[symbol]
		if !ok {
			t.Errorf("asked gc's verdict on %s, which has a constant initializer or none", symbol)
		}
		return s, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for s := range static {
		want = append(want, s)
	}
	slices.Sort(want)
	slices.Sort(asked)
	if !slices.Equal(asked, want) {
		t.Errorf("asked gc's verdict on %q, want %q", asked, want)
	}
	if len(xs) > 0 && !slices.Equal(changed, []int{0}) {
		t.Errorf("changed = %v, want [0]", changed)
	}
	var out, errb bytes.Buffer
	in, err := girinterp.New([]*ir.File{roundTrip(t, f)}, &out, &errb)
	if err != nil {
		t.Fatal(err)
	}
	if code, err := in.Run(); err != nil || code != 0 {
		t.Fatalf("run: code %d, err %v\n%s", code, err, errb.String())
	}
	return out.String() + errb.String()
}

// The expected outputs are gc's (go1.27.1, go build -ldflags), recorded
// for the same programs: static data is overwritten, initializer code
// runs after it and wins.
func TestApplyXMatchesGc(t *testing.T) {
	const src = `package main

const k = "konst"

var a = "dev"
var b string
var c = a
var d = "v" + a
var e = k + "!"
var z = ""
var h, i = "h0", "i0"
var p = id("p0")
var j = dyn()
var u = "unset"

func id(s string) string { return s }

//go:noinline
func dyn() string { return "j0" }

func main() {
	println(a, b, c, d, e, z, h, i, p, j, u, k)
}
`
	args := []string{"main.a=A", "main.b=B=1", "main.c=C", "main.d=D", "main.e=E", "main.z=Z",
		"main.h=H", "main.i=I", "main.p=P", "main.j=J", "main.k=K", "main.main=M", "main.nosuch=N",
		"other.a=O", "main.a=A2"}
	// gc's object: p's inlined call is static data, j's call and c's and
	// d's expressions over a string variable are initializer code.
	static := map[string]bool{"main.c": false, "main.d": false, "main.p": true, "main.j": false}
	got := runX(t, src, args, static)
	want := "A2 B=1 A2 vA2 E Z H I P j0 unset konst\n"
	if got != want {
		t.Errorf("output %q, want gc's %q", got, want)
	}
	if got := runX(t, src, nil, nil); got != "dev  dev vdev konst!  h0 i0 p0 j0 unset konst\n" {
		t.Errorf("without -X: %q", got)
	}
}

// A package without any initializer gets one.
func TestApplyXCreatesInitializer(t *testing.T) {
	const src = `package main

var v, w string

func main() { println(v, w) }
`
	if got, want := runX(t, src, []string{"main.w=set"}, nil), " set\n"; got != want {
		t.Errorf("output %q, want %q", got, want)
	}
}

func TestApplyXRejectsNonString(t *testing.T) {
	const src = `package main

type S string

var n = 3
var s S = "s"
var ok string

func main() { println(n, s, ok) }
`
	f := emitMain(t, src)
	xs, _ := ParseX([]string{"main.n=4", "main.ok=x", "main.s=S"})
	_, err := ApplyX([]*ir.File{f}, xs, func(int, string) (bool, error) { return false, nil })
	var le *Error
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want *Error", err)
	}
	// cmd/link's diagnostics, in setting order.
	want := []string{
		"main.n: cannot set with -X: not a var of type string (type:int)",
		"main.s: cannot set with -X: not a var of type string (type:main.S)",
	}
	if !slices.Equal(le.Problems, want) {
		t.Errorf("problems %q, want %q", le.Problems, want)
	}
}
