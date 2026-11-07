# RESUME — lane rev1107-22-r09 (graalgo R-09, P1)

Status: **BLOCKED-quota — fix staged, verified RED→GREEN in /tmp, nothing
committed, no worktree claimed, no registry entry written.**

## Issue (re-verified from disk truth at HEAD 59f0a247, main repo read-only)

`tools/stdlib-vendor` defaults pointed at the deleted tree:
- `tools/stdlib-vendor/main.go:104-115` — `stdlibForkDir()` returned
  `<repo>/phases/phase3/stdlib-fork`; `defaultManifestPath`/`defaultPinPath`
  derive from it.
- `phases/phase3/stdlib-fork` does **not** exist (confirmed: `ls` fails).
- Live manifest/pin exist at `tools/stdlib-fork/vendor-manifest.json` /
  `tools/stdlib-fork/pinned-version.txt` (confirmed present at HEAD).
- Dead defaults also stated in doc comments: `main.go:8-9,39-40`,
  `vendor.go:57-58,357`, `verify.go:19,166`. `vendor_test.go` had no test
  covering the defaults. Confirmed by repo-wide grep at 2026-11-07 ~21:13 CET.

## Prior-lane verification (rv1107-03, claimed dead in the brief — confirmed)

- Registry `~/workspace/system/work-registry/rv1107-03.json`: status `active`
  but heartbeat `2026-11-07T06:15:37+00:00` = creation time, never updated.
- Branch `wt/graalgo-independent-codebase-review/rv1107-03` tip is
  `bf9b96ff` = its base — zero commits of work.
- No worktree dir `~/workspace/worktrees/graalgo/rv1107-03` exists.
- Conclusion: dead, covers nothing live. This lane does not collide with it.

## Quota blocker (why nothing was claimed/committed)

- Home probe, 2026-11-07 ~21:13 CET: real 8 KB write to
  `~/quota-probe-rev1107-22-r09.bin` failed:
  `OSError: [Errno 122] Disk quota exceeded`. (Home fs itself has 36G avail;
  this is the user quota, EDQUOT flapping since 2026-11-06.)
- `/var/tmp` is 100% full (2.0G, 1.5M avail) — unusable for staging.
- `/tmp` is a separate tmpfs with 478M avail — staging lives here.
- Backoff was CLEAR (load 2.66, no frozen processes) before the test runs.
- Per the brief: no worktree claimed, no registry entry, no commit.

## What is staged here (/tmp/rev1107-22-r09/)

- `fix.patch` — the complete fix, verified to apply cleanly against HEAD
  `59f0a247` (`git apply --check` in the main repo, read-only) and to
  reproduce the fixed files byte-for-byte from pristine HEAD copies.
- `fixed-files/` — the four fixed files (`main.go`, `vendor.go`,
  `verify.go`, `vendor_test.go`), for copy-apply as an alternative.
- `pristine/` — the four files as `git show HEAD:tools/stdlib-vendor/...`.
- `repo/` — faithful mini-repo layout used for RED/GREEN runs
  (`.git` marker, `tools/stdlib-vendor/` package, live manifest/pin copies
  under `tools/stdlib-fork/`). Fixtures restored to live copies after E2E.
- `RED.log` — new test fails on unfixed code (real mechanism):
  `defaultManifestPath(...) = ".../phases/phase3/stdlib-fork/vendor-manifest.json",
  want ".../tools/stdlib-fork/vendor-manifest.json"`.
- `GREEN.log` — full suite passes on fixed code (9/9, incl. new test);
  `gofmt -l` clean, `go vet` clean.
- `E2E.log` — built binary, default `vendor --goroot <fake> --target <tmp>
  --repo <staged-repo>` wrote the manifest to
  `.../tools/stdlib-fork/vendor-manifest.json`, no `phases/` dir resurrected,
  default `verify --repo ...` reports `VERIFY OK`.
- `vet.log` (empty = clean), `stdlib-vendor` (built binary, -buildvcs=false),
  `fake-goroot/`, `fake-target/`, `gocache/` (GOCACHE was redirected to /tmp
  because the default cache is on quota-blocked home).

## The fix (4 files, `tools/stdlib-vendor/` only)

1. `main.go` — `stdlibForkDir()` now returns
   `filepath.Join(repo, "tools", "stdlib-fork")`; header comment
   (`// phases/...` → `// tools/stdlib-fork/.`) and `usageText` defaults
   repointed at `<repo>/tools/stdlib-fork/vendor-manifest.json` /
   `pinned-version.txt`.
2. `vendor.go` — struct-field comments (lines 57-58) and `--manifest` flag
   help (line 357) repointed; pin comment made explicit
   (`<repo>/.../stdlib-fork/` → `<repo>/tools/stdlib-fork/`).
3. `verify.go` — struct-field comment (line 19) and `--manifest` flag help
   (line 166) repointed.
4. `vendor_test.go` — new `TestDefaultPathsPointAtLiveTree`: asserts
   `defaultManifestPath`/`defaultPinPath` equal the live-tree paths, that
   those files exist, and that `usageText` no longer states the dead path.
   Capable of failing (RED log proves it); runs from the package dir, repo
   root = `../..`.

No other code touched. `resolveRepoPaths` / `verifyTree` needed no change —
they already delegate to `defaultManifestPath`.

## Resume steps (when home quota clears)

1. Re-probe home with a real 8 KB write and verify byte count (touch proves
   nothing). Only proceed on success.
2. Register id `graalgo-rev1107-22-r09`; claim worktree:
   `~/workspace/system/bin/wt.py new graalgo rev1107-22-r09 --chat graalgo-independent-codebase-review`.
   Work ONLY there; never build/commit in the main tree.
3. In the worktree: `git apply /tmp/rev1107-22-r09/fix.patch`
   (or copy `fixed-files/*` over `tools/stdlib-vendor/`).
   NOTE: /tmp is tmpfs — it survives reboots only while the VM lives; if
   this staging dir is gone, the patch is reproducible from this note
   (4 files, substitutions listed above) — do NOT re-derive the mechanism
   from the review alone; re-verify at the then-current HEAD first.
4. Verify in the worktree (all under `nice -n 10`, backoff-gated):
   `cd tools/stdlib-vendor && go test -v .` (expect 9/9 incl.
   `TestDefaultPathsPointAtLiveTree`), `gofmt -l .`, `go vet .`.
   Confirm the new test fails if the fix is reverted (RED check) — the
   staged RED.log already proves this on identical code.
5. Commit with explicit pathspecs (the 4 files only), report completion with
   branch/commits/evidence. Do NOT integrate — independent review first.

## Out of scope / follow-ups (NOT fixed here — flag for coordinator)

- Same dead `phases/phase3/stdlib-fork` path family survives in docs and
  generated-file headers outside this lane's scope: notably
  `tools/stdlib-fork/VENDORING.md` (lines 15-18, 71, 84-85, 93, 100, 108,
  115 — manifest/pin locations and relative-path commands),
  `docs/dev/adr/ADR-0006-stdlib-vendoring.md:39`,
  `docs/dev/adr/ADR-0007-asm-strategy.md:8`,
  `internal/frontend/p3frontend/asm.go` comments + generated
  `zz_asm_inventory_gen.go` header (`generated_by` field), and
  `docs/arch/reviews/phase3-m6/reviewer*.md` (historical review text).
  These are stale references, not live code defaults; fixing them is a
  separate doc sweep, not this lane. Do not conflate with REVIEW-18 TL-01
  (`tools/stdlib-excl/validate.go:30` dead default list path — different file,
  different lane).
- `tools/stdlib-vendor` test suite is still unwired into Makefile/CI (R-10,
  separate lane) — the new test only has signal once R-10 lands.

## Evidence pointers

- Review: `/tmp/REVIEW-2026-11-07-22.md` (R-09 rows at lines 38, 115, 159);
  original finding in
  `~/workspace/goals/graalgo-go-toolchain-on-graalvm-truffle/hidden_files/codebase-review/REVIEW-2026-11-07-07.md`
  (§R-09, line ~134).
- HEAD verified: `59f0a247088f0e0b6db7e0623076866727dd873c` (main repo, clean
  status — no edits made there).

## Note: daily-log append failed on quota too

Appending this run's entry to `~/memory/2026-11-07.md` failed with
`Disk quota exceeded (os error 122)` at ~22:18 CET. This RESUME.md is
therefore the only durable record of the run until home quota clears; the
coordinator should re-log it from here when possible.
