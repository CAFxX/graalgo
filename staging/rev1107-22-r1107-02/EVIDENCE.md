# R-1107-02 — Evidence (lane rev1107-22-r1107-02)

Issue: `graalgo vet` false-fails on multi-file packages (P1, carried from
REVIEW-2026-11-07-07, re-verified at HEAD `59f0a247`).

## Mechanism — verified from disk truth (read-only, main repo, HEAD 59f0a247)

1. `cmd/graalgo/commands.go:258-287` — `cmdVet` loops per file:
   `for _, f := range files { graalgoRun(root, []string{"--check", f}) }`
   (one `--check` invocation per file).
2. `internal/frontend/main.go:8-10` (gofrontend doc): "All files must declare
   the same package. They are type-checked together, so cross-file
   references resolve." A single file given alone therefore fails
   type-check when it references a sibling file's declaration — the
   `--check` gate fails before `vet.Run` ever runs.
3. `internal/frontend/export.go:893-898` (`ExportFiles`) enforces the
   single-package rule mechanically: `gofrontend: %s declares package %s,
   expected %s`.
4. Precedent in the same file: `cmdRun` (package path, `:108`) and
   `cmdBuild` (`:227`) already pass all package files in ONE `--check`
   invocation, excluding `_test.go` files.
5. The analyzer stage already assumes one package: `internal/vet/vet.go:49-95`
   (`vet.Run`) type-checks all files in a single `conf.Check("vetpkg", ...)`.
   The per-file `--check` loop was inconsistent with the stage that follows it.
6. Test gap confirmed: zero references to `cmdVet`/vet in
   `cmd/graalgo/*_test.go` (grep, this run).
7. `internal/backend/interop/.../p3sync/launcher/P3SyncMain.java:144-236`
   accepts `file.go [file.go...]` and passes all to gofrontend — the
   launcher side needs no change.
8. No CI invocation of `graalgo vet` exists anywhere (Makefile, .github,
   docs, tools, test) — no ground truth on test-file handling; the design
   decision below is documented, not inherited.

## The fix (staged)

`commands.go.patch` (applies cleanly to HEAD `59f0a247`; verified by
`patch --dry-run` + byte-identical result) and the full fixed file
`commands.go`:

- `cmdVet` now calls `checkPackages(root, files)` instead of the per-file
  loop. `checkPackages` groups non-test files by directory (one Go package
  per directory) and runs ONE `--check` per group, continuing through all
  groups even after a failure (same fail-collecting behavior as before).
- `_test.go` files are excluded from the `--check` gate, exactly like
  `cmdRun`/`cmdBuild` exclude them from the build: an external test package
  (`package foo_test`) declares a different package name and
  `ExportFiles` would reject the mixed invocation, and the frontend cannot
  resolve the package-under-test import (`diskimport.go`: local module
  packages resolve from neither GOMODCACHE, GOPATH/src, nor GOROOT/src).
  Test files are still parsed and analyzed by `vet.Run` below the gate.
  Internal test files (`package foo`) are excluded too, for the same
  import-resolution reason when they import a local package; this matches
  the `cmdRun`/`cmdBuild` precedent rather than inventing a new rule.
- Grouping by directory (not by a single combined invocation) preserves
  `graalgo vet dirA dirB`: a naive single invocation over two packages
  would newly fail on the package-name mismatch — the review's literal
  wording ("pass all package files in one --check invocation") is correct
  per package, and the grouping is what makes it correct across packages.
  This is a deliberate, documented refinement of the prescribed
  remediation, per the verify-the-remediation rule (AGENTS.md).
- Files named twice are de-duplicated within a group (a duplicate file in
  one `--check` would fail on duplicate declarations; the old loop passed
  it twice harmlessly).
- No new imports needed (`filepath`, `strings` already imported).
  No bridges, no name-keyed special cases, no behavior change to any other
  command. `vet.Run` is unchanged (its multi-package best-effort merge is
  pre-existing behavior; see open questions).

`vet_multifile_test.go` (new file, `cmd/graalgo`, package main) — 5 tests,
all using a fake `graalgo-sync-run` shell script under a temp root, so they
exercise the real `cmdVet` → `graalgoRun` invocation path with no
production-code seam and no backend build:

1. `TestVetMultiFilePackageSingleCheck` — the regression test: a.go defines
   `Foo`, b.go uses it; asserts exactly ONE `--check` invocation containing
   both files, exit 0, "vet: no issues found". Fails on the old code (two
   invocations).
2. `TestVetCheckFailureShortCircuits` — fake backend exits 1; asserts
   cmdVet returns 1 and the analyzers never run.
3. `TestVetTwoPackagesCheckedSeparately` — two dirs; asserts two
   invocations, each containing only its own package's file.
4. `TestVetTestFilesExcludedFromCheck` — external `foo_test` file excluded
   from `--check` but still analyzed (exit 0).
5. `TestVetDuplicateFileCheckedOnce` — same file named twice; asserted
   once in the single invocation.

## Verification done in this run (quota-blocked lane)

- `gofmt -e` parses both staged files clean; both are gofmt-formatted
  (`gofmt -l` empty).
- No identifier collisions with existing `cmd/graalgo/*_test.go`
  (`writeFile` reused from `testsynth_test.go`, same package).
- No `pass.Pkg` dereference in `internal/vet` non-test code — the
  two-package test's nil-Pkg path is safe.
- Patch dry-run applies; patched file is byte-identical to staged
  `commands.go`.
- Load was CLEAR before the (read-only + /tmp-only) work
  (`load-shed.sh backoff` exit 0).

## NOT verified (owed on quota recovery — see RESUME.md)

- No compile: `go build`/`go test` needs a build cache; home quota is
  EDQUOT-exhausted and there is no existing `~/.cache/go-build`
  (a fresh stdlib cache does not fit in /tmp's free space, and the brief
  forbids claiming a worktree/committing while quota is closed).
  RED→GREEN + affected-suite runs are therefore owed by the resume lane.
- No end-to-end `graalgo vet` run against the real backend (same reason).

## Open questions / follow-ups (not in scope for this fix)

- `vet.Run` still type-checks ALL files (possibly several packages, plus
  test files) in one `conf.Check`, best-effort. A multi-package `graalgo
  vet dirA dirB` can therefore produce spurious or missing analyzer
  diagnostics. Pre-existing behavior, unchanged by this fix; grouping
  `vet.Run` per package is a separate, larger redesign of the analyzer
  pass — flag for the coordinator, do not fold into this lane.
- `cmdVet` usage text says `graalgo vet [build flags] [packages]` but it
  parses no build flags (pre-existing; `resolveFiles` would treat a flag
  as a path). Out of scope.
