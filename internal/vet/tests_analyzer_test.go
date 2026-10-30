package vet

import (
	"strings"
	"testing"
)

// Tests for the `tests` analyzer (runTests in tests.go).
//
// Regression coverage for review23 P1-4: the analyzer used to flag every
// correct TestMain (its "Test" prefix matched "TestMain") and to miss
// every wrong-typed TestMain (the TestMain loop checked only the
// parameter count, never the type).

func TestTestsAnalyzerTestMain(t *testing.T) {
	correct := `package p

import "testing"

func TestFoo(t *testing.T) {}

func TestMain(m *testing.M) {}
`
	joined := runVetOnModule(t, "", map[string]string{"p_test.go": correct})
	if strings.Contains(joined, "Test functions must take") {
		t.Errorf("correct TestMain/TestFoo flagged:\n%s", joined)
	}
	if strings.Contains(joined, "TestMain must take") {
		t.Errorf("correct TestMain flagged:\n%s", joined)
	}
}

func TestTestsAnalyzerWrongTypedTestMain(t *testing.T) {
	// TestMain taking *testing.T instead of *testing.M must be flagged.
	wrongType := `package p

import "testing"

func TestMain(m *testing.T) {}
`
	joined := runVetOnModule(t, "", map[string]string{"p_test.go": wrongType})
	if !strings.Contains(joined, "TestMain must take one *testing.M parameter") {
		t.Errorf("wrong-typed TestMain not flagged, got:\n%s", joined)
	}
}

func TestTestsAnalyzerWrongCountTestMain(t *testing.T) {
	noParam := `package p

import "testing"

func TestMain() {}
`
	joined := runVetOnModule(t, "", map[string]string{"p_test.go": noParam})
	if !strings.Contains(joined, "TestMain must take one *testing.M parameter") {
		t.Errorf("parameterless TestMain not flagged, got:\n%s", joined)
	}
}

func TestTestsAnalyzerWrongTypedTestFunc(t *testing.T) {
	// A TestXxx function taking *testing.M (not *testing.T) must be
	// flagged, and must not be silently treated as a TestMain.
	wrongType := `package p

import "testing"

func TestFoo(m *testing.M) {}
`
	joined := runVetOnModule(t, "", map[string]string{"p_test.go": wrongType})
	if !strings.Contains(joined, "Test functions must take *testing.T") {
		t.Errorf("wrong-typed TestFoo not flagged, got:\n%s", joined)
	}
}

func TestTestsAnalyzerTestFuncParamCount(t *testing.T) {
	noParam := `package p

import "testing"

func TestFoo() {}
`
	joined := runVetOnModule(t, "", map[string]string{"p_test.go": noParam})
	if !strings.Contains(joined, "Test functions must take one *testing.T parameter") {
		t.Errorf("parameterless TestFoo not flagged, got:\n%s", joined)
	}
}

func TestTestsAnalyzerNonTestFileIgnored(t *testing.T) {
	// A function named TestMain in a non-test file is not a test
	// entry point and must not be judged by the tests analyzer.
	src := `package p

func TestMain() {}
`
	joined := runVetOnModule(t, "", map[string]string{"p.go": src})
	if strings.Contains(joined, "TestMain must take") {
		t.Errorf("TestMain in non-test file flagged:\n%s", joined)
	}
}
