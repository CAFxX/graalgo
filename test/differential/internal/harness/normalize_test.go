package harness

import (
	"strings"
	"testing"
)

// Red-green note: every normalization rule has a MUST-CHANGE case (asserting
// output != input, so the test fails if the rule is dropped) and a
// MUST-NOT-CHANGE case (asserting byte-identical output, so the test fails
// if the rule over-matches — over-normalization hides real divergences).

func TestNormalize_FrameArgsRedacted(t *testing.T) {
	in := "panic: boom\n\ngoroutine 1 [running]:\nmain.f(0xc000014260, 0x1)\n\t/x/main.go:7 +0x27\npanic({0x4b1e20?, 0xc000012345?})"
	out := Normalize(in)
	for _, raw := range []string{"0xc000014260", "0x1)", "+0x27", "0x4b1e20", "0xc000012345"} {
		if strings.Contains(out, raw) {
			t.Fatalf("address %q survived: %q", raw, out)
		}
	}
	if !strings.Contains(out, "main.f(0xADDR, 0xADDR)") {
		t.Fatalf("frame args not redacted: %q", out)
	}
}

func TestNormalize_MethodFrameRedactsOnlyArgs(t *testing.T) {
	in := "main.(*T).m(0xc000010000)"
	if got, want := Normalize(in), "main.(*T).m(0xADDR)"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalize_ProgramHexUntouched(t *testing.T) {
	// Computed values a program prints — in panic messages or plain
	// stderr lines — are observables and must compare exactly.
	for _, in := range []string{
		"hello world 123",
		"exit status 3",
		"panic: bad value 0xdeadbeef",
		"panic: main.E{X:0x10} [recovered]",
		"value: 0x1f600",
		"0x is not an address",
		"\tfmt.Println(x) // 0x2a",
	} {
		if out := Normalize(in); out != in {
			t.Fatalf("over-match: %q -> %q", in, out)
		}
	}
}

func TestNormalize_GoroutineIDs(t *testing.T) {
	in := "goroutine 18 [running]:\ncreated by main.main in goroutine 7"
	want := "goroutine # [running]:\ncreated by main.main in goroutine #"
	if out := Normalize(in); out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestNormalize_LineNumbersKept(t *testing.T) {
	// Line attribution is compared: a panic attributed to the wrong line
	// is a divergence (this rule used to erase every .go:N).
	in := "\tmain.main()\n\t\t/tmp/x/main.go:42 +0x27"
	out := Normalize(in)
	if !strings.Contains(out, "main.go:42") {
		t.Fatalf("line number erased: %q", out)
	}
	if strings.Contains(out, "+0x27") {
		t.Fatalf("pc offset survived: %q", out)
	}
	if Normalize("/x/main.go:41 +0x1") == Normalize("/x/main.go:42 +0x1") {
		t.Fatal("different lines normalized equal")
	}
}

func TestNormalize_TracesEqualModuloAddrs(t *testing.T) {
	a := "panic: boom\n\ngoroutine 6 [running]:\nmain.level3(0xc000010000)\n\t/tmp/c/main.go:4 +0x25\ncreated by main.main in goroutine 1\n\t/tmp/c/main.go:8 +0x29"
	b := "panic: boom\n\ngoroutine 19 [running]:\nmain.level3(0xc000098000)\n\t/tmp/c/main.go:4 +0x3a\ncreated by main.main in goroutine 1\n\t/tmp/c/main.go:8 +0x31"
	if a == b {
		t.Fatal("test bug: inputs must differ before normalization")
	}
	if na, nb := Normalize(a), Normalize(b); na != nb {
		t.Fatalf("normalized traces differ:\n%s\n---\n%s", na, nb)
	}
}

func TestNormalize_SignalAndRegisters(t *testing.T) {
	in := "[signal SIGSEGV: segmentation violation code=0x1 addr=0x8 pc=0x4a3b2c]\nruntime.f() fp=0xc000046f58 sp=0xc000046f38 pc=0x4a3b2c"
	out := Normalize(in)
	if strings.Contains(out, "0x8 ") || strings.Contains(out, "0x4a3b2c") || strings.Contains(out, "0xc000046f58") {
		t.Fatalf("signal/register hex not redacted: %q", out)
	}
	if !strings.Contains(out, "SIGSEGV") {
		t.Fatalf("signal name must survive: %q", out)
	}
}

func TestNormalizeStdout_OptInOnly(t *testing.T) {
	in := "ptr 0xc000012080\n"
	if got := NormalizeStdout(in, nil); got != in {
		t.Fatalf("stdout normalized without opt-in: %q", got)
	}
	if got := NormalizeStdout(in, []string{"pointers"}); got != "ptr 0xPTR\n" {
		t.Fatalf("pointers rule not applied: %q", got)
	}
}

func TestParseNormalizeFile(t *testing.T) {
	r, err := ParseNormalizeFile("# comment\npointers # %p\n\n")
	if err != nil || len(r) != 1 || r[0] != "pointers" {
		t.Fatalf("got %v %v", r, err)
	}
	if _, err := ParseNormalizeFile("everything\n"); err == nil {
		t.Fatal("unknown rule accepted")
	}
}

func TestParseReviewedExclusions(t *testing.T) {
	ok := "version: 1\nexclusions:\n- file: a\n  reason: deferred-parity\n  detail: x\n"
	if _, m, err := ParseReviewedExclusions("t", ok); err != nil || m["a"].Reason != "deferred-parity" {
		t.Fatalf("valid file rejected: %v %v", m, err)
	}
	for name, bad := range map[string]string{
		"bad reason":  "version: 1\nexclusions:\n- file: a\n  reason: flaky\n  detail: x\n",
		"duplicate":   ok + "- file: a\n  reason: backend-specific\n  detail: y\n",
		"no version":  "exclusions:\n- file: a\n  reason: deferred-parity\n",
		"stray line":  ok + "  reasn: typo\n",
		"no file key": "version: 1\nexclusions:\n- file:\n  reason: deferred-parity\n",
	} {
		if _, _, err := ParseReviewedExclusions("t", bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
