package main

import (
	"reflect"
	"testing"

	"diffharness/internal/typeparam"
)

func TestSplitRecheckResults(t *testing.T) {
	rechecked := map[string]typeparam.Exclusion{
		"a.go": {File: "a.go", Reason: "backend-specific"},
		"b.go": {File: "b.go", Reason: "deferred-parity"},
	}
	comps := []typeparam.Comparison{
		{Program: "a.go", Same: true},                             // XPASS
		{Program: "b.go", Same: false, Reasons: []string{"boom"}}, // XFAIL
		{Program: "c.go", Same: true},                             // not rechecked: ignored
		{Program: "d.go", Same: false},                            // not rechecked: ignored
	}
	xpass, xfail := splitRecheckResults(comps, rechecked)
	if !reflect.DeepEqual(xpass, []string{"a.go"}) {
		t.Errorf("xpass = %v, want [a.go]", xpass)
	}
	if !reflect.DeepEqual(xfail, []string{"b.go"}) {
		t.Errorf("xfail = %v, want [b.go]", xfail)
	}
}

func TestSplitRecheckResultsEmpty(t *testing.T) {
	xpass, xfail := splitRecheckResults(nil, map[string]typeparam.Exclusion{})
	if len(xpass) != 0 || len(xfail) != 0 {
		t.Errorf("want empty, got xpass=%v xfail=%v", xpass, xfail)
	}
}
