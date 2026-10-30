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
	// Deadline must be PRESENT in the mirror: P4 (046afc86) restored it
	// to the phase-1 interface via the general multi-result mechanism,
	// removing the P3-M5 (afc0dfb) deliberate divergence. The loop above
	// already fails if the mirror lacks it or its signature drifts.

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
