package main

import (
	"go/token"
	"go/types"
	"os"
	"regexp"
	"strings"
	"testing"
)

// probeFunc builds a synthetic *types.Func in package pkgPath with the
// given number of results, for testing the registry-driven predicate
// and scope error without involving the exporter.
func probeFunc(pkgPath string, nresults int) *types.Func {
	name := pkgPath
	if i := strings.LastIndex(pkgPath, "/"); i >= 0 {
		name = pkgPath[i+1:]
	}
	pkg := types.NewPackage(pkgPath, name)
	errType := types.Universe.Lookup("error").Type()
	results := make([]*types.Var, nresults)
	for i := range results {
		typ := types.Type(types.Typ[types.Int])
		if i == nresults-1 {
			typ = errType
		}
		results[i] = types.NewVar(token.NoPos, pkg, "", typ)
	}
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(), types.NewTuple(results...), false)
	return types.NewFunc(token.NoPos, pkg, "Probe", sig)
}

// TestMultiResultSyntheticMilestoneRegistry pins the registry contract:
// every registry entry must make the predicate true and the scope error
// carry exactly that entry's milestone, and the milestone distribution
// must match the pre-registry lists (40 M5 / 18 M7 / 21 M8 / 20 M9 = 99).
// This test is deliberately capable of failing: it constructs real
// *types.Func values and runs both production functions on them.
func TestMultiResultSyntheticMilestoneRegistry(t *testing.T) {
	wantCounts := map[string]int{"M5": 40, "M7": 18, "M8": 21, "M9": 20}
	gotCounts := map[string]int{}
	for pkgPath, milestone := range multiResultSyntheticMilestones {
		switch milestone {
		case "M5", "M7", "M8", "M9":
		default:
			t.Errorf("registry: package %q has unknown milestone %q", pkgPath, milestone)
		}
		gotCounts[milestone]++
		fn := probeFunc(pkgPath, 2)
		if !isMultiResultSynthetic(fn) {
			t.Errorf("registry: isMultiResultSynthetic(%q) = false, want true", pkgPath)
		}
		err := multiResultSyntheticError(fn)
		if err == nil || !strings.Contains(err.Error(), "not in "+milestone+" scope") {
			t.Errorf("registry: multiResultSyntheticError(%q) = %v, want %q scope error", pkgPath, err, milestone)
		}
	}
	if len(multiResultSyntheticMilestones) != 99 {
		t.Errorf("registry: %d entries, want 99 (a package was added or removed without updating this test)", len(multiResultSyntheticMilestones))
	}
	for milestone, want := range wantCounts {
		if gotCounts[milestone] != want {
			t.Errorf("registry: %d %s entries, want %d", gotCounts[milestone], milestone, want)
		}
	}
}

// Packages that appear in a desugar table or the export switch but are
// deliberately NOT fail-fast scope. If a package is added to a table or
// the switch without a registry entry, the drift test below fails unless
// the package is added here with a reason — that edit is the reviewed
// decision, not a silent drift.
var (
	// tableOnlyPackages: desugar-table packages whose multi-result
	// calls are table-driven (or, for crypto/tls and crypto/x509,
	// resolved from real disk sources now), so they never reach the
	// fail-fast scope error.
	tableOnlyPackages = map[string]bool{
		"bufio": true, "math/big": true, "math/bits": true, "math/cmplx": true,
		"math/rand": true, "path": true, "net/http": true, "net/http/httptest": true,
		"crypto/tls": true, "crypto/x509": true,
		"hash/adler32": true, "hash/crc32": true, "hash/crc64": true,
		"hash/fnv": true, "hash/maphash": true,
	}
	// exportOnlyPackages: synthetic-import closure packages with no
	// multi-result fail-fast scope (import-only: no table entry and
	// no fail-fast predicate entry).
	exportOnlyPackages = map[string]bool{
		"cmp": true, "container/heap": true, "container/list": true, "container/ring": true,
		"flag": true, "maps": true, "math/rand/v2": true, "regexp/syntax": true,
		"slices": true, "testing": true, "testing/quick": true, "testing/iotest": true,
		"text/scanner": true, "unicode": true, "unicode/utf8": true, "unicode/utf16": true,
	}
	// failFastInternalPackages: registry packages that appear in
	// neither the export switch nor any desugar table. They are
	// synthetic-only internal dependencies (imported by synthetic
	// sources, never named by a user import) whose internal
	// multi-result calls still fail fast.
	failFastInternalPackages = map[string]bool{
		"crypto/internal/boring/bbig": true, "crypto/internal/constanttime": true,
		"crypto/internal/fips140": true, "crypto/internal/fips140/alias": true,
		"crypto/internal/fips140/check": true, "crypto/internal/fips140/drbg": true,
		"crypto/internal/fips140/hkdf": true, "crypto/internal/fips140/subtle": true,
		"crypto/internal/fips140cache": true, "crypto/internal/fips140deps/byteorder": true,
		"crypto/internal/fips140hash": true, "crypto/internal/fips140only": true,
		"crypto/internal/rand": true, "internal/bytealg": true,
		"internal/nettrace": true, "internal/poll": true,
		"internal/reflectlite": true, "internal/singleflight": true,
		"net/textproto": true,
	}
)

// exportSwitchPackages parses export.go (the test runs with the package
// directory as its working directory) and returns the package paths in
// the synthetic-import switch (export.go's need-flag switch over user
// imports). It fails the test if the switch cannot be found, so a
// refactor of that switch cannot silently void this check.
func exportSwitchPackages(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("export.go")
	if err != nil {
		t.Fatalf("drift test: cannot read export.go: %v", err)
	}
	caseRe := regexp.MustCompile(`^\s*case "([^"]+)":`)
	out := map[string]bool{}
	inSwitch := false
	for _, line := range strings.Split(string(src), "\n") {
		if strings.Contains(line, "switch strings.Trim(imp.Path.Value") {
			if inSwitch {
				t.Fatalf("drift test: export switch marker found twice; parser is ambiguous")
			}
			inSwitch = true
			continue
		}
		if !inSwitch {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "default:") {
			break
		}
		if m := caseRe.FindStringSubmatch(line); m != nil {
			out[m[1]] = true
		}
	}
	if !inSwitch {
		t.Fatalf("drift test: export switch marker not found in export.go")
	}
	if len(out) != 85 {
		t.Errorf("drift test: export switch has %d cases, want 85 (a package was added or removed without updating this test)", len(out))
	}
	return out
}

// TestPackageRegistryDrift is the P1-02 drift guard. It cross-checks the
// three package-name-keyed sites at package granularity:
//   - every package in the export switch or any MR desugar table must
//     be in the registry or in a documented allowlist above;
//   - every registry package must be anchored to the export switch, a
//     desugar table, or the internal-dependency allowlist;
//   - every allowlist entry must still be live (it must actually appear
//     where its list claims, and must not have moved into the registry).
//
// A new package added to any one site without a registry entry or an
// allowlist edit fails here. New functions in an already-listed package
// do not fail: the guard is package-level, matching the finding.
func TestPackageRegistryDrift(t *testing.T) {
	exportPkgs := exportSwitchPackages(t)
	tablePkgs := map[string]bool{}
	for _, mf := range m7MultiResultFuncs {
		tablePkgs[mf.pkgPath] = true
	}
	for _, mf := range m8MultiResultFuncs {
		tablePkgs[mf.pkgPath] = true
	}
	for _, mf := range osMultiResultFuncs {
		tablePkgs[mf.pkgPath] = true
	}
	for _, mf := range m9MultiResultFuncs {
		tablePkgs[mf.pkgPath] = true
	}
	for _, mf := range ioMultiResultFuncs {
		tablePkgs[mf.pkgPath] = true
	}

	sited := func(pkg string) bool { return exportPkgs[pkg] || tablePkgs[pkg] }

	for pkg := range exportPkgs {
		if multiResultSyntheticMilestones[pkg] == "" && !tableOnlyPackages[pkg] && !exportOnlyPackages[pkg] {
			t.Errorf("drift: package %q is in the export switch but not in multiResultSyntheticMilestones or any allowlist; add a registry entry or document the exception", pkg)
		}
	}
	for pkg := range tablePkgs {
		if multiResultSyntheticMilestones[pkg] == "" && !tableOnlyPackages[pkg] && !exportOnlyPackages[pkg] {
			t.Errorf("drift: package %q is in a desugar table but not in multiResultSyntheticMilestones or any allowlist; add a registry entry or document the exception", pkg)
		}
	}
	for pkg := range multiResultSyntheticMilestones {
		if !sited(pkg) && !failFastInternalPackages[pkg] {
			t.Errorf("drift: registry package %q is in neither the export switch, any desugar table, nor the internal-dependency allowlist; it is a stale registry entry", pkg)
		}
	}
	for pkg := range tableOnlyPackages {
		if multiResultSyntheticMilestones[pkg] != "" {
			t.Errorf("drift: allowlisted table-only package %q is now in the registry; remove it from tableOnlyPackages", pkg)
		}
		if !tablePkgs[pkg] {
			t.Errorf("drift: allowlisted table-only package %q no longer appears in any desugar table; remove it from tableOnlyPackages", pkg)
		}
	}
	for pkg := range exportOnlyPackages {
		if multiResultSyntheticMilestones[pkg] != "" {
			t.Errorf("drift: allowlisted export-only package %q is now in the registry; remove it from exportOnlyPackages", pkg)
		}
		if !exportPkgs[pkg] {
			t.Errorf("drift: allowlisted export-only package %q no longer appears in the export switch; remove it from exportOnlyPackages", pkg)
		}
	}
	for pkg := range failFastInternalPackages {
		if multiResultSyntheticMilestones[pkg] == "" {
			t.Errorf("drift: internal-dependency package %q is no longer in the registry; remove it from failFastInternalPackages", pkg)
		}
		if sited(pkg) {
			t.Errorf("drift: internal-dependency package %q now appears in the export switch or a desugar table; remove it from failFastInternalPackages", pkg)
		}
	}
}

// TestNonRegistryPackagesAreNotFailFast pins the intentional
// distinction the drift test documents: table/export packages outside
// the registry must not fail fast, and a single-result function in a
// registry package must not fail fast either (the result-count check
// in isMultiResultSynthetic is unchanged by the registry move).
func TestNonRegistryPackagesAreNotFailFast(t *testing.T) {
	for _, pkg := range []string{"fmt", "math/bits", "crypto/tls", "crypto/x509", "hash/adler32", "bufio", "net/http", "path"} {
		if isMultiResultSynthetic(probeFunc(pkg, 2)) {
			t.Errorf("isMultiResultSynthetic(%q) = true, want false (table/export-only package, not fail-fast scope)", pkg)
		}
	}
	for _, pkg := range []string{"bytes", "io", "net", "time", "crypto/ecdsa"} {
		if isMultiResultSynthetic(probeFunc(pkg, 1)) {
			t.Errorf("isMultiResultSynthetic(%q, 1 result) = true, want false", pkg)
		}
		if isMultiResultSynthetic(probeFunc(pkg, 0)) {
			t.Errorf("isMultiResultSynthetic(%q, 0 results) = true, want false", pkg)
		}
	}
	// The asymmetric-crypto quirk, pinned as behaviour, not as the old
	// comment claimed it: crypto/ecdsa is fail-fast with the M5 tag.
	if err := multiResultSyntheticError(probeFunc("crypto/ecdsa", 2)); err == nil || !strings.Contains(err.Error(), "not in M5 scope") {
		t.Errorf("multiResultSyntheticError(crypto/ecdsa) = %v, want M5 scope error (old code's default, not the M7 the old comment claimed)", err)
	}
	// A package outside the registry keeps the old M5 default label
	// when the error function is called on it directly.
	if err := multiResultSyntheticError(probeFunc("crypto/tls", 2)); err == nil || !strings.Contains(err.Error(), "not in M5 scope") {
		t.Errorf("multiResultSyntheticError(crypto/tls) = %v, want M5 default scope error", err)
	}
}
