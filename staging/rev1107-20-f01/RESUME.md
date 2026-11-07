# RESUME — lane rev1107-20-f01 (R20-F01, P1) — STAGED, quota-blocked

**Status: fix complete + RED/GREEN verified in /tmp; NOT landed. No worktree
claimed, no branch, no merge, no review requested yet.**

## What this is

R20-F01 (REVIEW-2026-11-07-20, HEAD `59f0a247`): `runtime.*` interception in
`internal/frontend/export.go` discriminated on package NAME, not PATH.
A non-stdlib package named `runtime` defining `Version`/`NumCPU`/`Gosched`/
`Goexit` got hijacked into graalgo wire nodes (or the Goexit scope error).

## Staged artifacts (all in /tmp/rev1107-20-f01/)

| File | Contents |
|---|---|
| `R20-F01.patch` (5,597 B) | **The deliverable.** Unified patch vs HEAD 59f0a247: export.go fix (3 sites) + new test file. Verified: `patch -p1 --dry-run` OK, and a real apply to pristine files reproduces staged `export.go` and `runtime_nonstdlib_test.go` byte-identically. |
| `export.go` | Patched export.go (reference copy). |
| `export.go.orig` | Pristine export.go @ 59f0a247 (458,491 B). |
| `runtime_nonstdlib_test.go` | New regression test (also inside the patch). |
| `EVIDENCE.md` | RED/GREEN + no-regression evidence, mechanism verification. |
| `RESUME.md` | This file. |
| `pkg/` | Patched module copy used for the test runs (no testdata). |
| `apply-check/` | Scratch dir where the patch was applied + byte-compared. |

Deleted on stand-down to relieve /tmp: `pkg-red/`, both test binaries
(kept only as the recorded outputs in EVIDENCE.md).

## Resume steps (on home-quota recovery)

1. Re-probe quota with a real 8 KB home write (byte-count verified) — a
   passing `touch` proves nothing.
2. Claim an exclusive worktree: `~/workspace/system/bin/wt.py new graalgo
   rev1107-20-f01 --chat graalgo-independent-codebase-review` (branch
   `wt/graalgo-independent-codebase-review/rev1107-20-f01`).
3. In the worktree: `git apply /tmp/rev1107-20-f01/R20-F01.patch` (or copy
   the two files), verify `git status` shows only
   `internal/frontend/export.go` (modified) +
   `internal/frontend/runtime_nonstdlib_test.go` (new).
4. Verify: `gofmt -l` clean; `go test -run
   'TestNonStdlibRuntimePackageNotIntercepted|TestRuntime|TestGoexit'
   -count=1 ./internal/frontend/` green; then the full frontend suite
   (`go test ./...` in internal/frontend) + `make snapshot-check` if the
   change is judged behavior-affecting (it is not for stdlib runtime: the
   overlay package path is "runtime", so interception is unchanged for it —
   but the resume agent should confirm via the differential, not trust this).
5. Commit with explicit pathspecs; register the work-registry entry
   (register failed this run on EDQUOT — retry on recovery).
6. Report completion (branch + SHA) to the coordinator for INDEPENDENT
   review before any merge. Do NOT integrate.

## Design notes for the reviewer

- Goexit site (:8564) was restructured, not literally Name→Path swapped:
  the old negative gate (scope error when the resolved package was not
  named "runtime") would have kept rejecting valid non-stdlib
  `runtime.Goexit()` calls. New form: positive intercept only when
  `Path() == "runtime"`, otherwise fall through to the general call path.
  The scope-error string `"runtime.Goexit did not resolve to the overlay
  package"` is deleted — verified this run: no live-code or test references
  it. It survives only in an archived review snapshot
  (`docs/arch/reviews/phase1-m5-loopcap/snapshot/export.go:2405`, historical
  doc — do NOT touch) and two dangling `.git/lost-found` objects.
- Outer `pkgIdent.Name == "runtime"` pre-filters (:6910, :8584) left
  untouched: they only narrow interception, never cause a hijack.
- Open follow-up (NOT fixed here): aliased stdlib runtime
  (`rt "runtime"; rt.NumCPU()`) bypasses the :8585 block via the
  pre-filter and falls through to exportFuncCall — pre-existing behavior,
  unchanged by this fix. Worth a reviewer decision, not a silent change.
- Test uses `t.Setenv("GOPATH", tmp)` + import path
  `example.com/rev1107-20-f01/runtime` (unique — avoids the process-global
  `diskDirCache` colliding with other tests). It does not need testdata/.
- Full frontend suite was NOT run (no testdata in the /tmp copy; /tmp
  too small for a full run). Resume agent must run it in the worktree.

## Environment notes from this run

- Home quota CLOSED the whole run (real 8 KB probe EDQUOT 20:29 CET).
  /tmp tmpfs 512M, was 92% full / 43M free at 20:38, recovered to 86M free
  by 20:40 (another lane freed space). /var/tmp 100% full — never used.
- Shared warm Go cache used: /tmp/graalgo-gocache (stdlib entries warm for
  go/parser, go/ast, go/token, go/constant, go/scanner; no graalgo-frontend
  entries — the frontend package compiled fresh, ~22M cache growth).
  Writes into that cache are Go's normal concurrent-cache behavior; the
  resume agent should prefer a fresh GOCACHE on home once quota recovers.
- Work-registry register failed EDQUOT (no entry exists). No registry
  heartbeat was possible. Lane assignment lives in this file + the
  dispatch handoff.
