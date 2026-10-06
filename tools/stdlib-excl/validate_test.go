package main

// validate_test.go — red-green evidence for the exclusion-list validator.
//
// Each test maps to one hard requirement; every test asserts the REJECT
// (or accept) outcome, so removing the corresponding check turns the test
// red. The mapping:
//   (a) TestValidateRejectsMissingReason   — reason: mandatory
//   (b) TestValidateRejectsUnknownReason   — reason in vocabulary
//   (c) TestValidateRejects26thPackage     — 25-package review cap
//   (d) TestValidateRejectsVersionStaleChange — change requires version bump
//   (+) TestValidateRejectsStaleEntry      — package must exist in the tree
//   (+) TestValidateRejectsDuplicate      — no duplicate file entries
//   (+) TestValidateFivePercentClause      — 5%-of-test-binaries binding cap
//   (+) TestValidateAcceptsValidList       — happy path stays green

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

const testVocab = `reason_codes:
- code: PLATFORM
  description: out of OS/arch scope
- code: CGO
  description: requires import "C"
`

func testList(t *testing.T, dir, name, version, entries string) string {
	t.Helper()
	return writeTemp(t, dir, name,
		"# test fixture\nversion: "+version+"\n"+testVocab+"exclusions:\n"+entries)
}

// runValidateOK runs validate with explicit flags (no git, no real tree)
// and returns stdout/stderr text plus the error.
func runValidateOK(t *testing.T, list, src, baseline string, testBinaries, max int) (string, string, error) {
	t.Helper()
	var stdout, stderr strings.Builder
	args := []string{"--list", list, "--src", src, "--baseline", baseline,
		"--test-binaries", fmt.Sprint(testBinaries), "--max", fmt.Sprint(max)}
	err := runValidate(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// fixtureSrc builds a temp stdlib-src tree containing the named package dirs.
func fixtureSrc(t *testing.T, pkgs ...string) string {
	t.Helper()
	src := t.TempDir()
	for _, p := range pkgs {
		dir := filepath.Join(src, filepath.FromSlash(p))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "x.go"),
			[]byte("package x\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return src
}

func mustContain(t *testing.T, haystack, needle, what string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("%s: expected %q in output, got:\n%s", what, needle, haystack)
	}
}

// (a) validator rejects an entry without a reason code.
func TestValidateRejectsMissingReason(t *testing.T) {
	dir := t.TempDir()
	src := fixtureSrc(t, "runtime/cgo")
	list := testList(t, dir, "list.yaml", "1",
		"- file: runtime/cgo\n  detail: has evidence\n")
	base := testList(t, dir, "base.yaml", "1",
		"- file: runtime/cgo\n  detail: has evidence\n")
	_, stderr, err := runValidateOK(t, list, src, base, 1000, 25)
	if err == nil {
		t.Fatal("expected rejection of entry without reason code, got accept")
	}
	mustContain(t, stderr, "missing reason code", "missing-reason rejection")
}

// (b) validator rejects an unknown reason code.
func TestValidateRejectsUnknownReason(t *testing.T) {
	dir := t.TempDir()
	src := fixtureSrc(t, "runtime/cgo")
	list := testList(t, dir, "list.yaml", "1",
		"- file: runtime/cgo\n  reason: MADE_UP\n  detail: has evidence\n")
	base := testList(t, dir, "base.yaml", "1", "")
	_, stderr, err := runValidateOK(t, list, src, base, 1000, 25)
	if err == nil {
		t.Fatal("expected rejection of unknown reason code, got accept")
	}
	mustContain(t, stderr, `unknown reason code "MADE_UP"`, "unknown-reason rejection")
}

// (c) validator rejects the 26th package (fixture with 26 entries).
func TestValidateRejects26thPackage(t *testing.T) {
	dir := t.TempDir()
	var pkgs []string
	var entries strings.Builder
	for i := 0; i < 26; i++ {
		p := "pkg" + fmt.Sprint(i)
		pkgs = append(pkgs, p)
		entries.WriteString("- file: " + p + "\n  reason: PLATFORM\n  detail: evidence\n")
	}
	src := fixtureSrc(t, pkgs...)
	list := testList(t, dir, "list.yaml", "1", entries.String())
	base := testList(t, dir, "base.yaml", "0", "")
	_, stderr, err := runValidateOK(t, list, src, base, 1000, 25)
	if err == nil {
		t.Fatal("expected rejection of 26 entries, got accept")
	}
	mustContain(t, stderr, "exceed the review cap", "26th-package rejection")
	// And 25 must pass the cap (isolated from other failures).
	var okEntries strings.Builder
	for i := 0; i < 25; i++ {
		okEntries.WriteString("- file: pkg" + fmt.Sprint(i) + "\n  reason: PLATFORM\n  detail: evidence\n")
	}
	list25 := testList(t, dir, "list25.yaml", "1", okEntries.String())
	stdout, _, err := runValidateOK(t, list25, src, base, 1000, 25)
	if err != nil {
		t.Fatalf("25 entries should pass the cap, got: %v", err)
	}
	mustContain(t, stdout, "validate: OK", "25-entry accept")
}

// (d) validator rejects a changed list without a version bump; a bumped
// version is accepted.
func TestValidateRejectsVersionStaleChange(t *testing.T) {
	dir := t.TempDir()
	src := fixtureSrc(t, "runtime/cgo", "plugin")
	base := testList(t, dir, "base.yaml", "1",
		"- file: runtime/cgo\n  reason: CGO\n  detail: evidence\n")
	stale := testList(t, dir, "stale.yaml", "1",
		"- file: runtime/cgo\n  reason: CGO\n  detail: evidence\n- file: plugin\n  reason: PLATFORM\n  detail: evidence\n")
	_, stderr, err := runValidateOK(t, stale, src, base, 1000, 25)
	if err == nil {
		t.Fatal("expected rejection of version-stale change, got accept")
	}
	mustContain(t, stderr, "without a version bump", "version-stale rejection")
	// Same change with a bumped version passes.
	bumped := testList(t, dir, "bumped.yaml", "2",
		"- file: runtime/cgo\n  reason: CGO\n  detail: evidence\n- file: plugin\n  reason: PLATFORM\n  detail: evidence\n")
	stdout, _, err := runValidateOK(t, bumped, src, base, 1000, 25)
	if err != nil {
		t.Fatalf("bumped version should pass, got: %v", err)
	}
	mustContain(t, stdout, "version 2", "bumped-version accept")
}

// Stale entries: a package that no longer exists in the tree is rejected.
func TestValidateRejectsStaleEntry(t *testing.T) {
	dir := t.TempDir()
	src := fixtureSrc(t, "runtime/cgo")
	list := testList(t, dir, "list.yaml", "1",
		"- file: runtime/cgo\n  reason: CGO\n  detail: evidence\n- file: does/not/exist\n  reason: CGO\n  detail: evidence\n")
	base := testList(t, dir, "base.yaml", "0", "")
	_, stderr, err := runValidateOK(t, list, src, base, 1000, 25)
	if err == nil {
		t.Fatal("expected rejection of stale entry, got accept")
	}
	mustContain(t, stderr, "stale entry", "stale-entry rejection")
}

// Duplicate file entries are rejected.
func TestValidateRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	src := fixtureSrc(t, "runtime/cgo")
	list := testList(t, dir, "list.yaml", "1",
		"- file: runtime/cgo\n  reason: CGO\n  detail: evidence\n- file: runtime/cgo\n  reason: CGO\n  detail: evidence\n")
	base := testList(t, dir, "base.yaml", "0", "")
	_, stderr, err := runValidateOK(t, list, src, base, 1000, 25)
	if err == nil {
		t.Fatal("expected rejection of duplicate entry, got accept")
	}
	mustContain(t, stderr, "duplicate file entry", "duplicate rejection")
}

// The 5% clause binds when it is tighter than --max: with 100 test
// binaries, 5% = 5, so 6 entries must be rejected even though 6 < 25.
func TestValidateFivePercentClause(t *testing.T) {
	dir := t.TempDir()
	var pkgs []string
	var entries strings.Builder
	for i := 0; i < 6; i++ {
		p := "pkg" + fmt.Sprint(i)
		pkgs = append(pkgs, p)
		entries.WriteString("- file: " + p + "\n  reason: PLATFORM\n  detail: evidence\n")
	}
	src := fixtureSrc(t, pkgs...)
	list := testList(t, dir, "list.yaml", "1", entries.String())
	base := testList(t, dir, "base.yaml", "0", "")
	_, stderr, err := runValidateOK(t, list, src, base, 100, 25)
	if err == nil {
		t.Fatal("expected five-percent-clause rejection of 6 entries with 100 test binaries, got accept")
	}
	mustContain(t, stderr, "5% of 100 stdlib test binaries", "5%-clause rejection")
	// Exactly 5 passes.
	var okEntries strings.Builder
	for i := 0; i < 5; i++ {
		okEntries.WriteString("- file: pkg" + fmt.Sprint(i) + "\n  reason: PLATFORM\n  detail: evidence\n")
	}
	list5 := testList(t, dir, "list5.yaml", "1", okEntries.String())
	if _, _, err := runValidateOK(t, list5, src, base, 100, 25); err != nil {
		t.Fatalf("5 entries with 100 test binaries should pass, got: %v", err)
	}
}

// Missing detail is rejected: v1 has a no-parking rule — every entry must
// carry evidence.
func TestValidateRejectsMissingDetail(t *testing.T) {
	dir := t.TempDir()
	src := fixtureSrc(t, "runtime/cgo")
	list := testList(t, dir, "list.yaml", "1",
		"- file: runtime/cgo\n  reason: CGO\n")
	base := testList(t, dir, "base.yaml", "0", "")
	_, stderr, err := runValidateOK(t, list, src, base, 1000, 25)
	if err == nil {
		t.Fatal("expected rejection of entry without detail, got accept")
	}
	mustContain(t, stderr, "missing detail", "missing-detail rejection")
}

// A fully valid list passes.
func TestValidateAcceptsValidList(t *testing.T) {
	dir := t.TempDir()
	src := fixtureSrc(t, "runtime/cgo", "plugin")
	list := testList(t, dir, "list.yaml", "1",
		"- file: runtime/cgo\n  reason: CGO\n  detail: SPEC §3.8\n- file: plugin\n  reason: PLATFORM\n  detail: SPEC §3.8\n")
	base := testList(t, dir, "base.yaml", "0", "")
	stdout, _, err := runValidateOK(t, list, src, base, 1000, 25)
	if err != nil {
		t.Fatalf("valid list should pass, got: %v", err)
	}
	mustContain(t, stdout, "validate: OK", "valid-list accept")
}

// The reason_codes: vocabulary section is parse-compatible with the
// GG-0033 readers: it contributes codes, never entries.
func TestParseVocabularySection(t *testing.T) {
	dir := t.TempDir()
	list := testList(t, dir, "list.yaml", "3",
		"- file: runtime/cgo\n  reason: CGO\n  detail: x\n")
	ef, err := ParseExclusionFile(list)
	if err != nil {
		t.Fatal(err)
	}
	if ef.Version != "3" {
		t.Fatalf("version = %q, want 3", ef.Version)
	}
	if len(ef.Codes) != 2 || len(ef.Exclusions) != 1 {
		t.Fatalf("codes=%d exclusions=%d, want 2 and 1", len(ef.Codes), len(ef.Exclusions))
	}
	if ef.Exclusions[0].File != "runtime/cgo" || ef.Exclusions[0].Reason != "CGO" {
		t.Fatalf("entry parsed wrong: %+v", ef.Exclusions[0])
	}
}
