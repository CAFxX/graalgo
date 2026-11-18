package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"graalgo-toolexec/release"
)

// buildLauncher builds this command into dir/bin/graalgo.
func buildLauncher(t *testing.T, home string) string {
	t.Helper()
	exe := filepath.Join(home, "bin", "graalgo")
	cmd := exec.Command("go", "build", "-o", exe, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build launcher: %v\n%s", err, out)
	}
	return exe
}

// fakeHome is a GRAALGO_HOME whose go is a shell script, for the exec
// semantics that do not need the real toolchain.
func fakeHome(t *testing.T, goScript string) string {
	t.Helper()
	home := t.TempDir()
	files := map[string]string{
		"goroot/VERSION":  release.GoVersion + "\n",
		"goroot/bin/go":   "#!/bin/sh\n" + goScript,
		"libexec/compile": "#!/bin/sh\nexit 0\n",
	}
	for name, content := range files {
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

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

// The launcher execs go: same process, so go's exit status and its
// signal handling are the launcher's (design §1 step 5).
func TestExecIdentityAndExitStatus(t *testing.T) {
	home := fakeHome(t, "echo \"pid=$$ argv=$*\"\necho \"GOFLAGS=$GOFLAGS\"\nexit 3\n")
	exe := buildLauncher(t, home)
	cmd := exec.Command(exe, "build", "-tags", "a", "./...")
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GRAALGO_HOME=")
	out, err := cmd.Output()
	if exitCode(err) != 3 {
		t.Fatalf("exit = %v, want 3\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if want := "pid=" + strconv.Itoa(cmd.Process.Pid) + " argv=build -tags=a,purego ./..."; lines[0] != want {
		t.Errorf("go saw %q, want %q (not exec'd in place, or argv not rewritten)", lines[0], want)
	}
	if want := "GOFLAGS=-toolexec=" + filepath.Join(home, "libexec/compile") + " -tags=purego"; lines[1] != want {
		t.Errorf("%q, want %q", lines[1], want)
	}
}

func TestSignalReachesGo(t *testing.T) {
	home := fakeHome(t, "trap 'echo got-TERM; exit 143' TERM\necho ready\nwhile :; do sleep 0.05; done\n")
	exe := buildLauncher(t, home)
	cmd := exec.Command(exe, "build")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, _ := stdout.Read(buf)
	if strings.TrimSpace(string(buf[:n])) != "ready" {
		t.Fatalf("unexpected output %q", buf[:n])
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	rest := make(chan string, 1)
	go func() {
		b := make([]byte, 64)
		n, _ := stdout.Read(b)
		rest <- string(b[:n])
	}()
	select {
	case s := <-rest:
		if strings.TrimSpace(s) != "got-TERM" {
			t.Errorf("after SIGTERM: %q", s)
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("SIGTERM did not reach go")
	}
	if err := cmd.Wait(); exitCode(err) != 143 {
		t.Errorf("exit = %v, want 143", err)
	}
}

func TestVersionAppended(t *testing.T) {
	home := fakeHome(t, "if [ \"$*\" = version ]; then echo 'go version "+release.GoVersion+" linux/amd64'; exit 0; fi\necho other; exit 4\n")
	exe := buildLauncher(t, home)
	out, err := exec.Command(exe, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := "go version " + release.GoVersion + " linux/amd64 graalgo-" + release.Version + "\n"; string(out) != want {
		t.Errorf("version = %q, want %q", out, want)
	}
	out, err = exec.Command(exe, "version", "-m", "x").Output()
	if exitCode(err) != 4 || string(out) != "other\n" {
		t.Errorf("version -m: %q, %v", out, err)
	}
}

func TestRefusesForeignGoroot(t *testing.T) {
	home := fakeHome(t, "exit 0\n")
	if err := os.WriteFile(filepath.Join(home, "goroot/VERSION"), []byte("go1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := buildLauncher(t, home)
	out, err := exec.Command(exe, "build").CombinedOutput()
	if exitCode(err) != 2 || !strings.Contains(string(out), "this graalgo tracks "+release.GoVersion) {
		t.Errorf("foreign GOROOT: %v\n%s", err, out)
	}
}
