package main

// Tests for stdlib-linkdiff. Every test is written against the
// synthetic fixture tree in testdata/fixture (a fake GOROOT) plus the
// real graalgo tree for provided-set extraction.
//
// Non-tautology design: each test asserts a specific classification
// that flips if the corresponding logic is broken; TestTagFilterHasEffect
// runs the scan with the platform filter disabled and asserts the
// gated linknames ARE counted (proving the filter does the excluding,
// not the scanner silently dropping files); the red-green log in
// redgreen/ shows TestBuildTagGatedExcluded going red when the filter
// is mutated out.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func graalgoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// <graalgo>/tools/stdlib-linkdiff/main_test.go -> <graalgo>
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "testdata", "fixture")
}

func fixtureReport(t *testing.T) *Report {
	t.Helper()
	graalgo := graalgoRoot(t)
	provided, err := loadProvided(options{graalgo: graalgo})
	if err != nil {
		t.Fatalf("loadProvided: %v", err)
	}
	routes, err := loadRoutes(filepath.Join(fixtureRoot(t), "..", "routes.txt"))
	if err != nil {
		t.Fatalf("loadRoutes: %v", err)
	}
	scan, err := ScanTree(fixtureRoot(t), "linux", "amd64", nil)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	return Classify(scan, provided, routes, options{graalgo: graalgo})
}

func entryByLocal(rep *Report, local string) *Entry {
	for i := range rep.Entries {
		if rep.Entries[i].Local == local {
			return &rep.Entries[i]
		}
	}
	return nil
}

func TestSatisfied(t *testing.T) {
	rep := fixtureReport(t)
	e := entryByLocal(rep, "okLocal")
	if e == nil {
		t.Fatal("okLocal entry missing from report")
	}
	if e.Status != StatusSatisfied {
		t.Fatalf("okLocal: got %s, want SATISFIED", e.Status)
	}
	if e.Target != "runtime.fastrand" {
		t.Fatalf("okLocal target: got %q, want runtime.fastrand", e.Target)
	}
	if !strings.Contains(e.ProvidedFrom, "Linknames.register") {
		t.Fatalf("okLocal provided_from: got %q, want Linknames.register provenance", e.ProvidedFrom)
	}
	// The linux&&amd64-gated file must be counted too.
	if e := entryByLocal(rep, "selLocal"); e == nil || e.Status != StatusSatisfied {
		t.Fatalf("selLocal: got %+v, want SATISFIED", e)
	}
}

func TestMissingAndSkipList(t *testing.T) {
	rep := fixtureReport(t)
	e := entryByLocal(rep, "missLocal")
	if e == nil {
		t.Fatal("missLocal entry missing from report")
	}
	if e.Status != StatusMissing {
		t.Fatalf("missLocal: got %s, want MISSING", e.Status)
	}
	want := map[string]bool{
		"runtime.noSuchSymbol42": true,
		"runtime.otherMissing":   true,
		"pkgbad.badLocal":        true,
		// pkgvar.varLocal is a live self-link: satisfied, not listed.
	}
	if len(rep.SkipList) != len(want) {
		t.Fatalf("skip-list has %d entries, want %d: %v", len(rep.SkipList), len(want), rep.SkipList)
	}
	for _, s := range rep.SkipList {
		if !want[s.Target] {
			t.Fatalf("unexpected skip-list target %q", s.Target)
		}
		if s.LinknameRefs < 1 {
			t.Fatalf("skip-list target %q has no refs", s.Target)
		}
		delete(want, s.Target)
	}
	if len(want) > 0 {
		t.Fatalf("skip-list missing targets: %v", want)
	}
}

func TestBuildTagGatedExcluded(t *testing.T) {
	rep := fixtureReport(t)
	for _, local := range []string{"darwinLocal", "windowsLocal"} {
		if e := entryByLocal(rep, local); e != nil {
			t.Fatalf("%s: counted despite excluding build tag: %+v", local, e)
		}
	}
	if rep.Counts.Linknames != 7 {
		t.Fatalf("linkname count: got %d, want 7 (gated files must not be counted)", rep.Counts.Linknames)
	}
	if rep.Counts.ConstraintExcluded != 2 {
		t.Fatalf("constraint-excluded files: got %d, want 2", rep.Counts.ConstraintExcluded)
	}
}

// TestTagFilterHasEffect proves the build-tag filter is what excludes
// the gated linknames: with the platform filter disabled, they ARE
// counted. If tag filtering were removed from the scanner, this test
// still passes but TestBuildTagGatedExcluded goes red (see redgreen/).
func TestTagFilterHasEffect(t *testing.T) {
	scan, err := scanTree(fixtureRoot(t), "linux", "amd64", nil,
		func(dir, name string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	seen := map[string]bool{}
	for _, ln := range scan.Linknames {
		seen[ln.Local] = true
	}
	for _, local := range []string{"darwinLocal", "windowsLocal"} {
		if !seen[local] {
			t.Fatalf("%s: not counted with filter disabled — the filter cannot be what excludes it", local)
		}
	}
	if scan.ConstraintExcluded != 0 {
		t.Fatalf("constraint-excluded with disabled filter: got %d, want 0", scan.ConstraintExcluded)
	}
}

func TestTestFilesExcluded(t *testing.T) {
	rep := fixtureReport(t)
	if e := entryByLocal(rep, "testLocal"); e != nil {
		t.Fatalf("testLocal: _test.go linkname counted: %+v", e)
	}
	if rep.Counts.TestFilesExcluded != 1 {
		t.Fatalf("test files excluded: got %d, want 1", rep.Counts.TestFilesExcluded)
	}
}

func TestMalformedNotDropped(t *testing.T) {
	rep := fixtureReport(t)
	e := entryByLocal(rep, "badLocal")
	if e == nil {
		t.Fatal("badLocal: malformed directive silently dropped from report")
	}
	if e.Status != StatusMissing {
		t.Fatalf("badLocal: got %s, want MISSING", e.Status)
	}
	if !strings.Contains(e.Reason, "malformed") {
		t.Fatalf("badLocal reason: got %q, want malformed-directive reason", e.Reason)
	}
}

func TestSelfLink(t *testing.T) {
	rep := fixtureReport(t)
	e := entryByLocal(rep, "varLocal")
	if e == nil {
		t.Fatal("varLocal entry missing")
	}
	if e.Target != "pkgvar.varLocal" {
		t.Fatalf("varLocal target: got %q, want pkgvar.varLocal (self-link)", e.Target)
	}
	// A live self-link is a provision, not a need: the target IS the
	// declared symbol, provided by the compiled package itself.
	if e.Status != StatusSatisfied {
		t.Fatalf("varLocal: got %s, want SATISFIED (self-link)", e.Status)
	}
	if e.ProvidedFrom != "self" {
		t.Fatalf("varLocal: ProvidedFrom = %q, want %q", e.ProvidedFrom, "self")
	}
}

func TestRouted(t *testing.T) {
	rep := fixtureReport(t)
	e := entryByLocal(rep, "wbLocal")
	if e == nil {
		t.Fatal("wbLocal entry missing")
	}
	if e.Status != StatusRouted {
		t.Fatalf("wbLocal: got %s, want ROUTED", e.Status)
	}
	if e.Route != "runtime.writeBarrier -> const:false" {
		t.Fatalf("wbLocal route: got %q", e.Route)
	}
	if e.RouteReason == "" {
		t.Fatal("wbLocal: route reason not recorded")
	}
	if rep.Counts.Routed != 1 {
		t.Fatalf("routed count: got %d, want 1", rep.Counts.Routed)
	}
}

func TestDuplicateLocalNames(t *testing.T) {
	rep := fixtureReport(t)
	found := false
	for _, d := range rep.Duplicates {
		if d.Local == "pkgmiss.missLocal" {
			found = true
			if len(d.Files) != 2 {
				t.Fatalf("duplicate files: got %v, want 2", d.Files)
			}
		}
	}
	if !found {
		t.Fatal("pkgmiss.missLocal not reported as duplicate")
	}
}

// TestSkipListSizeExact: the headline kill-trigger metric must equal
// the skip-list length and the exact expected fixture count.
func TestSkipListSizeExact(t *testing.T) {
	rep := fixtureReport(t)
	if rep.SkipListSize != len(rep.SkipList) {
		t.Fatalf("skip_list_size=%d != len(skip_list)=%d", rep.SkipListSize, len(rep.SkipList))
	}
	if rep.SkipListSize != 3 {
		t.Fatalf("skip_list_size=%d, want exactly 3", rep.SkipListSize)
	}
	// And it must be prominent + machine-readable in the JSON encoding.
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	v, ok := m["skip_list_size"]
	if !ok {
		t.Fatal("JSON report has no top-level skip_list_size")
	}
	if v.(float64) != 3 {
		t.Fatalf("JSON skip_list_size=%v, want 3", v)
	}
}

func TestDecoyNotDirective(t *testing.T) {
	rep := fixtureReport(t)
	for _, e := range rep.Entries {
		if strings.Contains(e.Local, "linknamestd") {
			t.Fatalf("//go:linknamestd comment counted as directive: %+v", e)
		}
	}
}

func TestRealRoutesParse(t *testing.T) {
	graalgo := graalgoRoot(t)
	routes, err := loadRoutes(filepath.Join(graalgo, "tools", "stdlib-linkdiff", "routes.txt"))
	if err != nil {
		t.Fatalf("loadRoutes shipped routes.txt: %v", err)
	}
	found := false
	for _, r := range routes {
		if r.Target == "runtime.writeBarrier" {
			found = true
			if r.Replacement != "const:false" || r.Reason == "" {
				t.Fatalf("writeBarrier route malformed: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("shipped routes.txt has no runtime.writeBarrier route")
	}
}

func TestProvidedExtraction(t *testing.T) {
	provided, err := loadProvided(options{graalgo: graalgoRoot(t)})
	if err != nil {
		t.Fatalf("loadProvided: %v", err)
	}
	// Spot checks across every source family.
	want := []string{
		"runtime.fastrand", "runtime.fastrandn", "runtime.nanotime",
		"graalgo.runtime.Linknames.callGo",
		"runtime.Goexit", "runtime.SetFinalizer", "runtime.KeepAlive", "runtime.GC",
		"fmt.Print", "fmt.Errorf",
		"sync.Mutex", "sync.(*Mutex).Lock", "sync.(*Mutex).TryLock",
		"sync.(*WaitGroup).Go", "sync.Locker", "sync.Locker.Lock",
		"sync/atomic.Int64", "sync/atomic.LoadInt64", "sync/atomic.CompareAndSwapBool",
		"sync/atomic.(*Int64).CompareAndSwap", "sync/atomic.(*Bool).Swap",
		"time.Now", "time.Duration", "time.Nanosecond", "time.Time",
		"time.Duration.String", "time.(*Timer).Stop", "time.Month.String",
		"context.Background", "context.Context", "context.Canceled",
		"context.Context.Done",
		// M10: embedded synthetic packages (syntheticSources registry).
		"graalgo.os.open", "graalgo.net.socket", "graalgo.x509.parse",
		"graalgo.tls.newClientEngine",
		"os.Open", "os.ReadFile",
		"internal/poll.runtime_pollOpen",
		"reflect.TypeOf", "encoding/json.Marshal", "crypto/sha256.New",
		"net/http.Get", "syscall.Getenv", "maps.Clone",
		"internal/reflectlite.TypeOf",
	}
	for _, w := range want {
		if !provided.has(w) {
			t.Errorf("provided set missing %q", w)
		}
	}
	// Bool must NOT have Add (fiveOps is not applied to Bool).
	if provided.has("sync/atomic.(*Bool).Add") {
		t.Error("sync/atomic.(*Bool).Add present: fiveOps must not apply to Bool")
	}
	// No unexported names may leak, except the genuine lowercase
	// targets from the linkname registry (LinknameIntrinsics.java
	// Go→intrinsic direction): runtime.fastrand, the graalgo.*
	// frozen namespaces (lowercase by design — os.md §2.4), the
	// internal/poll.* netpoller table, etc. Provenance — not a
	// hardcoded list — decides: anything the registry genuinely
	// registers is exempt.
	finalIdent := regexp.MustCompile(`([A-Za-z_][\w]*)$`)
	for s, prov := range provided.Symbols {
		if strings.HasPrefix(prov, "p3link LinknameIntrinsics.java") {
			continue
		}
		m := finalIdent.FindStringSubmatch(s)
		if m == nil || !(m[1][0] >= 'A' && m[1][0] <= 'Z') {
			t.Errorf("unexported symbol leaked into provided set: %q", s)
		}
	}
	if len(provided.Symbols) < 200 {
		t.Errorf("provided set suspiciously small: %d symbols", len(provided.Symbols))
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

// (P2-1) routes that match zero linknames are flagged as stale --
// never silently listed under "Routes applied" with no indication.
func TestStaleRoutesFlagged(t *testing.T) {
	graalgo := graalgoRoot(t)
	provided, err := loadProvided(options{graalgo: graalgo})
	if err != nil {
		t.Fatalf("loadProvided: %v", err)
	}
	routesPath := filepath.Join(t.TempDir(), "routes.txt")
	content := "runtime.writeBarrier -> const:false # fixture mirror\n" +
		"bogus.gone -> const:false # no such linkname in the fixture tree\n"
	if err := os.WriteFile(routesPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	routes, err := loadRoutes(routesPath)
	if err != nil {
		t.Fatalf("loadRoutes: %v", err)
	}
	scan, err := ScanTree(fixtureRoot(t), "linux", "amd64", nil)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	rep := Classify(scan, provided, routes, options{graalgo: graalgo})
	if len(rep.StaleRoutes) != 1 || rep.StaleRoutes[0] != "bogus.gone" {
		t.Fatalf("StaleRoutes = %v, want [bogus.gone]", rep.StaleRoutes)
	}
	for _, s := range rep.StaleRoutes {
		if s == "runtime.writeBarrier" {
			t.Fatal("live route runtime.writeBarrier wrongly flagged stale")
		}
	}
	// The stale route is still listed as applied (it was attempted);
	// the flag is the new signal.
	if len(rep.RoutesApplied) != 2 {
		t.Fatalf("RoutesApplied = %d, want 2", len(rep.RoutesApplied))
	}
}
