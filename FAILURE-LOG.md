# FAILURE-LOG.md — red-master fix lane fix-redmaster-snapshot-56a3dd6-r2

Adopted worktree: ~/workspace/worktrees/graalgo/fix-redmaster-snapshot-56a3dd6-r2
Branch: wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/fix-redmaster-snapshot-56a3dd6-r2
Base: 56a3dd68 (master tip). Local commit on top (after evidence-based drop):
  - 9105181e "Fix N-target multi-result interface method assignment desugar"
  (98bb0ce9 "Fix method table itab for disk-MR stubs; lazy-init errors.errorType"
   was dropped — see ROOT-CAUSE.md; it did not address the observed failure.)
Verified at adoption: no live owner processes (ps self-match only);
`git status --porcelain` clean; no TEMP-DEBUG debris.
NOTE 2026-10-17 ~08:00 CEST: another agent entered this worktree (reflog:
reset to 9105181e, created + pushed ci-probe branch 90c49304) and deleted
these evidence files; rewritten from lane context. Worktree exclusivity
violation noted for coordinator.

## STEP 1 — OBSERVE (2026-10-17 ~07:10–07:25 CEST)

Source: actual CI job logs for master 56a3dd68, pulled via GitHub API + job-log
302→Azure-blob redirect (fetched WITHOUT the surrogate auth header, per AGENTS.md lesson).

CI status on 56a3dd68 (last poll 2026-10-17T03:42:25Z): 5 `ci` runs, ALL failing
job = `build-and-check`, `full-suite` skipped in every run:
  run 38993255386 → job 117280019090 (122752 bytes)
  run 38993251197 → job 117280006775 (122808 bytes)
  run 38993015244 → job 117279295334 (120737 bytes)
  run 38992799551 → job 117278642665 (120672 bytes)
  run 38992323081 → job 117277194627 (120522 bytes)
Logs: ~/workspace/tmp-snapshot-r2/ci-logs/job-*.log
The 5 DRIFT summaries are byte-identical (md5 e096084739653ab478a21900134c1db5).

### Distinct failure #1 (the ONLY observed CI failure on master):
`make all` fails at **Makefile:263 `snapshot-check`**:
  `go run ./cmd/diffharness snapshot check -config backends-p3.json -corpus corpus
   -backend graalgo -snapshot ../../test/snapshots/go1.27.1.json -jobs 4 -timeout 60`
  → `diffharness snapshot: error: 38 programs drifted from the snapshot`
  → `make: *** [Makefile:263: snapshot-check] Error 1`
  → `##[error]Process completed with exit code 2.`

Every one of the 38 corpus programs drifts with the same signature:
  `DRIFT <name>  buildOk: want=true got=false`
including trivial programs (01_hello, 02_arithmetic, ...). NO other drift fields
differ — every program fails to BUILD under the graalgo backend (backend build
command: `internal/backend/graalgo-sync-run --check {SRC}`).

Notable: the toolchain itself compiles on CI (javac steps all succeed, out/classes
touched) — the failure is at runtime: graalgo-sync-run cannot build ANY program.
Root error (reproduced locally): `assign.fields: lhs/rhs length mismatch`
from NodeFactory.java:4525.

### Local full-snapshot-check note
A local `make snapshot-check` with the fixed frontend ALSO showed 38 drifted,
but forensics proved this was a LOAD artifact, not the bug: the box was at
load ~17 with 1GB free; the diffharness BuildOnly 120s context SIGKILLed the
JVM workers (`failed: signal: killed`, probe confirmed 124s > 120s timeout).
Direct single-program runs with the fixed frontend succeed (exit=0) in all
tested env configurations. CI runners fail FAST (~15s/program), consistent
with the real bug, not timeouts.
