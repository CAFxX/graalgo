package p3frontend

// P3-M5 (WS E, ADR-0005): p3frontend tests for the synthetic "time" and
// "context" packages. The importer reimplements the frozen exporter's
// synthetic signatures (it cannot reuse them — unexported and frozen);
// these tests pin the mirror so drift fails fast.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
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
	// "String" is the documented mirror-only divergence (the phase-1
	// source lacks it; see the header comment in
	// synth_time_context.go) — pinned here, allowed in the drift test.
	for _, name := range []string{"Unix", "UnixNano", "Add", "Sub", "Before",
		"After", "Equal", "Compare", "IsZero", "String", "Format",
		"Date", "Clock", "Zone", "Round", "Truncate", "AppendFormat",
		"MarshalText", "UnmarshalText",
		"GoString", "AddDate", "Year", "Month", "Day", "Weekday",
		"Hour", "Minute", "Second", "Nanosecond", "YearDay",
		"UTC", "Local", "In", "Location", "UnixMilli", "UnixMicro"} {
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

	// Location type with String on *Location, and the Local/UTC vars.
	locObj := pkg.Scope().Lookup("Location")
	if locObj == nil {
		t.Fatal("time.Location missing")
	}
	locNamed, ok := locObj.Type().(*types.Named)
	if !ok {
		t.Fatalf("time.Location is %T, want *types.Named", locObj.Type())
	}
	if _, ok := locNamed.Underlying().(*types.Struct); !ok {
		t.Fatalf("time.Location underlying is %T, want *types.Struct", locNamed.Underlying())
	}
	if !hasMethod(locNamed, "String") {
		t.Fatal("time.Location lacks method String")
	}
	for _, name := range []string{"Local", "UTC"} {
		v := pkg.Scope().Lookup(name)
		if v == nil {
			t.Fatalf("time.%s missing", name)
		}
		if _, ok := v.Type().(*types.Pointer); !ok {
			t.Fatalf("time.%s is %T, want *Location", name, v.Type())
		}
	}

	// ParseError is a named struct with gc's five fields (Layout,
	// Value, LayoutElem, ValueElem, Message) and Error on *ParseError.
	peObj := pkg.Scope().Lookup("ParseError")
	if peObj == nil {
		t.Fatal("time.ParseError missing")
	}
	peNamed, ok := peObj.Type().(*types.Named)
	if !ok {
		t.Fatalf("time.ParseError is %T, want *types.Named", peObj.Type())
	}
	peStruct, ok := peNamed.Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("time.ParseError underlying is %T, want *types.Struct", peNamed.Underlying())
	}
	if peStruct.NumFields() != 5 {
		t.Fatalf("time.ParseError has %d fields, want 5", peStruct.NumFields())
	}
	for i, want := range []string{"Layout", "Value", "LayoutElem", "ValueElem", "Message"} {
		if peStruct.Field(i).Name() != want {
			t.Fatalf("time.ParseError field %d = %q, want %q", i, peStruct.Field(i).Name(), want)
		}
		if peStruct.Field(i).Type() != types.Typ[types.String] {
			t.Fatalf("time.ParseError.%s is %v, want string", want, peStruct.Field(i).Type())
		}
	}
	if !hasMethod(peNamed, "Error") {
		t.Fatal("time.ParseError lacks method Error")
	}

	// Package-level functions.
	for _, name := range []string{"Now", "Sleep", "After", "Tick", "NewTimer",
		"NewTicker", "AfterFunc", "Since", "Until", "ParseDuration",
		"Date", "Parse", "Unix"} {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			t.Fatalf("time.%s missing", name)
		}
		if _, ok := obj.(*types.Func); !ok {
			t.Fatalf("time.%s is %T, want *types.Func", name, obj)
		}
	}
}

// phase1SyntheticContextPackage type-checks the REAL phase-1 synthetic
// context source (internal/frontend/synth_time_context.go,
// syntheticContextSource const) and returns its *types.Package. This is
// the drift oracle for the mirror: the test below compares the mirror's
// Context interface method-by-method against this package, so a
// phase-1-side change (like the afc0dfb Deadline removal) fails fast
// here instead of silently diverging.
func phase1SyntheticContextPackage(t *testing.T, timePkg *types.Package) *types.Package {
	t.Helper()
	srcPath := "../synth_time_context.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, srcPath, nil, 0)
	if err != nil {
		t.Fatalf("parsing phase-1 synth source: %v", err)
	}
	var raw string
	found := false
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name == "syntheticContextSource" && i < len(vs.Values) {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("syntheticContextSource is not a string literal")
					}
					raw, err = strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquoting syntheticContextSource: %v", err)
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("syntheticContextSource const not found in %s", srcPath)
	}
	ctxFset := token.NewFileSet()
	ctxFile, err := parser.ParseFile(ctxFset, "context.go", raw, parser.AllErrors)
	if err != nil {
		t.Fatalf("parsing synthetic context source: %v", err)
	}
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			if path == "time" {
				return timePkg, nil
			}
			return nil, fmt.Errorf("unexpected synthetic import %q", path)
		}),
	}
	pkg, err := conf.Check("context", ctxFset, []*ast.File{ctxFile}, nil)
	if err != nil {
		t.Fatalf("type-checking phase-1 synthetic context: %v", err)
	}
	return pkg
}

// importerFunc adapts a func to types.Importer.
type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// sigString renders a method signature with package-qualified type names
// (both the mirror and the phase-1 check use packages named "context"
// and "time", so equal signatures render equal strings).
func sigString(sig *types.Signature) string {
	return types.TypeString(sig, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	})
}

// TestContextSyntheticSignatures pins the mirrored synthetic context
// package: Context interface, CancelFunc/CancelCauseFunc, package funcs,
// and Canceled/DeadlineExceeded vars.
//
// The Context interface check is a REAL drift test: it compares the
// mirror's method set (names + signatures) against the phase-1
// synthetic package's actual *types.Package — not against the mirror
// itself. (The pre-P1-6 version asserted 4 methods including Deadline
// by inspecting only the mirror, which is why the afc0dfb interface fix
// never tripped it.)
func TestContextSyntheticSignatures(t *testing.T) {
	timePkg := syntheticTimePackage()
	pkg := syntheticContextPackage(timePkg)
	if pkg.Name() != "context" || pkg.Path() != "context" {
		t.Fatalf("package name/path = %s/%s, want context/context",
			pkg.Name(), pkg.Path())
	}

	// Drift check: the mirror's Context interface must match the
	// phase-1 synthetic's method-for-method (name + signature).
	phase1Pkg := phase1SyntheticContextPackage(t, timePkg)
	ifaceOf := func(p *types.Package) *types.Interface {
		obj := p.Scope().Lookup("Context")
		if obj == nil {
			t.Fatalf("context.Context missing in %s", p.Path())
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			t.Fatalf("context.Context is %T, want *types.Named", obj.Type())
		}
		iface, ok := named.Underlying().(*types.Interface)
		if !ok {
			t.Fatalf("context.Context underlying is %T, want *types.Interface",
				named.Underlying())
		}
		return iface
	}
	mirrorIface, phase1Iface := ifaceOf(pkg), ifaceOf(phase1Pkg)
	if mirrorIface.NumMethods() != phase1Iface.NumMethods() {
		t.Fatalf("mirror Context has %d methods, phase-1 has %d",
			mirrorIface.NumMethods(), phase1Iface.NumMethods())
	}
	for i := 0; i < phase1Iface.NumMethods(); i++ {
		want := phase1Iface.Method(i)
		var got *types.Func
		for j := 0; j < mirrorIface.NumMethods(); j++ {
			if mirrorIface.Method(j).Name() == want.Name() {
				got = mirrorIface.Method(j)
				break
			}
		}
		if got == nil {
			t.Fatalf("mirror Context missing method %q (phase-1 has it)", want.Name())
		}
		wantSig, gotSig := want.Type().(*types.Signature), got.Type().(*types.Signature)
		if wantSig == nil || gotSig == nil {
			t.Fatalf("context.Context.%s is not a signature", want.Name())
		}
		if sigString(wantSig) != sigString(gotSig) {
			t.Fatalf("mirror Context.%s signature drift:\n phase-1: %s\n mirror:  %s",
				want.Name(), sigString(wantSig), sigString(gotSig))
		}
	}
	// Deadline must be PRESENT in the mirror: phase-1 restored it (P4
	// multi-result interface mechanism, 046afc86), retiring the old
	// afc0dfb omission. The method-for-method loop above already
	// compares its signature; this explicit check keeps the
	// restoration pinned if the loop's expectations ever change.
	foundDeadline := false
	for i := 0; i < mirrorIface.NumMethods(); i++ {
		if mirrorIface.Method(i).Name() == "Deadline" {
			foundDeadline = true
		}
	}
	if !foundDeadline {
		t.Fatal("mirror Context must declare Deadline (restored in phase-1, 046afc86)")
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

// TestTimeMirrorMatchesPhase1 is the drift test for the synthetic time
// mirror: the mirror must match the live phase-1 syntheticTimeSource
// (internal/frontend/synth_time_context.go) in scope and in the method
// sets of every named type. ONE documented exception: the mirror declares
// Time.String, which the phase-1 source lacks (a live-path gap — gc has
// it; see the header comment in synth_time_context.go). Any other
// divergence fails fast here instead of silently diverging.
func TestTimeMirrorMatchesPhase1(t *testing.T) {
	mirror := syntheticTimePackage()
	// The time source imports nothing, so any import is a failure.
	phase1 := phase1SyntheticPackageForTest(t,
		"../synth_time_context.go", "syntheticTimeSource", "time", rejectImports{})
	assertScopeMatchesPhase1(t, "time", mirror, phase1)
	for _, name := range []string{"Duration", "Time", "Timer", "Ticker",
		"Location", "Month", "Weekday", "ParseError"} {
		compareNamedMethodSets(t, "time."+name, mirror, phase1, name,
			map[string]bool{"Time.String": true})
	}
}

// compareNamedMethodSets checks that the named type `name` has the same
// method set in mirror and phase1 (name + rendered signature), except for
// the explicitly allowed extras (mirror-only "Type.Method" keys, e.g.
// the documented Time.String divergence). Order-independent: go/types
// sorts type-checked methods, AddMethod keeps insertion order.
func compareNamedMethodSets(t *testing.T, label string, mirror, phase1 *types.Package, name string, allowExtra map[string]bool) {
	t.Helper()
	mirrorObj := mirror.Scope().Lookup(name)
	phase1Obj := phase1.Scope().Lookup(name)
	if mirrorObj == nil || phase1Obj == nil {
		t.Fatalf("%s: mirror=%v phase1=%v", label, mirrorObj != nil, phase1Obj != nil)
	}
	mirrorNamed, ok := mirrorObj.Type().(*types.Named)
	if !ok {
		t.Fatalf("%s: mirror type is %T, want *types.Named", label, mirrorObj.Type())
	}
	phase1Named, ok := phase1Obj.Type().(*types.Named)
	if !ok {
		t.Fatalf("%s: phase1 type is %T, want *types.Named", label, phase1Obj.Type())
	}
	qual := func(p *types.Package) string { return p.Name() }
	sigOf := func(named *types.Named, method string) string {
		for i := 0; i < named.NumMethods(); i++ {
			if m := named.Method(i); m.Name() == method {
				return types.TypeString(m.Type(), qual)
			}
		}
		return ""
	}
	for i := 0; i < phase1Named.NumMethods(); i++ {
		m := phase1Named.Method(i)
		got := sigOf(mirrorNamed, m.Name())
		if got == "" {
			t.Errorf("%s: mirror lacks method %s", label, m.Name())
			continue
		}
		want := types.TypeString(m.Type(), qual)
		if got != want {
			t.Errorf("%s.%s: mirror %s, want %s", label, m.Name(), got, want)
		}
	}
	for i := 0; i < mirrorNamed.NumMethods(); i++ {
		m := mirrorNamed.Method(i)
		if sigOf(phase1Named, m.Name()) == "" && !allowExtra[name+"."+m.Name()] {
			t.Errorf("%s: mirror has extra method %s (not in phase-1 source)", label, m.Name())
		}
	}
}
