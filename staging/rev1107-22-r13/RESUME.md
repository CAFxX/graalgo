# RESUME — rev1107-22-r13 (R-13: e2e module probes dead + unwired)

Status: **BLOCKED-quota**. Fix staged, NOT committed. Resume when home quota
recovers (real 8 KB home write succeeds) AND /tmp has headroom (it hit 100%
during this run from sibling lanes' staging — do not delete their dirs).

## 0. Preconditions (in order, all under `nice -n 10`)

1. Probe home with a real 8 KB write, verify byte count, remove it. If it
   fails EDQUOT, stop here — do not claim a worktree or commit.
2. `~/workspace/system/bin/load-shed.sh backoff` → CLEAR before any build/test.
3. Register (suggested id: `graalgo-rev1107-22-r13`), then claim a worktree:
   `~/workspace/system/bin/wt.py new graalgo rev1107-22-r13 --chat graalgo-independent-codebase-review`.
   Work ONLY there. Never build or commit in the main repo tree.

## 1. Apply the staged fix (from /tmp/rev1107-22-r13/)

In the worktree (base must be HEAD `59f0a247` or a descendant that still
contains the old `run_module.sh` — if the base moved, re-verify the old
script's `phases/` paths first; if they changed, re-derive the patch):

```bash
cd <worktree>
git apply /tmp/rev1107-22-r13/r13-combined.patch
git status --short   # expect exactly: M Makefile, M test/e2e/README.md, M test/e2e/run_module.sh
git diff --stat
```

Or copy the three files from `/tmp/rev1107-22-r13/new/` over the worktree
files (same content; the patch route is preferred — it fails loudly on
context drift). `EVIDENCE.md` in the staging dir records the disk-truth
re-verification; do NOT copy staging files into the worktree except the
three fixed files.

## 2. RED→GREEN verification (owed — the full run never happened)

RED (before the fix, for the record — already demonstrated, do not re-run
the old script in the worktree): old `run_module.sh` builds a garbage
classpath (`ls` of the nonexistent `phases/phase1/lib/*.jar` fails) and
fails all 8 uuid cases; nothing invokes it.

GREEN (in the worktree, after applying the patch):

1. `make generate` (gofrontend binary) then `make build` (backend classes).
   Both are prerequisites of the new `test-e2e` target; run them explicitly
   once so a failure is attributable.
2. `make test-e2e` — runs `test/e2e/run_module.sh test/e2e/uuid` (all 8
   cases, `--gmp=1` + `--gmp=4` legs each). Record the full per-case table.
   - If all 8 PASS: the gate is live and green. Proceed to §3.
   - If some FAIL **on merit** (real backend gaps, e.g. a new BUGS.md
     entry): do NOT weaken the gate (no `|| true`, no exclusions, no
     case-skipping). File each failure in `test/e2e/BUGS.md` with a minimal
     reproducer, keep the `make test` wiring in the patch, and report the
     per-case evidence to the coordinator — landing the wiring with a red
     `make test` is a coordinator decision, not the lane's. The staged
     patch is still the correct fix; the gate is live, the backend is not
     yet good enough.
   - If a case fails on **harness** grounds (missing go.sum, module-cache
     miss, GOCACHE/TMPDIR trouble): fix the harness, not the gate. See §4.
3. `make test` (full suite) — confirm the new `test-e2e` step runs last
   and the suite result matches the `test-e2e` result (no silent swallow:
   `make test` must exit non-zero iff `test-e2e` fails).
4. `bash -n test/e2e/run_module.sh` (already passed on the staged copy;
   re-run after apply) and `git diff --check`.

## 3. Commit + report

- `go.sum`: the gc leg (`GOFLAGS=-mod=mod`) writes `test/e2e/uuid/go.sum`
  on first run. Verify it matches the staged `go.sum.generated`
  (uuid v1.6.0, two lines) — regenerate via the real run, do not blindly
  copy the staged file — and commit it as part of this change
  (`git add` with explicit pathspecs only).
- Commit with explicit pathspecs: `Makefile`, `test/e2e/run_module.sh`,
  `test/e2e/README.md`, `test/e2e/uuid/go.sum`, plus `test/e2e/BUGS.md`
  ONLY if step 2 produced genuine failure entries.
- Report completion with branch, commits, and the verification evidence
  (per-case table + `make test` result). Do NOT integrate/merge — an
  independent reviewer checks before any merge.

## 4. Known caveats (do not expand scope to fix these)

- Disk importer picks the HIGHEST uuid version in the module cache
  (`diskimport.go`), while `go.mod` pins v1.6.0 for the gc leg. Today the
  cache holds only v1.6.0, so both legs agree. If a newer uuid ever lands
  in the cache, the legs can diverge — that is a WS G design question for
  the coordinator, not this lane.
- `/dev/null` is a regular empty file (REVIEW-20 anomaly, still true at
  this run). Anything redirecting to /dev/null accumulates instead of
  discarding; the staged script does not redirect there.
- The old script's `JAVA` env var is gone (the launcher owns the JVM via
  `GRAALVM_HOME`); callers setting `JAVA` will get a silent no-op. That
  var was only ever used for the hand-built classpath, so this is safe.
- `test/e2e/uuid/.tmp` (the script's default TMPDIR) is gitignored via
  the root `.tmp/` pattern (`.gitignore:23`).

## 5. Staging inventory

- `/tmp/rev1107-22-r13/r13-combined.patch` — apply this.
- `/tmp/rev1107-22-r13/new/{Makefile,test/e2e/run_module.sh,test/e2e/README.md}` — patched files.
- `/tmp/rev1107-22-r13/orig/` — pristine originals at HEAD 59f0a247.
- `/tmp/rev1107-22-r13/EVIDENCE.md` — full re-verification evidence.
- `/tmp/rev1107-22-r13/go.sum.generated` — reference go.sum (verify, don't copy blindly).
- `/tmp/rev1107-22-r13/apply-test/` — skeleton apply/parse verification; discard.
