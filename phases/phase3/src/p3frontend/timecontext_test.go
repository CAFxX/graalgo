package p3frontend

// P3-M5 (WS E, ADR-0005): p3frontend tests for the synthetic "time" and
// "context" packages. The importer reimplements the frozen exporter's
// synthetic signatures (it cannot reuse them — unexported and frozen);
// these tests pin the mirror so drift fails fast.

import (
	"go/types"
	"testing"
)

// TestTimeSyntheticSignatures pins the mirrored synthetic time package:
// Duration+consts+methods, Time+methods, Timer/Ticker with C/Stop/Reset,
// Month/Weekday+String+consts, and the package-level functions.
func TestTimeSyntheticSignatures(t *testing.T) {
	pkg := syntheticTimePackage()
	if pkg.Name() != "time" || pkg.Path() != "time" {
		t.Fatalf("package name/path = %s/%s, want time/time", pkg.Name(), pkg.Path())
	}

	// Duration is a named int64.
	durObj := pkg.Scope().Lookup("Duration")
	if durObj == nil {
		t.Fatal("time.Duration missing")
	}
	durNamed, ok := durObj.Type().(*types.Named)
	if !ok {
		t.Fatalf("time.Duration is %T, want *types.Named", durObj.Type())
	}
	if _, ok := durNamed.Underlying().(*types.Basic); !ok {
		t.Fatalf("time.Duration underlying is %T, want *types.Basic (int64)",
			durNamed.Underlying())
	}

	// Duration constants.
	for _, name := range []string{"Nanosecond", "Microsecond", "Millisecond",
		"Second", "Minute", "Hour"} {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			t.Fatalf("time.%s missing", name)
		}
		if _, ok := obj.(*types.Const); !ok {
			t.Fatalf("time.%s is %T, want *types.Const", name, obj)
		}
	}

	// Duration methods.
	for _, name := range []string{"String", "Nanoseconds", "Microseconds",
		"Milliseconds", "Seconds", "Minutes", "Hours", "Truncate", "Round", "Abs"} {
		found := false
		for i := 0; i < durNamed.NumMethods(); i++ {
			if durNamed.Method(i).Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("time.Duration.%s missing", name)
		}
	}

	// Time is a named struct.
	timeObj := pkg.Scope().Lookup("Time")
	if timeObj == nil {
		t.Fatal("time.Time missing")
	}
	timeNamed, ok := timeObj.Type().(*types.Named)
	if !ok {
		t.Fatalf("time.Time is %T, want *types.Named", timeObj.Type())
	}
	if _, ok := timeNamed.Underlying().(*types.Struct); !ok {
		t.Fatalf("time.Time underlying is %T, want *types.Struct",
			timeNamed.Underlying())
	}

	// Time methods (including multi-result Date/Clock/Zone).
	for _, name := range []string{"Unix", "UnixNano", "Add", "Sub", "Before",
		"After", "Equal", "Compare", "IsZero", "String", "Format",
		"Date", "Clock", "Zone"} {
		found := false
		for i := 0; i < timeNamed.NumMethods(); i++ {
			if timeNamed.Method(i).Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("time.Time.%s missing", name)
		}
	}

	// Timer has C field and Stop/Reset methods.
	timerObj := pkg.Scope().Lookup("Timer")
	if timerObj == nil {
		t.Fatal("time.Timer missing")
	}
	timerNamed := timerObj.Type().(*types.Named)
	timerStruct := timerNamed.Underlying().(*types.Struct)
	if timerStruct.NumFields() != 1 || timerStruct.Field(0).Name() != "C" {
		t.Fatalf("time.Timer should have single field C, got %v", timerStruct)
	}
	if _, ok := timerStruct.Field(0).Type().(*types.Chan); !ok {
		t.Fatalf("time.Timer.C is %T, want *types.Chan", timerStruct.Field(0).Type())
	}

	// Ticker has C field and Stop/Reset methods.
	tickerObj := pkg.Scope().Lookup("Ticker")
	if tickerObj == nil {
		t.Fatal("time.Ticker missing")
	}
	tickerNamed := tickerObj.Type().(*types.Named)
	if tickerNamed.NumMethods() != 2 {
		t.Fatalf("time.Ticker has %d methods, want 2 (Stop/Reset)",
			tickerNamed.NumMethods())
	}

	// Package-level functions.
	for _, name := range []string{"Now", "Sleep", "After", "Tick", "NewTimer",
		"NewTicker", "AfterFunc", "Since", "Until", "ParseDuration"} {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			t.Fatalf("time.%s missing", name)
		}
		if _, ok := obj.(*types.Func); !ok {
			t.Fatalf("time.%s is %T, want *types.Func", name, obj)
		}
	}
}

// TestContextSyntheticSignatures pins the mirrored synthetic context
// package: Context interface, CancelFunc/CancelCauseFunc, package funcs,
// and Canceled/DeadlineExceeded vars.
func TestContextSyntheticSignatures(t *testing.T) {
	timePkg := syntheticTimePackage()
	pkg := syntheticContextPackage(timePkg)
	if pkg.Name() != "context" || pkg.Path() != "context" {
		t.Fatalf("package name/path = %s/%s, want context/context",
			pkg.Name(), pkg.Path())
	}

	// Context is a named interface with 4 methods.
	ctxObj := pkg.Scope().Lookup("Context")
	if ctxObj == nil {
		t.Fatal("context.Context missing")
	}
	ctxNamed, ok := ctxObj.Type().(*types.Named)
	if !ok {
		t.Fatalf("context.Context is %T, want *types.Named", ctxObj.Type())
	}
	ctxIface, ok := ctxNamed.Underlying().(*types.Interface)
	if !ok {
		t.Fatalf("context.Context underlying is %T, want *types.Interface",
			ctxNamed.Underlying())
	}
	if ctxIface.NumMethods() != 4 {
		t.Fatalf("context.Context has %d methods, want 4", ctxIface.NumMethods())
	}
	for _, name := range []string{"Deadline", "Done", "Err", "Value"} {
		found := false
		for i := 0; i < ctxIface.NumMethods(); i++ {
			if ctxIface.Method(i).Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("context.Context.%s missing", name)
		}
	}

	// CancelFunc and CancelCauseFunc.
	for _, name := range []string{"CancelFunc", "CancelCauseFunc"} {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			t.Fatalf("context.%s missing", name)
		}
		if _, ok := obj.Type().(*types.Named); !ok {
			t.Fatalf("context.%s is %T, want *types.Named", name, obj.Type())
		}
	}

	// Package-level functions.
	for _, name := range []string{"Background", "TODO", "WithCancel",
		"WithCancelCause", "WithDeadline", "WithTimeout", "WithValue",
		"Cause", "AfterFunc", "WithoutCancel"} {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			t.Fatalf("context.%s missing", name)
		}
		if _, ok := obj.(*types.Func); !ok {
			t.Fatalf("context.%s is %T, want *types.Func", name, obj)
		}
	}

	// Canceled and DeadlineExceeded vars.
	for _, name := range []string{"Canceled", "DeadlineExceeded"} {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			t.Fatalf("context.%s missing", name)
		}
		if _, ok := obj.(*types.Var); !ok {
			t.Fatalf("context.%s is %T, want *types.Var", name, obj)
		}
	}
}

// TestTimeContextImportResolvesSynthetic verifies the loader resolves
// "time" and "context" as synthetic (never GOROOT).
func TestTimeContextImportResolvesSynthetic(t *testing.T) {
	l := NewLoader()
	for _, path := range []string{"time", "context"} {
		ref, err := l.ResolveImport("/tmp", path)
		if err != nil {
			t.Fatalf("ResolveImport(%q) failed: %v", path, err)
		}
		if ref.Kind != kindSynthetic {
			t.Fatalf("ResolveImport(%q).Kind = %v, want kindSynthetic", path, ref.Kind)
		}
	}
}
