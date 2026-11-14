package harness

import (
	"strings"
	"testing"
)

func mkOutcome(backend string) Outcome {
	return Outcome{
		Program:  "p",
		Backend:  backend,
		BuildOK:  true,
		Stdout:   "hello\n",
		Stderr:   "",
		ExitCode: 0,
	}
}

func TestCompare_Identical(t *testing.T) {
	c := Compare(mkOutcome("a"), mkOutcome("b"))
	if !c.Same || len(c.Reasons) != 0 {
		t.Fatalf("identical outcomes diverged: %+v", c)
	}
}

func TestCompare_StdoutDiff(t *testing.T) {
	a, b := mkOutcome("a"), mkOutcome("b")
	b.Stdout = "goodbye\n"
	c := Compare(a, b)
	if c.Same {
		t.Fatal("stdout difference not detected")
	}
	if !strings.Contains(strings.Join(c.Reasons, " "), "stdout") {
		t.Fatalf("reason should mention stdout: %v", c.Reasons)
	}
}

func TestCompare_ExitCodeDiff(t *testing.T) {
	a, b := mkOutcome("a"), mkOutcome("b")
	b.ExitCode = 3
	c := Compare(a, b)
	if c.Same {
		t.Fatal("exit code difference not detected")
	}
}

func TestCompare_TimeoutDiff(t *testing.T) {
	a, b := mkOutcome("a"), mkOutcome("b")
	b.TimedOut = true
	b.ExitCode = -1
	c := Compare(a, b)
	if c.Same {
		t.Fatal("timeout difference not detected")
	}
}

func TestCompare_StderrNormalizedEqual(t *testing.T) {
	// Mimics the runner: stderr is normalized at capture, so traces that
	// differ only in addresses compare Same.
	a, b := mkOutcome("a"), mkOutcome("b")
	a.Stderr = Normalize("panic: x\n\tmain.go:3 +0x25\ngoroutine 6 [running]:")
	b.Stderr = Normalize("panic: x\n\tmain.go:3 +0x99\ngoroutine 42 [running]:")
	if a.Stderr == "panic: x\n\tmain.go:3 +0x25\ngoroutine 6 [running]:" {
		t.Fatal("test bug: normalization did not change the input")
	}
	if c := Compare(a, b); !c.Same {
		t.Fatalf("normalized-equal stderr diverged: %+v", c)
	}
	// ...but a real message difference still diverges.
	b.Stderr = Normalize("panic: y\n\tmain.go:3 +0x99\ngoroutine 42 [running]:")
	if c := Compare(a, b); c.Same {
		t.Fatal("stderr message difference not detected")
	}
}

func TestCompare_BothBuildFail(t *testing.T) {
	// Production-shaped logs, as emitted by the runner's BuildOnly
	// failure path: "build [<full backend command>] failed: <err>\n"
	// + backend stderr. The two commands differ by construction
	// ("go build -o ..." vs "graalgo-sync-run --check ..."), so the
	// prefix must not decide the verdict — only the error text may.
	// (R0614-P1-9: the pre-fix comparison of the raw logs reported
	// DIVERGE for identical errors, with the reason showing the
	// command line instead of the error.)
	refLog := func(err string) string {
		return Normalize("build [/home/hatch/tools/go/bin/go build -o {WORK}/prog ./main.go] failed: exit status 1\n" + err)
	}
	altLog := func(err string) string {
		return Normalize("build [/home/hatch/workspace/graalgo/build/graalgo-sync-run --check ./main.go] failed: exit status 1\n" + err)
	}
	a, b := mkOutcome("a"), mkOutcome("b")
	a.BuildOK, b.BuildOK = false, false
	a.BuildLog = refLog("./main.go:4: undefined: foo")
	b.BuildLog = altLog("./main.go:4: undefined: foo")
	if c := Compare(a, b); !c.Same {
		t.Fatalf("identical build failures diverged: %+v", c)
	}
	// Differing errors must still diverge — and the reason must name
	// the differing error line, not the (always differing) command.
	b.BuildLog = altLog("./main.go:5: undefined: bar")
	if c := Compare(a, b); c.Same {
		t.Fatal("differing build failures not detected")
	} else if c.Reasons[0] == "" || !strings.Contains(c.Reasons[0], "undefined: bar") {
		t.Fatalf("divergence reason hides the real error: %v", c.Reasons)
	}
	// Bare stderr with no command prefix (e.g. logs recorded before
	// the prefix existed) still compares on the error text.
	a.BuildLog = Normalize("./main.go:4: undefined: foo")
	b.BuildLog = Normalize("./main.go:4: undefined: foo")
	if c := Compare(a, b); !c.Same {
		t.Fatalf("identical bare build failures diverged: %+v", c)
	}
}

func TestStripBuildPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"build [/bin/go build -o x ./main.go] failed: exit status 1\n./main.go:4: undefined: foo", "./main.go:4: undefined: foo"},
		{"build [/bin/go build -o x ./main.go] failed: exit status 1\n", ""},
		{"build [/bin/go build -o x ./main.go] failed: exit status 1", ""},
		{"./main.go:4: undefined: foo", "./main.go:4: undefined: foo"},
		{"", ""},
		// Looks like a prefix but lacks the "] failed: " marker: kept.
		{"build [something] else\n./main.go:4: undefined: foo", "build [something] else\n./main.go:4: undefined: foo"},
		// Prefix-shaped line after the first line is real stderr: kept.
		{"./main.go:4: undefined: foo\nbuild [/bin/go] failed: exit status 1", "./main.go:4: undefined: foo\nbuild [/bin/go] failed: exit status 1"},
		// Multi-line stderr survives intact after the prefix.
		{"build [/bin/go build -o x ./main.go] failed: exit status 1\n# pkg\n./main.go:4: undefined: foo\n./main.go:5: undefined: bar", "# pkg\n./main.go:4: undefined: foo\n./main.go:5: undefined: bar"},
	}
	for i, tc := range cases {
		if got := stripBuildPrefix(tc.in); got != tc.want {
			t.Errorf("case %d: stripBuildPrefix(%q) = %q, want %q", i, tc.in, got, tc.want)
		}
	}
}

func TestCompare_OneBuildFails(t *testing.T) {
	a, b := mkOutcome("a"), mkOutcome("b")
	b.BuildOK = false
	b.BuildLog = "boom"
	if c := Compare(a, b); c.Same {
		t.Fatal("one-sided build failure not detected")
	}
}
