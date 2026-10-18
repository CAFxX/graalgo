package main

// Regression tests for the 2026-10-18 independent codebase review,
// remediation lane review-p0-os (synthetic os/syscall surface).
//
//  1. (P0) os.Expand/os.ExpandEnv are real gc 1.27.1 string code — the
//     header's "full env surface" claim is now true at runtime, not just
//     at type-check.
//  2. (P1) __fileReadFrom/__fileWriteTo implement the D4 copy semantics
//     (io.ReadAll + concrete write / concrete read-all + io.WriteString)
//     instead of panicking on the stale P0-2 re-test trigger.
//  3. (P1) os.ProcAttr.Sys is *syscall.SysProcAttr (not any), and the
//     synthetic syscall.SysProcAttr carries gc's exact field set —
//     including the 8 fields the stale "not in M8 vocabulary" excuse
//     dropped.
//  4. (P1) os.OpenInRoot exists; (*os.Root) has gc's full method set;
//     the phantom (*Root).ReadDir is gone; the new multi-result
//     fail-fasts are wired in osMultiResultFuncs.

import (
	"go/ast"
	"go/types"
	"testing"
)

// reviewOsPkg returns the type-checked synthetic os package. A
// parse/type failure panics inside buildSyntheticPackage — that panic is
// itself the regression signal.
func reviewOsPkg(t *testing.T) *syntheticPackage {
	t.Helper()
	sp := getSyntheticPackage("os")
	if sp == nil || sp.pkg == nil {
		t.Fatal("synthetic os package not loaded")
	}
	return sp
}

// reviewFuncDecl finds a package-level func by name in the synthetic
// package's parsed source.
func reviewFuncDecl(t *testing.T, sp *syntheticPackage, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range sp.files[0].Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
			return fd
		}
	}
	t.Fatalf("synthetic %s: func %s not found", sp.path, name)
	return nil
}

// reviewBodyPanics reports whether a function body contains a panic(...)
// call anywhere.
func reviewBodyPanics(fd *ast.FuncDecl) bool {
	panics := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
			panics = true
			return false
		}
		return true
	})
	return panics
}

// reviewBodyCallsIdent reports whether a body calls a same-package
// function by bare name.
func reviewBodyCallsIdent(fd *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

// reviewBodyCallsSelector reports whether a body calls pkg.name(...).
func reviewBodyCallsSelector(fd *ast.FuncDecl, pkg, name string) bool {
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if xid, ok := sel.X.(*ast.Ident); ok && xid.Name == pkg && sel.Sel.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

// TestReviewOsExpandIsReal pins issue 1: Expand is gc's real
// implementation (no panic stub), delegating the name scan to the
// single-result __getShellName; getShellName keeps gc's exact
// (string, int) signature as an unreachable stub (v1 cannot emit
// multi-result funcdecls).
func TestReviewOsExpandIsReal(t *testing.T) {
	sp := reviewOsPkg(t)
	if fd := reviewFuncDecl(t, sp, "Expand"); reviewBodyPanics(fd) {
		t.Error("os.Expand still panics: the P0 stub was not replaced with gc's implementation")
	} else if !reviewBodyCallsIdent(fd, "__getShellName") {
		t.Error("os.Expand does not call __getShellName")
	}
	if fd := reviewFuncDecl(t, sp, "__getShellName"); reviewBodyPanics(fd) {
		t.Error("os.__getShellName still panics")
	}
	obj := sp.pkg.Scope().Lookup("getShellName")
	fn, ok := obj.(*types.Func)
	if !ok {
		t.Fatal("os.getShellName missing from package scope")
	}
	if sig := fn.Type().(*types.Signature); sig.Results().Len() != 2 {
		t.Errorf("os.getShellName has %d results, want 2 (gc signature)", sig.Results().Len())
	}
}

// TestReviewOsReadFromWriteToD4 pins issue 2: the __-helpers implement
// the D4 copy semantics instead of panicking on the stale trigger.
func TestReviewOsReadFromWriteToD4(t *testing.T) {
	sp := reviewOsPkg(t)
	rf := reviewFuncDecl(t, sp, "__fileReadFrom")
	if reviewBodyPanics(rf) {
		t.Error("__fileReadFrom still panics on the stale P0-2 re-test trigger")
	}
	if !reviewBodyCallsSelector(rf, "io", "ReadAll") {
		t.Error("__fileReadFrom does not call io.ReadAll (D4: ReadFrom = io.ReadAll + concrete write)")
	}
	wt := reviewFuncDecl(t, sp, "__fileWriteTo")
	if reviewBodyPanics(wt) {
		t.Error("__fileWriteTo still panics on the stale P0-2 re-test trigger")
	}
	if !reviewBodyCallsSelector(wt, "io", "WriteString") {
		t.Error("__fileWriteTo does not call io.WriteString (D4: WriteTo = concrete read-all + io.WriteString)")
	}
}

// TestReviewOsProcAttrSys pins issue 3: ProcAttr.Sys is
// *syscall.SysProcAttr, and the synthetic SysProcAttr has gc's full
// field set (the 8 previously dropped fields included).
func TestReviewOsProcAttrSys(t *testing.T) {
	sp := reviewOsPkg(t)
	syscallSp := getSyntheticPackage("syscall")
	if syscallSp == nil || syscallSp.pkg == nil {
		t.Fatal("synthetic syscall package not loaded")
	}

	// os.ProcAttr.Sys must be *syscall.SysProcAttr.
	obj := sp.pkg.Scope().Lookup("ProcAttr")
	if obj == nil {
		t.Fatal("os.ProcAttr missing from package scope")
	}
	nt, ok := obj.Type().(*types.Named)
	if !ok {
		t.Fatalf("os.ProcAttr is %T, want named struct", obj.Type())
	}
	st, ok := nt.Underlying().(*types.Struct)
	if !ok {
		t.Fatal("os.ProcAttr underlying is not a struct")
	}
	var sysField *types.Var
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == "Sys" {
			sysField = st.Field(i)
		}
	}
	if sysField == nil {
		t.Fatal("os.ProcAttr has no Sys field")
	}
	ptr, ok := sysField.Type().(*types.Pointer)
	if !ok {
		t.Fatalf("os.ProcAttr.Sys is %s, want *syscall.SysProcAttr", sysField.Type())
	}
	sysObj := syscallSp.pkg.Scope().Lookup("SysProcAttr")
	if sysObj == nil {
		t.Fatal("syscall.SysProcAttr missing from package scope")
	}
	wantSys := sysObj.Type()
	if !types.Identical(ptr.Elem(), wantSys) {
		t.Errorf("os.ProcAttr.Sys = %s, want *syscall.SysProcAttr", sysField.Type())
	}

	// syscall.SysProcAttr must carry gc 1.27.1's exact field set.
	snt, ok := wantSys.(*types.Named)
	if !ok {
		t.Fatal("syscall.SysProcAttr is not a named type")
	}
	sst, ok := snt.Underlying().(*types.Struct)
	if !ok {
		t.Fatal("syscall.SysProcAttr underlying is not a struct")
	}
	have := map[string]bool{}
	for i := 0; i < sst.NumFields(); i++ {
		have[sst.Field(i).Name()] = true
	}
	for _, f := range []string{
		"Chroot", "Credential", "Ptrace", "Setsid", "Setpgid",
		"Setctty", "Noctty", "Ctty", "Foreground", "Pgid",
		"Pdeathsig", "Cloneflags",
		// The 8 fields the stale "vocabulary" excuse dropped:
		"Unshareflags", "UidMappings", "GidMappings",
		"GidMappingsEnableSetgroups", "AmbientCaps",
		"UseCgroupFD", "CgroupFD", "PidFD",
	} {
		if !have[f] {
			t.Errorf("syscall.SysProcAttr missing gc field %s", f)
		}
	}

	// syscall.SysProcIDMap must exist for the Uid/GidMappings fields.
	mobj := syscallSp.pkg.Scope().Lookup("SysProcIDMap")
	mnt, ok := mobj.Type().(*types.Named)
	if !ok {
		t.Fatal("syscall.SysProcIDMap missing or not a named type")
	}
	mst, ok := mnt.Underlying().(*types.Struct)
	if !ok {
		t.Fatal("syscall.SysProcIDMap underlying is not a struct")
	}
	for _, f := range []string{"ContainerID", "HostID", "Size"} {
		found := false
		for i := 0; i < mst.NumFields(); i++ {
			if mst.Field(i).Name() == f {
				found = true
			}
		}
		if !found {
			t.Errorf("syscall.SysProcIDMap missing field %s", f)
		}
	}
}

// reviewRootMethods are gc 1.27.1's exact (*os.Root) method names.
var reviewRootMethods = []string{
	"Name", "Close", "Open", "Create", "OpenFile", "OpenRoot",
	"Chmod", "Mkdir", "MkdirAll", "Chown", "Lchown", "Chtimes",
	"Remove", "RemoveAll", "Stat", "Lstat", "Readlink", "Rename",
	"Link", "Symlink", "ReadFile", "WriteFile", "FS",
}

// TestReviewOsRootSurface pins issue 4: (*os.Root) has gc's full method
// set, the phantom ReadDir is gone, and os.OpenInRoot exists.
func TestReviewOsRootSurface(t *testing.T) {
	sp := reviewOsPkg(t)
	obj := sp.pkg.Scope().Lookup("Root")
	if obj == nil {
		t.Fatal("os.Root missing from package scope")
	}
	nt, ok := obj.Type().(*types.Named)
	if !ok {
		t.Fatal("os.Root is not a named type")
	}
	ms := types.NewMethodSet(types.NewPointer(nt))
	for _, m := range reviewRootMethods {
		if ms.Lookup(nil, m) == nil {
			t.Errorf("(*os.Root) missing gc method %s", m)
		}
	}
	if ms.Lookup(nil, "ReadDir") != nil {
		t.Error("phantom (*os.Root).ReadDir still present — no such method in gc")
	}
	if sp.pkg.Scope().Lookup("OpenInRoot") == nil {
		t.Error("os.OpenInRoot missing from package scope")
	}
}

// TestReviewOsNewDesugarEntriesWired pins that the three new
// multi-result fail-fasts are in osMultiResultFuncs with helpers, pair
// structs, and field names matching synth_os.go exactly (the table's
// stated contract).
func TestReviewOsNewDesugarEntriesWired(t *testing.T) {
	sp := reviewOsPkg(t)
	type want struct {
		name, recv, helper, pair string
		fields                   []string
	}
	for _, w := range []want{
		{"OpenInRoot", "", "__openInRoot", "openInRootPair", []string{"f", "err"}},
		{"OpenRoot", "Root", "__rootOpenRoot", "rootOpenRootPair", []string{"root", "err"}},
		{"Readlink", "Root", "__rootReadlink", "rootReadlinkPair", []string{"s", "err"}},
	} {
		found := false
		for _, mf := range osMultiResultFuncs {
			if mf.pkgPath != "os" || mf.name != w.name || mf.recvType != w.recv {
				continue
			}
			found = true
			if mf.helper != w.helper || mf.pairType != w.pair || len(mf.pairFields) != len(w.fields) {
				t.Errorf("osMultiResultFuncs os.%s%s: helper/pair/arity mismatch: %+v", w.recv, w.name, mf)
			} else {
				for i, f := range w.fields {
					if mf.pairFields[i] != f {
						t.Errorf("osMultiResultFuncs os.%s%s: field %d = %q, want %q", w.recv, w.name, i, mf.pairFields[i], f)
					}
				}
			}
		}
		if !found {
			t.Errorf("osMultiResultFuncs missing entry for os.%s%s", w.recv, w.name)
		}
		hobj := sp.pkg.Scope().Lookup(w.helper)
		if _, ok := hobj.(*types.Func); !ok {
			t.Errorf("synthetic os helper %s missing from package scope", w.helper)
		}
		pobj := sp.pkg.Scope().Lookup(w.pair)
		if pobj == nil {
			t.Errorf("synthetic os pair struct %s missing from package scope", w.pair)
			continue
		}
		pst, ok := pobj.Type().Underlying().(*types.Struct)
		if !ok {
			t.Errorf("synthetic os %s is not a struct", w.pair)
			continue
		}
		if pst.NumFields() != len(w.fields) {
			t.Errorf("synthetic os %s has %d fields, want %d", w.pair, pst.NumFields(), len(w.fields))
			continue
		}
		for i, f := range w.fields {
			if pst.Field(i).Name() != f {
				t.Errorf("synthetic os %s field %d = %q, want %q", w.pair, i, pst.Field(i).Name(), f)
			}
		}
	}
}

// TestReviewOsGuestExport is the end-to-end proof: a guest program
// exercising all four fixes exports without error. Exporting an
// os-importing program emits every synthetic os body — including the
// new __fileReadFrom/__fileWriteTo with their cross-package io.ReadAll
// / io.WriteString calls — so success proves the Appendix-M desugar
// fires inside synthetic bodies (the failure mode the old P0-2 trap
// guarded against).
func TestReviewOsGuestExport(t *testing.T) {
	const src = `package main

import (
	"bytes"
	"os"
	"strings"
	"syscall"
)

func main() {
	// Issue 1: Expand/ExpandEnv are real.
	_ = os.Expand("a${X}b", func(s string) string { return s })
	_ = os.ExpandEnv("a${X}b")
	// Issue 3: ProcAttr.Sys typing.
	_ = &os.ProcAttr{Sys: &syscall.SysProcAttr{
		Unshareflags: 1,
		UidMappings:  []syscall.SysProcIDMap{{ContainerID: 0, HostID: 1000, Size: 1}},
	}}
	// Issue 4: OpenInRoot desugars to its fail-fast helper.
	_, _ = os.OpenInRoot("/tmp", "nope")
	// Issue 2: ReadFrom/WriteTo desugar to the D4 helpers.
	f, _ := os.Open("/tmp/nope2")
	if f != nil {
		var b bytes.Buffer
		_, _ = f.ReadFrom(strings.NewReader("z"))
		_, _ = f.WriteTo(&b)
	}
}
`
	doc := exportSrc(t, src)
	if len(doc) == 0 {
		t.Fatal("ExportFile returned an empty document")
	}
}
