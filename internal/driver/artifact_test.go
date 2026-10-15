package driver

import (
	"archive/tar"
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeSrc(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// extractPayload pulls the base64 tar payload out of a rendered artifact
// script and returns the name->content map from the tar.
func extractPayload(t *testing.T, script string) map[string]string {
	t.Helper()
	lines := strings.Split(script, "\n")
	var b64 []string
	inPayload := false
	for _, l := range lines {
		if !inPayload {
			if strings.Contains(l, "<<'GRAALGO_PAYLOAD_EOF'") {
				inPayload = true
			}
			continue
		}
		if l == "GRAALGO_PAYLOAD_EOF" {
			break
		}
		b64 = append(b64, l)
	}
	if !inPayload {
		t.Fatal("no payload heredoc found in artifact script")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(b64, ""))
	if err != nil {
		t.Fatalf("payload is not valid base64: %v", err)
	}
	tr := tar.NewReader(strings.NewReader(string(raw)))
	got := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("cannot read payload tar: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got[hdr.Name] = string(body)
	}
	return got
}

func TestEmitRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeSrc(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	utilPath := writeSrc(t, dir, "util.go", "package main\n\nfunc helper() int { return 42 }\n")

	out := filepath.Join(t.TempDir(), "prog")
	err := Emit(Artifact{
		OutputPath:  out,
		GraalgoRoot: "/opt/graalgo",
		Files: []ProgramFile{
			{Name: "main.go", Path: mainPath},
			{Name: "util.go", Path: utilPath},
		},
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

	script, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(script)
	if !strings.HasPrefix(s, "#!/usr/bin/env bash") {
		t.Errorf("artifact missing bash shebang")
	}
	if !strings.Contains(s, "/opt/graalgo") {
		t.Errorf("artifact does not bake in the graalgo root")
	}
	if !strings.Contains(s, "GRAALGO_HOME") {
		t.Errorf("artifact does not honor GRAALGO_HOME override")
	}
	// The script must be syntactically valid bash.
	if out, err := exec.Command("bash", "-n", out).CombinedOutput(); err != nil {
		t.Errorf("bash -n failed on artifact: %v\n%s", err, out)
	}

	payload := extractPayload(t, s)
	if len(payload) != 2 {
		t.Fatalf("payload has %d files, want 2", len(payload))
	}
	for name, path := range map[string]string{"main.go": mainPath, "util.go": utilPath} {
		want, _ := os.ReadFile(path)
		if payload[name] != string(want) {
			t.Errorf("payload %s mismatch", name)
		}
	}
}

func TestEmitDuplicateBaseName(t *testing.T) {
	dir := t.TempDir()
	a := writeSrc(t, dir, "a.go", "package main\n")
	sub := filepath.Join(dir, "sub")
	b := writeSrc(t, sub, "a.go", "package main\n")
	err := Emit(Artifact{
		OutputPath:  filepath.Join(t.TempDir(), "prog"),
		GraalgoRoot: "/opt/graalgo",
		Files: []ProgramFile{
			{Name: "a.go", Path: a},
			{Name: "a.go", Path: b},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("Emit with duplicate base names = %v, want duplicate error", err)
	}
}

func TestEmitNoFiles(t *testing.T) {
	err := Emit(Artifact{OutputPath: filepath.Join(t.TempDir(), "prog"), GraalgoRoot: "/x"})
	if err == nil {
		t.Errorf("Emit with no files succeeded, want error")
	}
}

func TestShQuote(t *testing.T) {
	cases := map[string]string{
		"simple":     "'simple'",
		"with space": "'with space'",
		"o'clock":    "'o'\\''clock'",
	}
	for in, want := range cases {
		if got := shQuote(in); got != want {
			t.Errorf("shQuote(%q) = %q, want %q", in, got, want)
		}
	}

	dcases := map[string]string{
		"simple":  "simple",
		"o'clock": "o'clock", // single quotes need no escaping in "..."
		// input: a"b\c$d`e
		"a\"b\\c$d`e": "a\\\"b\\\\c\\$d\\`e",
	}
	for in, want := range dcases {
		if got := shDQuote(in); got != want {
			t.Errorf("shDQuote(%q) = %q, want %q", in, got, want)
		}
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
