package main

// compare_test.go — verdict-logic tests for the M8 fuzzer.
//
// The critical property: a broken harness (backend binary missing or
// unexecutable) must yield HARNESS_ERROR, never DIVERGED — otherwise every
// seed in a soak becomes a false-positive divergence.

import "testing"

func okResult() runResult {
	return runResult{buildOK: true, buildCode: 0, exit: 0, stdout: "hi\n", stderr: ""}
}

// A missing/unexecutable graalgo-run must not masquerade as a divergence.
func TestHarnessErrorGraalSide(t *testing.T) {
	gc := okResult()
	gr := runResult{buildOK: false, buildCode: -1, harnessErr: true}
	if v := compare(gc, gr); v != vHarnessError {
		t.Fatalf("got %v, want HARNESS_ERROR", v)
	}
}

// Same for the gc side: a dead gc binary is a harness problem, not a
// generator bug.
func TestHarnessErrorGCSide(t *testing.T) {
	gc := runResult{buildOK: false, buildCode: -1, harnessErr: true}
	gr := okResult()
	if v := compare(gc, gr); v != vHarnessError {
		t.Fatalf("got %v, want HARNESS_ERROR", v)
	}
}

// Harness-error takes precedence even when the other side also failed.
func TestHarnessErrorPrecedence(t *testing.T) {
	gc := runResult{buildOK: false, buildCode: 1, buildErr: "syntax error"}
	gr := runResult{buildOK: false, buildCode: -1, harnessErr: true}
	if v := compare(gc, gr); v != vHarnessError {
		t.Fatalf("got %v, want HARNESS_ERROR", v)
	}
}

// The harness check must not swallow real divergences.
func TestRealDivergenceStillDiverged(t *testing.T) {
	gc := okResult()
	gr := okResult()
	gr.exit = 1
	if v := compare(gc, gr); v != vDiverged {
		t.Fatalf("got %v, want DIVERGED", v)
	}
}

// Scope-error fail-fast still maps to SCOPE_REJECT, not HARNESS_ERROR.
func TestScopeRejectUnaffected(t *testing.T) {
	gc := okResult()
	gr := runResult{buildOK: false, buildCode: 2, buildErr: "scope error: not supported"}
	if v := compare(gc, gr); v != vScopeReject {
		t.Fatalf("got %v, want SCOPE_REJECT", v)
	}
}

// A graalgo build failure that is neither a scope error nor a harness
// failure is still a build-behavior divergence.
func TestGraalBuildFailureDiverged(t *testing.T) {
	gc := okResult()
	gr := runResult{buildOK: false, buildCode: 1, buildErr: "some backend crash"}
	if v := compare(gc, gr); v != vDiverged {
		t.Fatalf("got %v, want DIVERGED", v)
	}
}

func TestHarnessErrorString(t *testing.T) {
	if vHarnessError.String() != "HARNESS_ERROR" {
		t.Fatalf("got %q", vHarnessError.String())
	}
}
