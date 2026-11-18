package emit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"graalgo-toolexec/ir"
)

const embedMain = `package main

import (
	"embed"
	"fmt"
	"unsafe"
)

//go:embed hello.txt
var s string

//go:embed big.bin
var b []byte

//go:embed hello.txt static
var fsys embed.FS

type file struct {
	name string
	data string
	hash [16]byte
}

func main() {
	fmt.Printf("s %q\n", s)
	fmt.Printf("b %d %x\n", len(b), b[:8])
	files := *(**[]file)(unsafe.Pointer(&fsys))
	for _, f := range *files {
		fmt.Printf("f %s %d %x %x\n", f.name, len(f.data), f.data[:min(len(f.data), 8)], f.hash)
	}
}
`

// //go:embed lowering against gc: gc's own embed.FS image (read back
// through unsafe) and the string/[]byte variables must equal what the
// emitted initializers construct from DATA — names, order, contents and
// content hashes (small files and the >1 KiB hashing path).
func TestEmbed(t *testing.T) {
	if testing.Short() {
		t.Skip("runs gc")
	}
	dir := t.TempDir()
	big := make([]byte, 3000)
	for i := range big {
		big[i] = byte(i * 7)
	}
	files := map[string][]byte{
		"go.mod":           []byte("module e\ngo 1.27\n"),
		"main.go":          []byte(embedMain),
		"hello.txt":        []byte("hello, embed\n"),
		"big.bin":          big,
		"static/a.txt":     []byte("A"),
		"static/sub/z.txt": []byte("zz"),
		"static/sub/b.txt": []byte(""),
		"static/c.bin":     big[:1025],
	}
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	want, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gc: %v\n%s", err, want)
	}
	cfg := &EmbedConfig{
		Patterns: map[string][]string{
			"hello.txt": {"hello.txt"},
			"big.bin":   {"big.bin"},
			"static":    {"static/a.txt", "static/c.bin", "static/sub/b.txt", "static/sub/z.txt"},
		},
		Files: map[string]string{},
	}
	for name := range files {
		cfg.Files[name] = filepath.Join(dir, name)
	}
	f := emitSrcCfg(t, embedMain, func(c *Config) { c.Embed = cfg })
	got := embedDump(t, f)
	if got != string(want) {
		t.Errorf("emitted embed data differs from gc's\n got:\n%s\nwant:\n%s", got, want)
	}
}

// embedDump renders the emitted initializers like embedMain's output.
func embedDump(t *testing.T, f *ir.File) string {
	body := funcBody(t, f, "init·vars")
	data := func(x ir.Expr) string {
		switch d := x.(type) {
		case *ir.DataExpr:
			return string(f.Data[d.Off : d.Off+d.Len])
		case *ir.ConvertExpr:
			if dd, ok := d.X.(*ir.DataExpr); ok {
				return string(f.Data[dd.Off : dd.Off+dd.Len])
			}
		}
		t.Fatalf("not a DATA reference: %#v", x)
		return ""
	}
	var out strings.Builder
	for _, st := range body.Body {
		as := st.(*ir.AssignStmt)
		name := f.Objects[as.Lhs[0].(*ir.GlobalExpr).Obj].Name
		switch name {
		case "s":
			fmt.Fprintf(&out, "s %q\n", data(as.Rhs[0]))
		case "b":
			d := data(as.Rhs[0])
			fmt.Fprintf(&out, "b %d %x\n", len(d), d[:8])
		case "fsys":
			lit := as.Rhs[0].(*ir.StructLitExpr).Fields[0].Value.(*ir.AddrExpr).X.(*ir.ArrayLitExpr)
			for _, el := range lit.Elems {
				var fname, fdata string
				var hash [16]byte
				for _, fi := range el.Value.(*ir.StructLitExpr).Fields {
					switch fi.Path[0] {
					case 0:
						fname = fi.Value.(*ir.ConstExpr).Value.Text
					case 1:
						fdata = data(fi.Value)
					case 2:
						for _, he := range fi.Value.(*ir.ArrayLitExpr).Elems {
							v, _ := strconv.Atoi(he.Value.(*ir.ConstExpr).Value.Text)
							hash[he.Index] = byte(v)
						}
					}
				}
				fmt.Fprintf(&out, "f %s %d %x %x\n", fname, len(fdata), fdata[:min(len(fdata), 8)], hash)
			}
		}
	}
	return out.String()
}
