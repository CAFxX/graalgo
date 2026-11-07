# RESUME — lane rev1107-22-r06 (R-06, P1) — BLOCKED on home quota

## Status

**Fix complete and verified in /tmp; NOT committed, NO worktree claimed,
NO registry entry written.** Home quota was EDQUOT-exhausted for the whole
run (real 8 KB home write failed `Disk quota exceeded`; `/var/tmp` 100%
full; `/tmp` 478M free). Per the lane brief, everything is staged here:

- `/tmp/rev1107-22-r06/R-06.patch` — unified diff, applies cleanly to HEAD
  `59f0a247` (verified: fresh-copy `patch -p1` → tree byte-identical to
  `fuzz-fixed/`).
- `/tmp/rev1107-22-r06/fuzz-fixed/` — the fixed `tools/fuzz/` tree
  (copy over the worktree's `tools/fuzz/`).
- `/tmp/rev1107-22-r06/fuzz-pristine/` — HEAD copy, RED baseline.
- `/tmp/rev1107-22-r06/EVIDENCE.md` — full evidence (RED/GREEN logs, control).
- `/tmp/rev1107-22-r06/stub-graalgo-run`, `fuzz-red`, `fuzz-fixed-bin`,
  `red-work/`, `green-work/`, `red-control-work/` — runnable artifacts/logs.
- `/tmp/rev1107-22-r06/gocache`, `gopath` — Go caches (reusable; delete if stale).

## Issue (re-verified from disk truth at HEAD 59f0a247, not taken on trust)

`tools/fuzz/exec.go:18` defaulted `graalgoRun` to
`$HOME/workspace/graalgo/phases/phase1/graalgo-run` — the whole `phases/`
directory is gone. `runGraal` therefore returned `buildOK=false,
buildCode=-1` for every seed, and `compare.go:89-96` maps that to
`vDiverged`. Any fuzz run reported ~100% false divergence (empirically:
seeds 0–1 → `map[DIVERGED:2]`, gc exit=0) and saved false minimized
"regressions". Real divergences were buried; the tool was unusable.

## Fix (3 files, `tools/fuzz/` only; no bridges, no name-keyed cases)

1. `exec.go` — default launcher is now
   `$HOME/workspace/graalgo/internal/backend/graalgo-run` (the live
   launcher, verified present + executable); `GRAALGO_RUN` env overrides it
   (alternate checkouts / worktrees / tests). New `checkBackends()` stats
   `goBin` and `graalgoRun` and errors if either is missing, a directory,
   or non-executable. Header comment updated (was `phases/phase1`).
   Design note: the `$HOME`-anchored default follows the existing `goBin`
   convention and the review's suggested fix literally; a
   `runtime.Caller`-relative resolution was considered and rejected as
   fragile for installed/copied binaries — `GRAALGO_RUN` is the escape hatch.
2. `main.go` — `cmdRun` and `cmdRegress` call `checkBackends()` right after
   flag parsing, before creating any workdir/state/journal; on error they
   print it and exit 1. `cmdGen` untouched (needs no backends).
3. `exec_test.go` (new, 8 tests) — default path has no `phases/` and ends in
   `internal/backend/graalgo-run`; env override; missing / non-executable /
   directory backends error (error names the backend and the `GRAALGO_RUN`
   remedy); executables pass; `compare` still maps buildCode=-1 to
   DIVERGED (pinned with a comment: the startup check — not a compare
   special case — is what prevents false positives) and still maps
   exit-2 + scope error to SCOPE_REJECT.

## Verification done (details + exact logs in EVIDENCE.md)

- RED: pristine HEAD copy, seeds 0–1 → 2/2 DIVERGED, false regressions saved.
- Control: RED binary + working stub launcher → still 2/2 DIVERGED
  (proves the dead default, not the launcher, is the defect).
- GREEN: fixed copy, `go vet` clean, `go test ./...` ok (8 tests),
  `gofmt -l` clean except pre-existing `gen.go` (untouched, out of scope).
- GREEN fail-fast: missing launcher → exit 1 with a clear error on both
  `run` and `regress`, no workdir/state/journal created; `gen` unaffected.
- GREEN end-to-end: stub launcher (working graalgo-run stand-in) →
  seeds 0–4 all SAME, `map[SAME:5]`, no false regressions.

## Resume steps (on quota recovery)

1. Probe home with a real 8 KB write; only proceed on success.
2. Register id `graalgo-rev1107-22-r06`; claim worktree:
   `~/workspace/system/bin/wt.py new graalgo rev1107-22-r06 --chat graalgo-independent-codebase-review`.
3. In the worktree: `git apply /tmp/rev1107-22-r06/R-06.patch`
   (or copy `fuzz-fixed/` over `tools/fuzz/`), `git status` to confirm
   only `tools/fuzz/exec.go`, `tools/fuzz/main.go`, `tools/fuzz/exec_test.go`.
4. Backoff-gate, then re-run in the worktree (GOCACHE may need /tmp while
   quota flaps): `go vet ./...`, `go test ./...` in `tools/fuzz/`.
5. **Still owed:** end-to-end against the REAL `internal/backend/graalgo-run`
   after `make build` in the worktree (out/classes + GraalVM JDK) — confirm
   verdicts are signal, not a uniform class. Then commit with explicit
   pathspecs and report completion; do NOT integrate (independent review first).
6. Suggested commit message: `tools/fuzz: point graalgo leg at live launcher + fail fast if missing (R-06)`.

## Out of scope (not touched)

- `test/differential/cmd/diffharness/snapshot_test.go` also references
  `phase1/graalgo-run` — same dead-path class, belongs to the live R-03 lane.
- `tools/fuzz/gen.go` gofmt drift at HEAD — pre-existing, left alone.
