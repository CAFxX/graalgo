package main

// ADR-0009: the exporter must attest exactly the obj ids of the
// disk-package multi-result stubs it withholds from emission, so the
// backend can tell a vestigial method-table entry apart from a genuinely
// bogus obj id (which must fail fast instead of being silently dropped).

import (
	"go/token"
	"go/types"
	"testing"
)

// stubFuncDeclInfo builds a minimal funcDeclInfo for a method M with a
// multi-result signature on the named type T in package
// example.com/disk — the shape transformDiskMultiResult leaves behind
// for the stub (original name, multi-result signature).
func stubFuncDeclInfo(funcID int) *funcDeclInfo {
	pkg := types.NewPackage("example.com/disk", "disk")
	tname := types.NewTypeName(token.NoPos, pkg, "T", nil)
	named := types.NewNamed(tname, types.NewStruct(nil, nil), nil)
	recv := types.NewVar(token.NoPos, pkg, "", named)
	results := types.NewTuple(
		types.NewVar(token.NoPos, pkg, "", types.Typ[types.Int]),
		types.NewVar(token.NoPos, pkg, "", types.Typ[types.String]),
	)
	sig := types.NewSignatureType(recv, nil, nil, results, nil, false)
	fn := types.NewFunc(token.NoPos, pkg, "M", sig)
	return &funcDeclInfo{obj: fn, funcID: funcID, recv: recv, sig: sig}
}

// withDiskMRSpec registers a single disk-MR spec for the fabricated
// (example.com/disk, M, T) stub, restoring the global table afterwards.
func withDiskMRSpec(t *testing.T) {
	t.Helper()
	oldSpecs := diskMRSpecs
	diskMRSpecs = nil
	t.Cleanup(func() { diskMRSpecs = oldSpecs })
	registerDiskMRSpecs([]diskMRSpec{{
		pkgPath:    "example.com/disk",
		name:       "M",
		recvType:   "T",
		helper:     "__T_M",
		pairType:   "__T_MPair",
		pairFields: []string{"f1", "f2"},
	}})
}

// TestExportFuncDeclRecordsDiskMRStubObjID drives exportFuncDecl on a
// disk-MR stub: the skip must fire (nil node, nil error — the desugar
// rewrote every call to the __-helper) and the stub's obj id must be
// recorded for the document's diskMRStubObjIDs attestation.
func TestExportFuncDeclRecordsDiskMRStubObjID(t *testing.T) {
	withDiskMRSpec(t)
	e := &exporter{}
	n, err := e.exportFuncDecl(stubFuncDeclInfo(42))
	if err != nil {
		t.Fatalf("exportFuncDecl returned error: %v", err)
	}
	if n != nil {
		t.Fatalf("exportFuncDecl emitted a node for a disk-MR stub, want skip (nil)")
	}
	if !e.diskMRStubObjIDs[42] {
		t.Fatalf("stub obj id 42 not recorded in diskMRStubObjIDs: %v", e.diskMRStubObjIDs)
	}
	if len(e.diskMRStubObjIDs) != 1 {
		t.Fatalf("diskMRStubObjIDs = %v, want exactly the stub id", e.diskMRStubObjIDs)
	}
}

// TestExportFuncDeclStubRecordingIdempotent: recording the same stub
// twice (e.g. if a future path re-exports it) must not duplicate the id
// — the attestation is a set, and the wire array must not grow.
func TestExportFuncDeclStubRecordingIdempotent(t *testing.T) {
	withDiskMRSpec(t)
	e := &exporter{}
	for i := 0; i < 2; i++ {
		n, err := e.exportFuncDecl(stubFuncDeclInfo(42))
		if err != nil {
			t.Fatalf("exportFuncDecl returned error: %v", err)
		}
		if n != nil {
			t.Fatalf("exportFuncDecl emitted a node for a disk-MR stub, want skip (nil)")
		}
	}
	if len(e.diskMRStubObjIDs) != 1 || !e.diskMRStubObjIDs[42] {
		t.Fatalf("diskMRStubObjIDs = %v, want exactly {42}", e.diskMRStubObjIDs)
	}
	if got := e.attestedStubObjIDs(); len(got) != 1 || got[0] != 42 {
		t.Fatalf("attestedStubObjIDs() = %v, want [42]", got)
	}
}

// TestAttestedStubObjIDsSorted: the attestation is sorted ascending for
// determinism, and empty-but-non-nil when nothing was withheld (the wire
// field keeps its [] shape instead of null).
func TestAttestedStubObjIDsSorted(t *testing.T) {
	e := &exporter{diskMRStubObjIDs: map[int]bool{9: true, 3: true, 7: true, 3: true}}
	got := e.attestedStubObjIDs()
	want := []int{3, 7, 9}
	if len(got) != len(want) {
		t.Fatalf("attestedStubObjIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attestedStubObjIDs() = %v, want %v", got, want)
		}
	}
	empty := (&exporter{}).attestedStubObjIDs()
	if empty == nil {
		t.Fatalf("attestedStubObjIDs() = nil, want empty non-nil slice (wire [])")
	}
	if len(empty) != 0 {
		t.Fatalf("attestedStubObjIDs() = %v, want empty", empty)
	}
}
