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
	a, b := mkOutcome("a"), mkOutcome("b")
	a.BuildOK, b.BuildOK = false, false
	a.BuildLog = Normalize("./main.go:4: undefined: foo")
	b.BuildLog = Normalize("./main.go:4: undefined: foo")
	if c := Compare(a, b); !c.Same {
		t.Fatalf("identical build failures diverged: %+v", c)
	}
	b.BuildLog = Normalize("./main.go:5: undefined: bar")
	if c := Compare(a, b); c.Same {
		t.Fatal("differing build failures not detected")
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
