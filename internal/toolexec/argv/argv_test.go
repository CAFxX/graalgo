package argv

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// Vectors from the GCC buildargv rules that objabi.ParseArgs implements.
func TestParseArgs(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"a b\tc\nd\r\ne", []string{"a", "b", "c", "d", "e"}},
		{`""`, []string{""}},
		{`'' x`, []string{"", "x"}},
		{`'a "b" \c'`, []string{`a "b" \c`}},
		{`"a \"b\" \\ \$ \` + "`" + ` \q"`, []string{`a "b" \ $ ` + "`" + ` \q`}},
		{"\"a\\\nb\"", []string{"ab"}},
		{"a\\\nb", []string{"ab"}},
		{"a\\\r\nb", []string{"ab"}},
		{`a\ b`, []string{"a b"}},
		{`trailing\`, []string{"trailing"}},
		{`x"y z"w`, []string{"xy zw"}},
		{"", nil},
		{"   \n ", nil},
	} {
		if got := ParseArgs([]byte(tc.in)); !slices.Equal(got, tc.want) {
			t.Errorf("ParseArgs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// What cmd/go writes, ParseArgs must read back unchanged.
func TestEncodeParseRoundTrip(t *testing.T) {
	args := []string{"", "plain", "with space", `q"uote`, "s'ingle", `back\slash`, "$HOME", "`tick`", "new\nline", "tab\t", "ünï"}
	if got := ParseArgs(EncodeArgs(args)); !slices.Equal(got, args) {
		t.Fatalf("round trip = %q, want %q", got, args)
	}
}

func FuzzEncodeParse(f *testing.F) {
	for _, s := range []string{"", "a b", `"\$`, "x\r\n"} {
		f.Add(s, "tail")
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		if !utf8.ValidString(a) || !utf8.ValidString(b) {
			// cmd/go's encodeArg iterates runes, so it (and EncodeArg,
			// which copies it) maps invalid UTF-8 to U+FFFD.
			return
		}
		in := []string{a, b}
		if got := ParseArgs(EncodeArgs(in)); !slices.Equal(got, in) {
			t.Fatalf("round trip of %q = %q", in, got)
		}
	})
}

func TestSplitJoinQuoted(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"a b  c", []string{"a", "b", "c"}},
		{`'-toolexec=x y' -tags=a`, []string{"-toolexec=x y", "-tags=a"}},
		{`"a 'b'" c`, []string{"a 'b'", "c"}},
		{`x'y z'`, []string{"x'y", "z'"}}, // quotes only count at field start
	} {
		got, err := SplitQuoted(tc.in)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("SplitQuoted(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := SplitQuoted(`'open`); err == nil {
		t.Error("unterminated quote accepted")
	}
	for _, fields := range [][]string{
		{"a", "b c", ""},
		{"/h/libexec/compile", "-chain", `strace -o "log file"`},
		{"-toolexec=/h/libexec/compile -chain 'x y'"},
	} {
		s, err := JoinQuoted(fields)
		if err != nil {
			t.Fatalf("JoinQuoted(%q): %v", fields, err)
		}
		back, err := SplitQuoted(s)
		if err != nil || !slices.Equal(back, fields) {
			t.Errorf("JoinQuoted(%q) = %q splits to %q", fields, s, back)
		}
	}
	if _, err := JoinQuoted([]string{`both ' and "`}); err == nil {
		t.Error("unquotable field accepted")
	}
}

func FuzzJoinSplit(f *testing.F) {
	f.Add("a b", "'c'")
	f.Fuzz(func(t *testing.T, a, b string) {
		in := []string{a, b}
		s, err := JoinQuoted(in)
		if err != nil {
			return // JoinQuoted refuses rather than mis-quotes
		}
		if back, err := SplitQuoted(s); err != nil || !slices.Equal(back, in) {
			t.Fatalf("JoinQuoted(%q) = %q, splits to %q", in, s, back)
		}
	})
}

func TestChainEncoding(t *testing.T) {
	for _, s := range []string{"strace", `/p/my wrapper -o "x y" 'z' 100%`, "", "ü\t\n"} {
		enc := EncodeChain(s)
		if strings.ContainsAny(enc, " \t\n\r'\"") {
			t.Errorf("EncodeChain(%q) = %q needs quoting", s, enc)
		}
		if back, err := DecodeChain(enc); err != nil || back != s {
			t.Errorf("DecodeChain(EncodeChain(%q)) = %q, %v", s, back, err)
		}
	}
	if EncodeChain("/usr/bin/strace") != "/usr/bin/strace" {
		t.Error("plain paths should stay readable")
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExpandNestedAndBounded(t *testing.T) {
	dir := t.TempDir()
	inner := writeFile(t, dir, "inner", "x \"y z\"\n")
	outer := writeFile(t, dir, "outer", "-a\n@"+inner+"\n-b\n")
	got, err := Expand([]string{"first", "@" + outer, "last"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"first", "-a", "x", "y z", "-b", "last"}; !slices.Equal(got, want) {
		t.Errorf("Expand = %q, want %q", got, want)
	}
	loop := filepath.Join(dir, "loop")
	writeFile(t, dir, "loop", "@"+loop+"\n")
	if _, err := Expand([]string{"@" + loop}); err == nil || !strings.Contains(err.Error(), "nested deeper") {
		t.Errorf("self-including response file: err = %v", err)
	}
	if _, err := Expand([]string{"@" + filepath.Join(dir, "missing")}); err == nil {
		t.Error("missing response file accepted")
	}
}

// cmd/go puts everything after argv[0] of the -toolexec command into the
// response file: graalgo-tool's -chain, the tool path, the tool's args.
// The stock tool must get a file without the graalgo-tool prefix.
func TestParseInvocationResponseFile(t *testing.T) {
	dir := t.TempDir()
	toolArgs := []string{"-o", "out dir/_pkg_.a", "-p", "example.com/m", "-D", `we"ird`, "a.go"}
	rf := writeFile(t, dir, "args", string(EncodeArgs(append([]string{"-chain", EncodeChain(`strace -o "x y"`), "/goroot/pkg/tool/linux_amd64/compile"}, toolArgs...))))
	inv, err := ParseInvocation([]string{"@" + rf})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"strace", "-o", "x y"}; !slices.Equal(inv.Chain, want) {
		t.Errorf("Chain = %q, want %q", inv.Chain, want)
	}
	if inv.Tool != "/goroot/pkg/tool/linux_amd64/compile" {
		t.Errorf("Tool = %q", inv.Tool)
	}
	if !slices.Equal(inv.Expanded, toolArgs) {
		t.Errorf("Expanded = %q, want %q", inv.Expanded, toolArgs)
	}
	if !slices.Equal(inv.Args, []string{"@" + rf}) || inv.ResponseFile != rf {
		t.Errorf("Args = %q, ResponseFile = %q", inv.Args, inv.ResponseFile)
	}
	data, err := os.ReadFile(rf)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(EncodeArgs(toolArgs)) {
		t.Errorf("rewritten response file = %q", data)
	}
	if got := inv.Command(); !slices.Equal(got, []string{"strace", "-o", "x y", "/goroot/pkg/tool/linux_amd64/compile", "@" + rf}) {
		t.Errorf("Command = %q", got)
	}
}

func TestParseInvocationPlain(t *testing.T) {
	inv, err := ParseInvocation([]string{"/t/link", "-o", "x", "main.a"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Chain != nil || inv.Tool != "/t/link" || !slices.Equal(inv.Args, []string{"-o", "x", "main.a"}) || inv.ResponseFile != "" {
		t.Errorf("got %+v", inv)
	}
	for _, bad := range [][]string{{"-chain"}, {"-chain", "x"}, {"-chain", "'open", "t"}, {"-chain", "%zz", "t"}} {
		if _, err := ParseInvocation(bad); err == nil {
			t.Errorf("ParseInvocation(%q) accepted", bad)
		}
	}
}
