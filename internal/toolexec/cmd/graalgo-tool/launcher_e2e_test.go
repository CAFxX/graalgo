package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"graalgo-toolexec/bundle"
	"graalgo-toolexec/release"
)

// home is an installed GRAALGO_HOME (design §1) whose goroot is a symlink
// to the stock GOROOT of the tracked release.
type home struct {
	t       *testing.T
	dir     string
	cache   string
	toolLog string
	mod     string
	runner  string
}

func installHome(t *testing.T) *home {
	t.Helper()
	if testing.Short() {
		t.Skip("builds graalgo and runs the go command; skipped in -short mode")
	}
	h := &home{t: t, dir: t.TempDir(), cache: t.TempDir()}
	h.toolLog = filepath.Join(h.dir, "tool.log")
	for _, b := range []struct{ out, pkg, ldflags string }{
		{"libexec/compile", ".", ""},
		{"libexec/graalgo-stub", "../graalgo-stub", "-buildid=" + bundle.BuildIDPlaceholder},
		{"bin/graalgo", "../graalgo-launcher", ""},
	} {
		args := []string{"build", "-o", filepath.Join(h.dir, b.out)}
		if b.ldflags != "" {
			args = append(args, "-ldflags="+b.ldflags)
		}
		if out, err := goCmd(t, ".", nil, append(args, b.pkg)...); err != nil {
			t.Fatalf("build %s: %v\n%s", b.pkg, err, out)
		}
	}
	// GRAALGO_HOME/goroot is the upstream distribution with the fork
	// applied (design §5.1); the shallow form suffices to build packages.
	if err := loadFork(t).Assemble(filepath.Join(h.dir, "goroot"), true); err != nil {
		t.Fatal(err)
	}
	h.runner = filepath.Join(h.dir, "fake-runner")
	if err := os.WriteFile(h.runner, []byte("#!/bin/sh\necho \"runner $*\"\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.mod = t.TempDir()
	h.write("go.mod", "module example.com/skel\n\ngo 1.27\n")
	h.write("main.go", "package main\n\nimport \"example.com/skel/lib\"\n\nfunc main() {\n\tprintln(lib.Answer())\n}\n")
	h.write("lib/lib.go", "package lib\n\nfunc Answer() int {\n\treturn 42\n}\n")
	h.write("lib/pure.go", "//go:build purego\n\npackage lib\n\nconst Pure = true\n")
	h.write("lib/notpure.go", "//go:build !purego\n\npackage lib\n\nconst Pure = false\n")
	return h
}

func (h *home) write(name, content string) {
	p := filepath.Join(h.mod, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// graalgo runs the launcher in the module with a clean go environment.
func (h *home) graalgo(extraEnv []string, args ...string) (string, error) {
	h.t.Helper()
	os.Remove(h.toolLog)
	cmd := exec.Command(filepath.Join(h.dir, "bin", "graalgo"), args...)
	cmd.Dir = h.mod
	cmd.Env = append(os.Environ(), "GOCACHE="+h.cache, "GOFLAGS=", "GOENV=off", "GRAALGO_HOME=",
		"GOTOOLCHAIN=", "CGO_ENABLED=", "GRAALGO_TOOL_LOG="+h.toolLog, "GRAALGO_RUNNER="+h.runner)
	// Unset (not empty) GOTOOLCHAIN/CGO_ENABLED, so the launcher's defaults apply.
	cmd.Env = withoutKeys(cmd.Env, "GOTOOLCHAIN", "CGO_ENABLED")
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func withoutKeys(env []string, keys ...string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, d := range keys {
			drop = drop || k == d
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

func (h *home) log() []string {
	data, _ := os.ReadFile(h.toolLog)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (h *home) ran(tool string) bool {
	for _, l := range h.log() {
		if strings.HasPrefix(l, tool+" ") && !strings.HasSuffix(l, " -V=full") {
			return true
		}
	}
	return false
}

// The launcher's identity: go env shows graalgo's injection, go version
// carries graalgo's version, purego is in effect.
func TestLauncherIdentity(t *testing.T) {
	h := installHome(t)
	out, err := h.graalgo(nil, "env", "GOROOT", "GOFLAGS", "CGO_ENABLED", "GOTOOLCHAIN")
	if err != nil {
		t.Fatalf("graalgo env: %v\n%s", err, out)
	}
	want := filepath.Join(h.dir, "goroot") + "\n-toolexec=" + filepath.Join(h.dir, "libexec/compile") + " -tags=purego\n0\nlocal\n"
	if out != want {
		t.Errorf("graalgo env:\n%s\nwant:\n%s", out, want)
	}
	out, err = h.graalgo(nil, "version")
	if err != nil || out != "go version "+release.GoVersion+" linux/amd64 graalgo-"+release.Version+"\n" {
		t.Errorf("graalgo version = %q, %v", out, err)
	}
	out, err = h.graalgo(nil, "list", "-f", "{{.GoFiles}}", "./lib")
	if err != nil || strings.TrimSpace(out) != "[lib.go pure.go]" {
		t.Errorf("purego default: go list = %q, %v", out, err)
	}
}

// graalgo build/run go through graalgo-tool; go run's exit status and the
// program's arguments pass through the stub and the runner.
func TestLauncherBuildAndRun(t *testing.T) {
	h := installHome(t)
	exe := filepath.Join(t.TempDir(), "skel")
	if out, err := h.graalgo(nil, "build", "-o", exe, "."); err != nil {
		t.Fatalf("graalgo build: %v\n%s", err, out)
	}
	b, err := bundle.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	// The two user packages, the facade runtime and its three imports.
	if b.Main != "example.com/skel" || len(b.Packages) != 6 {
		t.Errorf("bundle main=%q packages=%d", b.Main, len(b.Packages))
	}
	out, err := h.graalgo(nil, "run", ".", "x", "-toolexec=y")
	if exitCode(err) != 1 || !strings.Contains(out, "runner ") || !strings.Contains(out, " x -toolexec=y\n") || !strings.Contains(out, "exit status 7") {
		t.Errorf("graalgo run: exit %v\n%s", err, out)
	}
	// A compile error exits 1 with gc's diagnostics.
	h.write("lib/bad.go", "package lib\n\nvar _ int = \"s\"\n")
	out, err = h.graalgo(nil, "build", "-o", exe, ".")
	if exitCode(err) != 1 || !strings.Contains(out, "cannot use \"s\"") {
		t.Errorf("ill-typed build: exit %v\n%s", err, out)
	}
}

// A user -toolexec is chained: it wraps every stock tool run (seeing the
// same "<tool> <args>" argv as under stock go), and graalgo-tool still
// produces the graalgo build.
func TestLauncherChainsUserToolexec(t *testing.T) {
	h := installHome(t)
	userLog := filepath.Join(t.TempDir(), "user.log")
	wrapper := filepath.Join(t.TempDir(), "my wrapper")
	script := "#!/bin/sh\necho \"$(basename \"$1\") $2\" >> \"$USER_LOG\"\nexec \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "skel")
	for _, viaGOFLAGS := range []bool{false, true} {
		os.Remove(userLog)
		env := []string{"USER_LOG=" + userLog}
		args := []string{"build", "-a", "-o", exe}
		if viaGOFLAGS {
			env = append(env, `GOFLAGS='-toolexec="`+wrapper+`"'`)
		} else {
			args = append(args, "-toolexec", "'"+wrapper+"'")
		}
		if out, err := h.graalgo(env, append(args, ".")...); err != nil {
			t.Fatalf("GOFLAGS=%v: %v\n%s", viaGOFLAGS, err, out)
		}
		data, _ := os.ReadFile(userLog)
		if !strings.Contains(string(data), "compile -V=full\n") || !regexp.MustCompile(`(?m)^compile -o$`).Match(data) {
			t.Errorf("GOFLAGS=%v: user wrapper did not wrap compile:\n%s", viaGOFLAGS, data)
		}
		if _, err := bundle.ReadFile(exe); err != nil {
			t.Errorf("GOFLAGS=%v: not a graalgo build: %v", viaGOFLAGS, err)
		}
	}
}

// cmd/go switches to response files above 30 KiB of arguments because
// graalgo-tool runs under the basename "compile" (design §1). Both the
// compile and the link step must still work, and the IR pass must see the
// expanded command line.
func TestResponseFiles(t *testing.T) {
	h := installHome(t)
	long := strings.Repeat("x", 31<<10)
	exe := filepath.Join(t.TempDir(), "skel")
	out, err := h.graalgo(nil, "build", "-o", exe, "-gcflags=-D=/"+long, "-ldflags=-X=main.unused="+long, ".")
	if err != nil {
		t.Fatalf("build with long argv: %v\n%s", err, out)
	}
	b, err := bundle.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if b.Main != "example.com/skel" {
		t.Errorf("bundle main = %q", b.Main)
	}
	sawCompile, sawLink := false, false
	for _, l := range h.log() {
		sawCompile = sawCompile || (strings.HasPrefix(l, "compile ") && strings.Contains(l, "-D=/"+long) && strings.Contains(l, "-p main "))
		sawLink = sawLink || (strings.HasPrefix(l, "link ") && strings.Contains(l, "main.unused="+long))
	}
	if !sawCompile || !sawLink {
		t.Errorf("expanded long command lines not seen (compile %v, link %v)", sawCompile, sawLink)
	}
}

// -race, -cover and fuzzing fail loudly instead of building a binary that
// would pass vacuously (design §2.5, §2.6).
func TestUnsupportedInstrumentationFailsLoudly(t *testing.T) {
	h := installHome(t)
	h.write("lib/lib_test.go", "package lib\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n\nfunc FuzzA(f *testing.F) { f.Fuzz(func(t *testing.T, b []byte) {}) }\n")
	out, err := h.graalgo(nil, "test", "-cover", "./lib")
	if err == nil || !strings.Contains(out, "coverage (-cover) not yet supported") {
		t.Errorf("go test -cover: %v\n%s", err, out)
	}
	if !h.ran("cover") {
		t.Error("cover tool was not passed through before compile refused")
	}
	out, err = h.graalgo(nil, "test", "-fuzz=FuzzA", "-fuzztime=1x", "./lib")
	if err == nil || !strings.Contains(out, "fuzzing instrumentation") {
		t.Errorf("go test -fuzz: %v\n%s", err, out)
	}
	exe := filepath.Join(t.TempDir(), "skel")
	out, err = h.graalgo([]string{"CGO_ENABLED=1"}, "build", "-race", "-o", exe, ".")
	if err == nil || !strings.Contains(out, "no race detector; set GRAALGO_RACE=ignore") {
		t.Errorf("go build -race: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(exe); statErr == nil {
		t.Error("-race build wrote an executable")
	}
	out, err = h.graalgo([]string{"CGO_ENABLED=1", "GRAALGO_RACE=ignore"}, "build", "-race", "-o", exe, ".")
	if err != nil {
		t.Errorf("GRAALGO_RACE=ignore: %v\n%s", err, out)
	}
}

// vet, asm, cgo and preprofile pass through to the stock tools.
func TestPassThroughTools(t *testing.T) {
	h := installHome(t)
	out, err := h.graalgo(nil, "vet", "./...")
	if err != nil || !h.ran("vet") {
		t.Errorf("go vet: %v (vet ran: %v)\n%s", err, h.ran("vet"), out)
	}

	h.write("asmpkg/a.go", "package asmpkg\n\nfunc Add(a, b int) int\n")
	h.write("asmpkg/a_amd64.s", "#include \"textflag.h\"\n\nTEXT ·Add(SB),NOSPLIT,$0-24\n\tMOVQ a+0(FP), AX\n\tADDQ b+8(FP), AX\n\tMOVQ AX, ret+16(FP)\n\tRET\n")
	out, err = h.graalgo(nil, "build", "./asmpkg")
	if err != nil || !h.ran("asm") {
		t.Errorf("asm package: %v (asm ran: %v)\n%s", err, h.ran("asm"), out)
	}

	h.write("cgopkg/c.go", "package cgopkg\n\n// int two(void) { return 2; }\nimport \"C\"\n\nfunc Two() int { return int(C.two()) }\n")
	out, err = h.graalgo([]string{"CGO_ENABLED=1"}, "vet", "./cgopkg")
	if err != nil || !h.ran("cgo") {
		t.Errorf("cgo package: %v (cgo ran: %v)\n%s", err, h.ran("cgo"), out)
	}

	prof := filepath.Join(h.mod, "default.pgo")
	f, err := os.Create(prof)
	if err != nil {
		t.Fatal(err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(50 * time.Millisecond); time.Now().Before(end); {
	}
	pprof.StopCPUProfile()
	f.Close()
	exe := filepath.Join(t.TempDir(), "skel")
	out, err = h.graalgo(nil, "build", "-pgo=auto", "-o", exe, ".")
	if err != nil || !h.ran("preprofile") {
		t.Errorf("-pgo build: %v (preprofile ran: %v)\n%s", err, h.ran("preprofile"), out)
	}
}

// GG-0028 / design §2.1: the -V=full answer is the stock line plus
// graalgo's suffix, it changes when graalgo-tool changes (so its cache
// entries are invalidated), and a tool of another release is refused.
func TestVersionProbeIdentity(t *testing.T) {
	h := installHome(t)
	tool := filepath.Join(h.dir, "libexec/compile")
	goroot := filepath.Join(h.dir, "goroot")
	probe := func(name string) string {
		out, err := exec.Command(tool, filepath.Join(goroot, "pkg/tool/linux_amd64", name), "-V=full").Output()
		if err != nil {
			t.Fatalf("%s -V=full: %v", name, err)
		}
		return string(out)
	}
	re := regexp.MustCompile(`^(compile|link|asm) version ` + regexp.QuoteMeta(release.GoVersion) + ` graalgo-` + regexp.QuoteMeta(irVersion) + `-([0-9a-f]{16})\n$`)
	var hashes = map[string]string{}
	for _, name := range []string{"compile", "link", "asm"} {
		m := re.FindStringSubmatch(probe(name))
		if m == nil {
			t.Fatalf("%s -V=full = %q", name, probe(name))
		}
		hashes[name] = m[2]
	}
	if hashes["compile"] != hashes["asm"] || hashes["compile"] == hashes["link"] {
		t.Errorf("hashes: %v (link must also cover the stub)", hashes)
	}

	exe := filepath.Join(t.TempDir(), "skel")
	if out, err := h.graalgo(nil, "build", "-o", exe, "."); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	if out, err := h.graalgo(nil, "build", "-o", exe, "."); err != nil || h.ran("compile") || h.ran("link") {
		t.Fatalf("warm build ran tools (%v):\n%s\n%s", err, strings.Join(h.log(), "\n"), out)
	}
	// A different graalgo-tool binary: every graalgo entry is stale.
	if out, err := goCmd(t, ".", nil, "build", "-ldflags=-X=graalgo-toolexec/release.Version=other", "-o", tool, "."); err != nil {
		t.Fatalf("rebuild tool: %v\n%s", err, out)
	}
	if out, err := h.graalgo(nil, "build", "-o", exe, "."); err != nil || !h.ran("compile") || !h.ran("link") {
		t.Errorf("build after a tool change did not rerun compile and link (%v):\n%s", err, out)
	}

	// A toolchain tool outside graalgo's GOROOT (GOTOOLCHAIN switch).
	foreign := filepath.Join(t.TempDir(), "compile")
	if err := os.WriteFile(foreign, []byte("#!/bin/sh\necho 'compile version go1.99.0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(tool, foreign, "-V=full").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "this graalgo tracks "+release.GoVersion) {
		t.Errorf("foreign compile accepted: %v\n%s", err, out)
	}
	// Outside an installed layout (development), the version line itself
	// is checked.
	dev := filepath.Join(t.TempDir(), "graalgo-tool")
	if err := os.Link(tool, dev); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command(dev, foreign, "-V=full").CombinedOutput()
	if err == nil || !strings.Contains(string(out), `reports "compile version go1.99.0"`) {
		t.Errorf("foreign compile version accepted: %v\n%s", err, out)
	}
	// And compile -goversion of another release.
	out, err = exec.Command(dev, filepath.Join(goroot, "pkg/tool/linux_amd64/compile"), "-goversion", "go1.99.0", "-p", "x", "-o", "/dev/null", "x.go").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "compile -goversion go1.99.0") {
		t.Errorf("foreign -goversion accepted: %v\n%s", err, out)
	}
}

func exitCode(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}
