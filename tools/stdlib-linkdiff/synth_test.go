package main

// M10 (WS I) tests: the synthetic registry, the synthetic-source scan,
// shadow marking, and the live skip-list split.
//
// Non-tautology design: each test asserts a specific mechanical fact
// that flips if the wiring breaks — the registry resolving a known
// source, the shadow set containing a known overlaid package and
// excluding a non-overlaid one, markShadowed tagging a fake scan, the
// synthetic os directive landing in the scan with Package "os".

import (
	"strings"
	"testing"
)

func testRegistry(t *testing.T) *synthRegistry {
	t.Helper()
	reg, err := loadSyntheticRegistry(graalgoRoot(t))
	if err != nil {
		t.Fatalf("loadSyntheticRegistry: %v", err)
	}
	return reg
}

func TestSyntheticRegistryResolves(t *testing.T) {
	reg := testRegistry(t)
	if len(reg.Sources) < 100 {
		t.Fatalf("registry resolved %d sources, want >= 100", len(reg.Sources))
	}
	for _, path := range []string{"os", "net", "mime", "encoding/json", "crypto/sha256", "internal/poll", "syscall"} {
		src, ok := reg.Sources[path]
		if !ok {
			t.Errorf("registry missing source for %q", path)
			continue
		}
		if !strings.Contains(src, "package ") {
			t.Errorf("source for %q has no package clause", path)
		}
		if reg.ConstOf[path] == "" || reg.FileOf[path] == "" {
			t.Errorf("source for %q lacks provenance (const/file)", path)
		}
	}
	// Every overlay-substituted path backed by the registry must resolve
	// to a non-empty source: the map<->const wiring must not rot.
	for path := range reg.Shadowed {
		if _, inMap := reg.Sources[path]; !inMap {
			continue // dedicated stubs (runtime/unsafe), not const-backed
		}
		if reg.Sources[path] == "" {
			t.Errorf("registry source for %q resolved empty", path)
		}
	}
}

func TestSyntheticShadowSet(t *testing.T) {
	reg := testRegistry(t)
	// Overlaid: the overlay substitutes these, so their real-stdlib
	// files are never compiled. (Post-P4 the overlay is map-driven:
	// syntheticSources keys plus the dedicatedSyntheticStubs pair.)
	for _, path := range []string{"os", "runtime", "unsafe", "time", "context",
		"net", "mime", "reflect_m7", "internal/poll", "syscall", "encoding/json"} {
		if !reg.Shadowed[path] {
			t.Errorf("shadow set missing overlaid package %q", path)
		}
	}
	// Not overlaid: their real-stdlib files are still compiled. This
	// includes the retired synthetics (fmt, sync, sync/atomic,
	// net/http, reflect now resolve from disk) — marking any of them
	// shadowed would hide live linkname needs from the skip-list split.
	for _, path := range []string{"database/sql", "net/rpc", "plugin", "runtime/cgo",
		"fmt", "sync", "sync/atomic", "net/http", "reflect"} {
		if reg.Shadowed[path] {
			t.Errorf("shadow set wrongly contains non-overlaid package %q", path)
		}
	}
}

func TestSyntheticParseErrorsSurfaced(t *testing.T) {
	reg := testRegistry(t)
	provided, err := loadProvided(options{graalgo: graalgoRoot(t)})
	if err != nil {
		t.Fatalf("loadProvided: %v", err)
	}
	// Skipped sources must be registry members: nothing may be dropped
	// that the registry does not know about, and nothing skipped may
	// pass silently (the report carries synthetic_parse_errors).
	for _, s := range provided.Skipped {
		// Format: synthetic "<path>": <err>.
		start := strings.Index(s, `"`)
		end := strings.LastIndex(s, `"`)
		if start < 0 || end <= start {
			t.Fatalf("skipped entry malformed: %q", s)
		}
		path := s[start+1 : end]
		if _, ok := reg.Sources[path]; !ok {
			t.Errorf("skipped %q is not a registry source", path)
		}
	}
	// The synthetic regexp/syntax source is currently unparseable (a
	// second import block from stringer output — buildSyntheticPackage
	// would panic importing it). It must appear on Skipped, never
	// vanish silently. If the source is repaired, Skipped shrinks and
	// this loop simply stops matching.
	found := false
	for _, s := range provided.Skipped {
		if strings.Contains(s, `"regexp/syntax"`) {
			found = true
		}
	}
	if src, ok := reg.Sources["regexp/syntax"]; ok && strings.Count(src, "\nimport ") > 1 {
		if !found {
			t.Error("broken synthetic regexp/syntax source not on Skipped")
		}
	}
}

func TestShadowMarking(t *testing.T) {
	reg := testRegistry(t)
	scan := &ScanResult{Linknames: []Linkname{
		{File: "os/file.go", Package: "os", Local: "a", Target: "runtime.b"},
		{File: "runtime/proc.go", Package: "runtime", Local: "c", Target: "runtime.d"},
		{File: "cmd/vendor/golang.org/x/sys/unix/auxv.go", Package: "unix", Local: "e", Target: "runtime.f"},
		{File: "database/sql/sql.go", Package: "sql", Local: "g", Target: "runtime.h"},
		{File: "synthetic/os", Package: "os", Local: "i", Target: "graalgo.os.open"},
	}}
	markShadowed(scan, reg.Shadowed)
	want := map[string]string{
		"os/file.go": "os", "runtime/proc.go": "runtime",
		"cmd/vendor/golang.org/x/sys/unix/auxv.go": "",
		"database/sql/sql.go":                      "", "synthetic/os": "",
	}
	for _, ln := range scan.Linknames {
		if ln.ShadowedBy != want[ln.File] {
			t.Errorf("File %s: ShadowedBy = %q, want %q", ln.File, ln.ShadowedBy, want[ln.File])
		}
	}
}

func TestSyntheticDirectivesScanned(t *testing.T) {
	reg := testRegistry(t)
	lns, _, skipped := scanSyntheticLinknames(reg)
	if len(lns) == 0 {
		t.Fatal("synthetic scan found no linkname directives")
	}
	// The synthetic os package linknames graalgo.os.open: Package must
	// be the import path "os" (the qualifier linkname targets use).
	found := false
	for _, ln := range lns {
		if ln.Target == "graalgo.os.open" {
			found = true
			if ln.Package != "os" {
				t.Errorf("graalgo.os.open directive: Package = %q, want \"os\"", ln.Package)
			}
			if !strings.HasPrefix(ln.File, "synthetic/") {
				t.Errorf("graalgo.os.open directive: File = %q, want synthetic/ prefix", ln.File)
			}
		}
		if ln.ShadowedBy != "" {
			t.Errorf("synthetic directive %s wrongly marked shadowed", ln.Target)
		}
	}
	if !found {
		t.Error("synthetic scan missed the os -> graalgo.os.open directive")
	}
	// Skipped sources are reported, never fatal.
	for _, s := range skipped {
		if !strings.Contains(s, "synthetic") {
			t.Errorf("skipped entry malformed: %q", s)
		}
	}
}

func TestSelfLinkSatisfiedByConstruction(t *testing.T) {
	provided := &ProvidedSet{Symbols: map[string]string{}}
	scan := &ScanResult{}
	scan.addFileDecls("math/big/arith.go", []string{"addVW", "subVW"})
	scan.addFileDecls("p/q.go", []string{"decl"})
	scan.Linknames = []Linkname{
		// Live self-link: the target is the declared symbol itself.
		{File: "math/big/arith.go", Package: "big", Local: "addVW", Declared: "addVW", SelfLink: true},
		// Shadowed self-link: falls through to the normal check
		// (here: MISSING, still marked shadowed, zero live refs).
		{File: "math/big/arith.go", Package: "big", Local: "subVW", Declared: "subVW", SelfLink: true, ShadowedBy: "math/big"},
		// Undeclared self-link: gc rejects, still MISSING.
		{File: "p/q.go", Package: "q", Local: "other", Declared: "decl", SelfLink: true},
	}
	rep := Classify(scan, provided, nil, options{})
	byTarget := map[string]*Entry{}
	for i := range rep.Entries {
		byTarget[rep.Entries[i].Target] = &rep.Entries[i]
	}
	if e := byTarget["big.addVW"]; e == nil || e.Status != StatusSatisfied {
		t.Fatalf("live self-link big.addVW: got %+v, want SATISFIED", e)
	} else if e.ProvidedFrom != "self" {
		t.Errorf("live self-link big.addVW: ProvidedFrom = %q, want %q", e.ProvidedFrom, "self")
	}
	if e := byTarget["big.subVW"]; e == nil || e.Status != StatusMissing {
		t.Fatalf("shadowed self-link big.subVW: got %+v, want MISSING", e)
	} else if e.ShadowedBy != "math/big" {
		t.Errorf("shadowed self-link big.subVW lost its shadow mark")
	}
	if e := byTarget["q.other"]; e == nil || e.Status != StatusMissing {
		t.Fatalf("mismatched self-link: got %+v, want MISSING", e)
	}
	if rep.SkipListSize != 2 {
		t.Fatalf("SkipListSize = %d, want 2 (live self-link must not be listed)", rep.SkipListSize)
	}
}

// TestLocalNamePackageScope: gc resolves a //go:linkname local name
// against the package's declarations, not just the declaration the
// comment is attached to (verified empirically against the gc
// toolchain: the runtime/cgo x_cgo_*/_cgo_* pair pattern is accepted
// iff the local name is declared somewhere in the package).
func TestLocalNamePackageScope(t *testing.T) {
	provided := &ProvidedSet{Symbols: map[string]string{"runtime.ok": "test"}}
	scan := &ScanResult{}
	scan.addFileDecls("cgo/callbacks.go", []string{"x_cgo_init", "_cgo_init"})
	scan.addFileDecls("cgo/other.go", []string{"unrelated"})
	scan.Linknames = []Linkname{
		// Local declared on a *different* declaration in the same
		// package: well-formed (the cgo pair pattern).
		{File: "cgo/callbacks.go", Package: "cgo", Local: "_cgo_init", Declared: "x_cgo_init", Target: "runtime.ok"},
		// Local declared nowhere in the package: gc rejects.
		{File: "cgo/other.go", Package: "cgo", Local: "_cgo_nope", Declared: "unrelated", Target: "runtime.ok"},
	}
	rep := Classify(scan, provided, nil, options{})
	byLocal := map[string]*Entry{}
	for i := range rep.Entries {
		byLocal[rep.Entries[i].Local] = &rep.Entries[i]
	}
	if e := byLocal["_cgo_init"]; e == nil || e.Status != StatusSatisfied {
		t.Fatalf("_cgo_init (package-scope declared): got %+v, want SATISFIED", e)
	}
	if e := byLocal["_cgo_nope"]; e == nil || e.Status != StatusMissing {
		t.Fatalf("_cgo_nope (undeclared): got %+v, want MISSING", e)
	} else if !strings.Contains(e.Reason, "gc rejects") {
		t.Errorf("_cgo_nope reason = %q, want the gc-rejects reason", e.Reason)
	}
}

func TestTargetPackage(t *testing.T) {
	cases := map[string]string{
		"runtime.fastrand":                "runtime",
		"internal/poll.runtime_pollOpen":  "internal/poll",
		"reflect.(*rtype).Name":           "reflect",
		"net/http.(*Transport).RoundTrip": "net/http",
		"go/types.(*Checker).infer":       "go/types",
		"http.cloneTLSConfig":             "http",
		"go:fipsinfo":                     "go:fipsinfo",
		"time.Duration.String":            "time",
	}
	for target, want := range cases {
		if got := targetPackage(target); got != want {
			t.Errorf("targetPackage(%q) = %q, want %q", target, got, want)
		}
	}
}

func TestLiveSkipListSplit(t *testing.T) {
	provided := &ProvidedSet{Symbols: map[string]string{"runtime.ok": "test"}}
	scan := &ScanResult{}
	scan.addFileDecls("database/sql/sql.go", []string{"a"})
	scan.addFileDecls("os/file.go", []string{"b"})
	scan.addFileDecls("x/y.go", []string{"c"})
	scan.Linknames = []Linkname{
		// Live MISSING: non-shadowed file.
		{File: "database/sql/sql.go", Package: "sql", Local: "a", Declared: "a", Target: "runtime.missing1"},
		// Shadowed MISSING: superseded real-stdlib file.
		{File: "os/file.go", Package: "os", Local: "b", Declared: "b", Target: "runtime.missing2", ShadowedBy: "os"},
		// Satisfied.
		{File: "x/y.go", Package: "y", Local: "c", Declared: "c", Target: "runtime.ok"},
	}
	rep := Classify(scan, provided, nil, options{})
	if rep.SkipListSize != 2 {
		t.Fatalf("SkipListSize = %d, want 2", rep.SkipListSize)
	}
	if rep.LiveSkipListSize != 1 {
		t.Fatalf("LiveSkipListSize = %d, want 1", rep.LiveSkipListSize)
	}
	for _, s := range rep.SkipList {
		switch s.Target {
		case "runtime.missing1":
			if s.LiveRefs != 1 {
				t.Errorf("missing1 LiveRefs = %d, want 1", s.LiveRefs)
			}
		case "runtime.missing2":
			if s.LiveRefs != 0 {
				t.Errorf("missing2 LiveRefs = %d, want 0", s.LiveRefs)
			}
		}
	}
	// Entries carry the shadow mark through to the report.
	for _, e := range rep.Entries {
		if e.Target == "runtime.missing2" && e.ShadowedBy != "os" {
			t.Errorf("missing2 entry lost its shadow mark")
		}
	}
}
