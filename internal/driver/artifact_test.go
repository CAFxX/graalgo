package driver

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// validWireJSON is a minimal well-formed frontend/v1 document: the
// shape Emit requires (protocol version, generator version, package
// name and files). It is deliberately not a real frontend dump — Emit
// checks the shape, not the full contract.
const validWireJSON = `{"frontend":"v1","generator":{"tool":"gofrontend","version":"0.7.0","goroot":"go1.27.1"},"package":{"name":"main","files":["main.go"],"lang":"go1.27"},"types":[],"objects":[],"funcs":[],"globals":[]}`

// TestEmitProducesBinary verifies that Emit produces a real executable
// binary (not a shell script) embedding the frontend output.
func TestEmitProducesBinary(t *testing.T) {
	// Minimal fake frontend JSON in the frontend/v1 shape Emit
	// requires (see validWireJSON); content beyond the shape is not
	// validated here.
	wireJSON := []byte(validWireJSON)

	out := filepath.Join(t.TempDir(), "prog")
	err := Emit(Artifact{
		OutputPath:  out,
		GraalgoRoot: "/opt/graalgo",
		WireJSON:    wireJSON,
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Errorf("artifact %s is not executable (mode %v)", out, fi.Mode())
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// Must be an ELF binary, not a shell script.
	if !bytes.HasPrefix(data, []byte("\x7fELF")) {
		t.Errorf("artifact is not an ELF binary (first bytes: %q)", data[:min(32, len(data))])
	}
	if bytes.HasPrefix(data, []byte("#!")) {
		t.Errorf("artifact is a script (shebang), not a binary")
	}
	// The graalgo root must be baked into the binary.
	if !bytes.Contains(data, []byte("/opt/graalgo")) {
		t.Errorf("artifact does not bake in the graalgo root")
	}
	// The launcher must honor GRAALGO_HOME (check the string is present
	// in the binary; the actual env-var logic is exercised at run time).
	if !bytes.Contains(data, []byte("GRAALGO_HOME")) {
		t.Errorf("artifact does not reference GRAALGO_HOME")
	}
	// The launcher must use --wire-input (not re-run the frontend).
	if !bytes.Contains(data, []byte("--wire-input")) {
		t.Errorf("artifact does not use --wire-input")
	}
}

func TestEmitNoOutput(t *testing.T) {
	err := Emit(Artifact{GraalgoRoot: "/x", WireJSON: []byte("{}")})
	if err == nil || !strings.Contains(err.Error(), "output") {
		t.Errorf("Emit with no output path = %v, want output error", err)
	}
}

func TestEmitNoWireJSON(t *testing.T) {
	err := Emit(Artifact{OutputPath: filepath.Join(t.TempDir(), "prog"), GraalgoRoot: "/x"})
	if err == nil || !strings.Contains(err.Error(), "frontend output") {
		t.Errorf("Emit with no wire JSON = %v, want frontend output error", err)
	}
}

func TestEmitNoRoot(t *testing.T) {
	err := Emit(Artifact{OutputPath: filepath.Join(t.TempDir(), "prog"), WireJSON: []byte("{}")})
	if err == nil || !strings.Contains(err.Error(), "graalgo root") {
		t.Errorf("Emit with no graalgo root = %v, want root error", err)
	}
}

func TestDefaultOutputName(t *testing.T) {
	// Explicit -o wins.
	if got, err := DefaultOutputName("custom", nil); err != nil || got != "custom" {
		t.Errorf("explicit -o: got %q, %v", got, err)
	}
	// First .go file: base name minus extension.
	if got, err := DefaultOutputName("", []string{"hello.go"}); err != nil || got != "hello" {
		t.Errorf("file operand: got %q, %v", got, err)
	}
	if got, err := DefaultOutputName("", []string{"/tmp/x/myprog.go"}); err != nil || got != "myprog" {
		t.Errorf("file path operand: got %q, %v", got, err)
	}
	// Directory operand: base of the directory.
	if got, err := DefaultOutputName("", []string{"./mydir/"}); err != nil || got != "mydir" {
		t.Errorf("dir operand: got %q, %v", got, err)
	}
	// No operands: base of the current directory.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	proj := filepath.Join(tmp, "coolproj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if got, err := DefaultOutputName("", nil); err != nil || got != "coolproj" {
		t.Errorf("no operands: got %q, %v", got, err)
	}
}

// TestEmitBinaryRuns is a smoke test that the produced binary actually
// executes. It uses a fake backend script that records its arguments,
// verifying the launcher passes --wire-input correctly. This does not
// require a real graalgo backend.
func TestEmitBinaryRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary execution test in short mode")
	}

	tmpDir := t.TempDir()

	// Fake backend: a script that writes its args to a file, copies the
	// wire-input file aside (the launcher deletes it on exit), and exits 0.
	// We fake GRAALGO_HOME to point at a dir containing
	// internal/backend/graalgo-sync-run.
	fakeRoot := filepath.Join(tmpDir, "fake-root")
	backendDir := filepath.Join(fakeRoot, "internal", "backend")
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(tmpDir, "args.txt")
	wireCopy := filepath.Join(tmpDir, "wire-copy.json")
	backendScript := "#!/usr/bin/env bash\n" +
		"printf '%s\\n' \"$@\" > " + argsFile + "\n" +
		"cp \"$2\" " + wireCopy + "\n" + // $2 is the --wire-input path
		"exit 0\n"
	backendPath := filepath.Join(backendDir, "graalgo-sync-run")
	if err := os.WriteFile(backendPath, []byte(backendScript), 0o755); err != nil {
		t.Fatal(err)
	}

	wireJSON := []byte(validWireJSON)
	out := filepath.Join(tmpDir, "prog")
	if err := Emit(Artifact{
		OutputPath:  out,
		GraalgoRoot: "/nonexistent-baked-in",
		WireJSON:    wireJSON,
	}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	// Run the artifact with GRAALGO_HOME pointing at the fake root.
	cmd := exec.Command(out, "hello", "world")
	cmd.Env = append(os.Environ(), "GRAALGO_HOME="+fakeRoot)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("artifact run failed: %v\n%s", err, out)
	}

	// Verify the backend received --wire-input with a real JSON file.
	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("backend was not invoked (no args file): %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(argsData)), "\n")
	if len(args) < 4 {
		t.Fatalf("backend args = %v, want at least [--wire-input FILE -- argv0 ...]", args)
	}
	if args[0] != "--wire-input" {
		t.Errorf("args[0] = %q, want --wire-input", args[0])
	}
	// The backend copied the wire file before the launcher cleaned up.
	wireData, err := os.ReadFile(wireCopy)
	if err != nil {
		t.Errorf("cannot read preserved wire-input copy: %v", err)
	} else if string(wireData) != string(wireJSON) {
		t.Errorf("wire-input content mismatch: got %q, want %q", wireData, wireJSON)
	}
	if args[2] != "--" {
		t.Errorf("args[2] = %q, want --", args[2])
	}
	// argv[0] for the program should be the artifact's absolute path.
	if !filepath.IsAbs(args[3]) {
		t.Errorf("program argv[0] = %q, want absolute path", args[3])
	}
	// User args forwarded.
	if len(args) < 6 || args[4] != "hello" || args[5] != "world" {
		t.Errorf("user args not forwarded: %v", args)
	}
}

// TestEmitRejectsTruncatedWireJSON: R15 — truncated, wrong-version,
// and wrong-shape wire JSON must fail the build in Emit, never ship
// as an artifact that fails only at run time.
func TestEmitRejectsTruncatedWireJSON(t *testing.T) {
	cases := []struct {
		name string
		wire string
	}{
		{"truncated mid-object", `{"frontend":"v1","gener`},
		{"truncated mid-array", `{"frontend":"v1","generator":{"tool":"gofrontend","version":"0.7.0"},"package":{"name":"main","files":["main.go"`},
		{"empty object", `{}`},
		{"wrong version", `{"frontend":"v2","generator":{"version":"0.7.0"},"package":{"name":"main","files":["main.go"]}}`},
		{"missing generator version", `{"frontend":"v1","generator":{},"package":{"name":"main","files":["main.go"]}}`},
		{"missing package name", `{"frontend":"v1","generator":{"version":"0.7.0"},"package":{"files":["main.go"]}}`},
		{"package without files", `{"frontend":"v1","generator":{"version":"0.7.0"},"package":{"name":"main","files":[]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Emit(Artifact{
				OutputPath:  filepath.Join(t.TempDir(), "prog"),
				GraalgoRoot: "/opt/graalgo",
				WireJSON:    []byte(tc.wire),
			})
			if err == nil {
				t.Fatalf("Emit with %s wire JSON succeeded, want error", tc.name)
			}
			if !strings.Contains(err.Error(), "frontend output") {
				t.Errorf("Emit error = %v, want it to mention frontend output", err)
			}
		})
	}
}

// TestLauncherSourceQuotesRoot: R14 — a root containing a quote,
// backslash, or newline must render as a valid Go string literal
// holding exactly that root, never as injected source.
func TestLauncherSourceQuotesRoot(t *testing.T) {
	roots := []string{
		`/tmp/a"b\c`,
		"/tmp/with\nnewline",
		`/tmp/${HOME}/x`,
		"/opt/graalgo",
	}
	for _, root := range roots {
		src := launcherSource(root)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "launcher.go", src, 0)
		if err != nil {
			t.Fatalf("launcherSource(%q) does not parse: %v\n%s", root, err, src)
		}
		var got string
		found := false
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) == 0 || vs.Names[0].Name != "graalgoRoot" {
					continue
				}
				lit, ok := vs.Values[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("graalgoRoot is not a string literal in %q", root)
				}
				got, err = strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("launcherSource(%q): graalgoRoot const not found", root)
		}
		if got != root {
			t.Errorf("launcherSource(%q): literal decodes to %q", root, got)
		}
	}
}

// TestEmitWithQuotedRoot: R14 end to end — Emit must succeed (i.e.
// the generated launcher must compile) when the root contains a quote
// and a backslash; with raw %s interpolation the generated source
// would not compile.
func TestEmitWithQuotedRoot(t *testing.T) {
	root := `/tmp/a"b\c`
	out := filepath.Join(t.TempDir(), "prog")
	if err := Emit(Artifact{
		OutputPath:  out,
		GraalgoRoot: root,
		WireJSON:    []byte(validWireJSON),
	}); err != nil {
		t.Fatalf("Emit with quoted root: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(root)) {
		t.Errorf("artifact does not bake in the quoted root %q", root)
	}
}

// TestParseGoVersionOutput: P2 item 14 — the toolchain version check
// parses `go version` output, including release candidates, and
// rejects garbage loudly instead of defaulting to an assumed version.
func TestParseGoVersionOutput(t *testing.T) {
	major, minor, version, err := parseGoVersionOutput("go version go1.27.1 linux/amd64")
	if err != nil || major != 1 || minor != 27 || version != "1.27.1" {
		t.Errorf("go1.27.1: got %d.%d %q, %v", major, minor, version, err)
	}
	major, minor, _, err = parseGoVersionOutput("go version go1.27rc1 linux/amd64")
	if err != nil || major != 1 || minor != 27 {
		t.Errorf("go1.27rc1: got %d.%d, %v", major, minor, err)
	}
	// VETO finding 5 (P2): a devel toolchain ("go version devel
	// go1.28-...") must parse — it is newer than the floor, not
	// unparseable — instead of being rejected by Sscanf on "devel".
	major, minor, _, err = parseGoVersionOutput("go version devel go1.28-abc123 linux/amd64")
	if err != nil || major != 1 || minor != 28 {
		t.Errorf("devel go1.28: got %d.%d, %v", major, minor, err)
	}
	if _, _, _, err := parseGoVersionOutput("go version devel linux/amd64"); err == nil {
		t.Error("devel without a version token: got nil error, want error")
	}
	if _, _, _, err := parseGoVersionOutput("not a version"); err == nil {
		t.Error("garbage version output: got nil error, want error")
	}
}

// TestGoBinaryCandidatesOrder: VETO finding 5 (P2) — the toolchain
// "pin" must prefer the toolchain that built the driver, then the
// project's pinned ~/tools/go, and only then PATH, so a stale system
// go on PATH cannot shadow the pinned toolchain.
func TestGoBinaryCandidatesOrder(t *testing.T) {
	got := goBinaryCandidates("/build/goroot", "/home/u", "/usr/bin/go")
	want := []string{"/build/goroot/bin/go", "/home/u/tools/go/bin/go", "/usr/bin/go"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := goBinaryCandidates("", "", "/usr/bin/go"); len(got) != 1 || got[0] != "/usr/bin/go" {
		t.Errorf("PATH-only candidates = %v, want [/usr/bin/go]", got)
	}
	if got := goBinaryCandidates("", "", ""); len(got) != 0 {
		t.Errorf("empty candidates = %v, want []", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
