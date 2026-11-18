package launcher

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"graalgo-toolexec/argv"
	"graalgo-toolexec/release"
)

// fakeHome builds a GRAALGO_HOME whose goroot holds only what Compose
// inspects.
func fakeHome(t *testing.T, version string) string {
	t.Helper()
	home := t.TempDir()
	for name, content := range map[string]string{
		"goroot/VERSION": version + "\ntime 2026-01-01T00:00:00Z\n",
		"goroot/bin/go":  "#!/bin/sh\n",
		"goroot/go.env":  "GOTOOLCHAIN=auto\nGOPROXY=https://proxy.golang.org,direct\n",
		ToolRel:          "#!/bin/sh\n",
	} {
		p := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func compose(t *testing.T, home string, env []string, args ...string) *Plan {
	t.Helper()
	// A private, empty user config dir unless the test sets GOENV.
	base := []string{"HOME=" + t.TempDir(), "XDG_CONFIG_HOME=", "PATH=/usr/bin"}
	p, err := Compose(Request{Home: home, Args: args, Env: append(base, env...)})
	if err != nil {
		t.Fatalf("Compose(%q, %q): %v", env, args, err)
	}
	return p
}

func getenv(p *Plan, k string) (string, bool) {
	return newEnv(p.Env).lookup(k)
}

func goflags(t *testing.T, p *Plan) []string {
	t.Helper()
	v, _ := getenv(p, "GOFLAGS")
	f, err := argv.SplitQuoted(v)
	if err != nil {
		t.Fatalf("GOFLAGS %q: %v", v, err)
	}
	return f
}

func TestDefaults(t *testing.T) {
	home := fakeHome(t, release.GoVersion)
	p := compose(t, home, []string{"GOROOT=/elsewhere", "GOTOOLCHAIN_UNRELATED=1"}, "build", "./...")
	tool := filepath.Join(home, ToolRel)
	if want := []string{"-toolexec=" + tool, "-tags=purego"}; !slices.Equal(goflags(t, p), want) {
		t.Errorf("GOFLAGS = %q, want %q", goflags(t, p), want)
	}
	for k, want := range map[string]string{
		"GOROOT":       filepath.Join(home, GorootRel),
		"GOTOOLCHAIN":  "local", // overrides goroot/go.env's auto
		"CGO_ENABLED":  "0",
		"GRAALGO_HOME": home,
	} {
		if v, _ := getenv(p, k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	if p.Go != filepath.Join(home, "goroot/bin/go") {
		t.Errorf("Go = %q", p.Go)
	}
	if !slices.Equal(p.Argv, []string{"go", "build", "./..."}) || p.AppendVersion {
		t.Errorf("Argv = %q AppendVersion = %v", p.Argv, p.AppendVersion)
	}
	// GOROOT appears exactly once (no stale user value left before ours).
	n := 0
	for _, kv := range p.Env {
		if strings.HasPrefix(kv, "GOROOT=") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("GOROOT set %d times", n)
	}
}

func TestUserSettingsWin(t *testing.T) {
	home := fakeHome(t, release.GoVersion)
	p := compose(t, home, []string{"GOTOOLCHAIN=go1.27.1+path", "CGO_ENABLED=1"}, "env")
	if v, _ := getenv(p, "GOTOOLCHAIN"); v != "go1.27.1+path" {
		t.Errorf("GOTOOLCHAIN = %q", v)
	}
	if v, _ := getenv(p, "CGO_ENABLED"); v != "1" {
		t.Errorf("CGO_ENABLED = %q", v)
	}

	// `go env -w` settings live in the GOENV file: the launcher must not
	// shadow them with its defaults, and must build on the file's GOFLAGS.
	envFile := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(envFile, []byte("# go env -w\nGOTOOLCHAIN=go1.27.1\nCGO_ENABLED=1\nGOFLAGS=-mod=mod -tags=netgo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p = compose(t, home, []string{"GOENV=" + envFile}, "build")
	if v, ok := getenv(p, "GOTOOLCHAIN"); ok {
		t.Errorf("GOTOOLCHAIN set to %q although the GOENV file sets it", v)
	}
	if v, ok := getenv(p, "CGO_ENABLED"); ok {
		t.Errorf("CGO_ENABLED set to %q although the GOENV file sets it", v)
	}
	tool := filepath.Join(home, ToolRel)
	if want := []string{"-toolexec=" + tool, "-mod=mod", "-tags=netgo,purego"}; !slices.Equal(goflags(t, p), want) {
		t.Errorf("GOFLAGS = %q, want %q", goflags(t, p), want)
	}

	// The environment beats the file; GOENV=off ignores the file.
	p = compose(t, home, []string{"GOENV=" + envFile, "GOFLAGS=-v"}, "build")
	if want := []string{"-toolexec=" + tool, "-tags=purego", "-v"}; !slices.Equal(goflags(t, p), want) {
		t.Errorf("GOFLAGS = %q, want %q", goflags(t, p), want)
	}
	p = compose(t, home, []string{"GOENV=off"}, "build")
	if v, _ := getenv(p, "CGO_ENABLED"); v != "0" {
		t.Errorf("GOENV=off: CGO_ENABLED = %q", v)
	}
}

func TestGOFLAGSToolexecChained(t *testing.T) {
	home := fakeHome(t, release.GoVersion)
	tool := filepath.Join(home, ToolRel)
	for _, tc := range []struct {
		user  string
		chain string
	}{
		{"-toolexec=strace", "strace"},
		{"--toolexec=/usr/bin/time", "/usr/bin/time"},
		{`'-toolexec=strace -f -o log' -x`, "strace -f -o log"},
		{`"-toolexec='/my wrapper' -e 'trace=open'"`, "'/my wrapper' -e 'trace=open'"},
	} {
		p := compose(t, home, []string{"GOFLAGS=" + tc.user}, "build")
		var te []string
		for _, f := range goflags(t, p) {
			if v, ok := strings.CutPrefix(f, "-toolexec="); ok {
				te = append(te, v)
			}
		}
		if len(te) != 1 {
			t.Fatalf("GOFLAGS %q: %d -toolexec entries: %q", tc.user, len(te), goflags(t, p))
		}
		// cmd/go splits the -toolexec value with quoted.Split.
		got, err := argv.SplitQuoted(te[0])
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{tool, "-chain", argv.EncodeChain(tc.chain)}; !slices.Equal(got, want) {
			t.Errorf("GOFLAGS %q: toolexec fields = %q, want %q", tc.user, got, want)
		}
	}
	// An empty user -toolexec means "none": no chain.
	p := compose(t, home, []string{"GOFLAGS=-toolexec="}, "build")
	if want := []string{"-tags=purego", "-toolexec=" + tool}; !slices.Equal(goflags(t, p), want) {
		t.Errorf("GOFLAGS = %q, want %q", goflags(t, p), want)
	}
	if _, err := Compose(Request{Home: home, Args: []string{"build"}, Env: []string{`GOFLAGS='-toolexec=unterminated`}}); err == nil {
		t.Error("malformed GOFLAGS accepted")
	}
}

func TestArgvRewrite(t *testing.T) {
	home := fakeHome(t, release.GoVersion)
	tool := filepath.Join(home, ToolRel)
	chained := func(x string) string {
		v, err := argv.JoinQuoted([]string{tool, "-chain", argv.EncodeChain(x)})
		if err != nil {
			t.Fatal(err)
		}
		return "-toolexec=" + v
	}
	for _, tc := range []struct {
		args, want []string
	}{
		{[]string{"build", "-toolexec", "strace -f", "./..."}, []string{"build", chained("strace -f"), "./..."}},
		{[]string{"build", "-o", "-toolexec", "."}, []string{"build", "-o", "-toolexec", "."}}, // -o's value
		{[]string{"test", "-tags", "a b", "./...", "-toolexec=x", "-args", "-tags=y"}, []string{"test", "-tags=a,b,purego", "./...", chained("x"), "-args", "-tags=y"}},
		{[]string{"run", "-tags=a,purego", ".", "-toolexec=prog-arg"}, []string{"run", "-tags=a,purego", ".", "-toolexec=prog-arg"}},
		{[]string{"-C", "dir", "vet", "--toolexec=v"}, []string{"-C", "dir", "vet", chained("v")}},
		{[]string{"build", "--", "-toolexec=x"}, []string{"build", "--", "-toolexec=x"}},
		{[]string{"tool", "compile", "-toolexec=x"}, []string{"tool", "compile", "-toolexec=x"}},
		{[]string{"env", "-w", "GOFLAGS=-toolexec=x"}, []string{"env", "-w", "GOFLAGS=-toolexec=x"}},
	} {
		p := compose(t, home, nil, tc.args...)
		if !slices.Equal(p.Argv[1:], tc.want) {
			t.Errorf("args %q:\n got %q\nwant %q", tc.args, p.Argv[1:], tc.want)
		}
	}
	if _, err := Compose(Request{Home: home, Args: []string{"build", "-toolexec"}}); err == nil {
		t.Error("-toolexec without value accepted")
	}
}

func TestPuregoOptOut(t *testing.T) {
	home := fakeHome(t, release.GoVersion)
	p := compose(t, home, []string{"GRAALGO_PUREGO=0", "GOFLAGS=-tags=x"}, "build", "-tags=a")
	if want := []string{"-toolexec=" + filepath.Join(home, ToolRel), "-tags=x"}; !slices.Equal(goflags(t, p), want) {
		t.Errorf("GOFLAGS = %q, want %q", goflags(t, p), want)
	}
	if !slices.Equal(p.Argv, []string{"go", "build", "-tags=a"}) {
		t.Errorf("Argv = %q", p.Argv)
	}
}

func TestVersionIdentity(t *testing.T) {
	home := fakeHome(t, release.GoVersion)
	if p := compose(t, home, nil, "version"); !p.AppendVersion {
		t.Error("bare `version` does not append graalgo's version")
	}
	for _, args := range [][]string{{"version", "-m", "x"}, {"env"}, {"build"}} {
		if p := compose(t, home, nil, args...); p.AppendVersion {
			t.Errorf("%q appends a version", args)
		}
	}
}

func TestRefusals(t *testing.T) {
	old := fakeHome(t, "go1.26.3")
	if _, err := Compose(Request{Home: old, Args: []string{"build"}}); err == nil || !strings.Contains(err.Error(), release.GoVersion) {
		t.Errorf("GOROOT of another release: err = %v", err)
	}
	noTool := fakeHome(t, release.GoVersion)
	os.Remove(filepath.Join(noTool, ToolRel))
	if _, err := Compose(Request{Home: noTool, Args: []string{"build"}}); err == nil || !strings.Contains(err.Error(), "graalgo-tool") {
		t.Errorf("missing graalgo-tool: err = %v", err)
	}
	if _, err := Compose(Request{Home: t.TempDir(), Args: []string{"build"}}); err == nil {
		t.Error("empty GRAALGO_HOME accepted")
	}
}

func TestHomeFromExecutable(t *testing.T) {
	if got := HomeFromExecutable("/opt/graalgo/bin/graalgo"); got != "/opt/graalgo" {
		t.Errorf("got %q", got)
	}
	if got := HomeFromExecutable("/opt/graalgo/graalgo"); got != "/opt/graalgo" {
		t.Errorf("got %q", got)
	}
}

// cmd/go treats an empty variable as unset (cfg.Getenv falls through to
// the go env file; an empty GOENV means the default file): GOTOOLCHAIN=,
// CGO_ENABLED= and GOFLAGS= must not count as user settings.
func TestEmptyMeansUnset(t *testing.T) {
	home := fakeHome(t, release.GoVersion)
	tool := filepath.Join(home, ToolRel)
	p := compose(t, home, []string{"GOTOOLCHAIN=", "CGO_ENABLED=", "GOFLAGS="}, "build")
	if v, _ := getenv(p, "GOTOOLCHAIN"); v != "local" {
		t.Errorf("GOTOOLCHAIN= : GOTOOLCHAIN = %q, want local", v)
	}
	if v, _ := getenv(p, "CGO_ENABLED"); v != "0" {
		t.Errorf("CGO_ENABLED= : CGO_ENABLED = %q, want 0", v)
	}
	// An empty variable does not hide the go env file's value.
	cfg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg, "go"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "go", "env"), []byte("GOFLAGS=-mod=mod\nCGO_ENABLED=\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p = compose(t, home, []string{"XDG_CONFIG_HOME=" + cfg, "GOENV=", "GOFLAGS="}, "build")
	if want := []string{"-toolexec=" + tool, "-tags=purego", "-mod=mod"}; !slices.Equal(goflags(t, p), want) {
		t.Errorf("GOFLAGS = %q, want %q", goflags(t, p), want)
	}
	if v, _ := getenv(p, "CGO_ENABLED"); v != "0" {
		t.Errorf("empty CGO_ENABLED in the env file: CGO_ENABLED = %q, want 0", v)
	}
}
