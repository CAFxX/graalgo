# PROGRESS-r63 — fix-redmaster-fullsuite-a0df481b-r63

Lane r63 (replaces r54/r55/r56/r57/r59/r60/r61/r62 — ONLY fixer on this signature).
Master: a0df481bd8ea809f2ea3a87dd77ecf368ccb4b38 (RED, unchanged since Nov 9).
Branch: wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/fix-redmaster-fullsuite-a0df481b-r63
Base: e2a2b394 (r62 tip on GitHub; r62 branch verified at e2a2b394 via ls-remote).
Chat: f7467b85-7da6-4da0-b9ec-94631a60dafe.

## Base correction (coordinator-verified)
- r62 branch on GitHub was b993d5c1, but /var/tmp/r62-graalgo held one more
  LOCAL commit: e2a2b394 "r62: PROGRESS update — bi01-bi11 PASS, batches
  running under contention" (PROGRESS-r62.md +25, clean tree). r62 believed
  the commit+push was aborted mid-command; the commit LANDED locally, the
  push did NOT. r63 pushed it: first attempt failed HTTP 401 Bad credentials
  on tree upload (transient — GET auth worked throughout); retry rc=0.
  ls-remote confirms r62 branch = e2a2b394621f4b00074df4db1d49cef805c78ebf.
  Base is effectively e2a2b394.

## Harvested state (verified by r63 from disk, 2026-11-17 ~06:12-06:17 UTC)
- r63 registry: home write FAILED EDQUOT (quota closed); /tmp fallback at
  /tmp/fix-redmaster-fullsuite-a0df481b-r63.json, status active, hb 06:13Z.
  Retry home on every heartbeat.
- r62 batches STILL ALIVE at r63 start (PIDs 30664 batch1, 38407 batch2).
  batch2 DONE 06:15:37 UTC:
  - gn09 rc=0 (1 test, 0 failures) — GREEN
  - gv rc=137 (OOM, environmental, NOT a verdict)
  - main-scope rc=0, main-nonascii rc=0, main-clear rc=0, main-utf8 rc=0 — GREEN
  - main-ptrstruct rc=137 (OOM, environmental)
  - sectionclamp rc=0 (3 tests, 0 failures) — GREEN
  - typeparam-excl rc=1 FAIL + typeparam-zero rc=1 FAIL: BOTH are
    "building diffharness failed: cannot find package
    diffharness/internal/harness" — ENVIRONMENTAL (Go module mode broken
    in this VM post-reboot; `go list fmt` fails even in a fresh module).
    Refuted by branch CI, where both typeparam tests PASS on 0533c7cc.
  - typeparam-readexcl rc=0 — GREEN
- batch1 (still running at 06:17 UTC): bi rc=137 (OOM, whole-class JVM);
  gn09 rc=0 — GREEN (double-confirmed); gv started 06:13:57, in progress.
- r62's separate runs: bi01-bi15 PASS (r62-bi.log), bi21 PASS (r62-bi21.log),
  GoByteCellTest 5/5, MapGroupLayoutTest 6/6, make build rc=0.
- Branch CI on 0533c7cc (runs 44023221607 + 44023518704, both completed
  FAILURE): truffle stage 269 tests, 1 failure — ONLY
  BuiltinsCorpusTest.testBi22FloatEdge (exit gc=0 graalgo=1). Runtime and
  interop stages NEVER RAN (make stops at first failing stage). Job log at
  /tmp/branch-ci-0533c7cc.log (86,005 B; tmpfs — summary copied here).
- Master CI job 133298085212 log (re-fetched by r62, /var/tmp/r62-ci-log.zip,
  691 lines): ran to completion, "269 tests, 28 failures" with all 28 named
  (MainTest x5, Gn09, gv01-gv10, bi12/bi17-bi22, Typeparam x2, SectionClamp
  x3). The coordinator's earlier "died in build, no summary" claim is
  REFUTED by this log — the 28 ARE this run's.

## gc oracle for bi22 (GO111MODULE=off go run, rc=0)
- lit: 0.000000 0 eq:true
- neg: -0 -0.000000 eq:true
- tiny: 0.000000
- tinyE: 1.000000e-300
- nilv: [] map[]
- nilt: []int map[string]int
- nanEq:false nanNe:true

## Task
1. Root-cause bi22 FloatEdge (the SOLE branch-CI residual): run the program
   under graalgo vs the gc oracle above, one fresh JVM per program
   (-Xmx768m). Do NOT run a new JVM while batch1's gv JVM is alive
   (MemAvailable ~1.5G; a second JVM OOM-kills both) — wait for a slot.
2. General fix only (no bridges, no test-weakening, no exclusions).
3. Verify bi22 + truffle spot-check locally; create r63 branch from
   e2a2b394 via Git Data API; commit + push EVERY increment.
4. workflow_dispatch branch CI on the new SHA; verify from API + job log
   (fetch pattern /tmp/fetch-ci-log.py: follow the 302 WITHOUT auth).
   Target: truffle 269/0 AND runtime + interop stages green.
5. check_no_bridges.py over the FULL branch delta before reporting.
6. Report new tip for independent review. Do NOT integrate/merge.

## Environment
- Go module mode BROKEN post-reboot (`go list fmt` fails); local builds ONLY
  with GO111MODULE=off. CI unaffected.
- Home quota FLAPS (EDQUOT); wt.py new fails under it — work in
  /var/tmp/r62-graalgo (tmpfs: commit+push every increment).
- One fresh JVM per test program (-Xmx768m); box OOM-kills multi-program
  JVM runs (exit 137). Load was 1.5/7.6/7.5 at 06:12 UTC.

## ROOT CAUSE FOUND + FIXED (2026-11-17 ~06:35 UTC)

### Symptom
bi22 FloatEdge fails in CI (both branch runs on 0533c7cc, identical) and
in any multi-program JVM, with:
  graalgo-run: execution failed: java.lang.IllegalArgumentException:
  reflect: unknown type token 11079
Isolated in a fresh JVM it PASSES. NOT a float-formatting defect.

### Mechanism (proven by stack trace from a TEMP-DEBUG build, since reverted)
Throw site: ReflectTypes$Table.info(11079), byTok.size=11074 — the token is
6 beyond the current table. Call chain (captured in /var/tmp/r63-bi2x-trace.log):
  UnsafeToPtrNode.executeObject:281 (VarDeclNode — `var m map[string]int`)
  -> AbiTypes.buildInterfaceView:870 -> abiPointerForTok(dynamicTok)
  -> fillAbiStruct:431 -> fillExtension:600 (MAP case)
  -> abiPointerForTok(MapGroupLayout.groupTokFor(info.key, info.elem))
  -> info(11079) THROW.
MapGroupLayout.GROUP_TOKS is a process-wide static ConcurrentHashMap keyed
by (keyTok, elemTok) — but tokens are PER-RUN (minted densely per document
in ReflectTypes phase A). A previous program (bi21's table, >=11080
entries) cached groupTok 11079 under the same (keyTok, elemTok) pair that
bi22's map[string]int carries; the cache hit returned the stale token
against bi22's 11074-entry table. AbiTypes' own caches already invalidate
on table change (cachedTable guard); GROUP_TOKS had no such invalidation.

### Fix (MapGroupLayout.java, +24/-1, general, no bridge)
- Added `private static volatile ReflectTypes.Table cachedTable;`
- groupTokFor is now `synchronized` and clears GROUP_TOKS whenever
  ReflectTypes.table() != cachedTable, before consulting the cache —
  the exact invalidation pattern AbiTypes.abiPointerForTok already uses.
  Synchronized so the check-clear-compute sequence is atomic across
  threads. Lock order is safe: groupTokFor is the only acquirer of the
  MapGroupLayout lock and nothing inside it takes the AbiTypes lock.
- TEMP-DEBUG patch to ReflectTypes.info() REVERTED (grep 0), recompiled,
  git tree clean except this fix + PROGRESS files.

### Verification (local, one fresh JVM per run, -Xmx1024m, TMPDIR /dev/shm)
- NEGATIVE CONTROL (pre-fix, same command): testBi2 FAILED bi22 with the
  identical "unknown type token 11079" (log /var/tmp/r63-bi2x.log).
- POSITIVE (post-fix): testBi2 rc=0 — bi20, bi21, bi22 ALL PASS,
  "3 tests, 0 failures" (log /var/tmp/r63-bi2x-fixed.log).
- IN FLIGHT: full BuiltinsCorpusTest class (testBi, one JVM) — the CI-shape
  cross-run pollution test for bi01-bi22 (log /var/tmp/r63-bi-full.log).

### Related hazard (NOT fixed here — follow-up, out of scope for bi22)
DESCRIPTOR_WIRE_IDS / WIRE_ID_DESCRIPTORS in NodeFactory are also
process-wide and keyed by per-document wire ids (last-writer-wins). They
are consumed via the CURRENT table's wireIdToTok, so a stale wire id can
resolve to a WRONG (but in-range) token for a type the current program
never materialized. Benign for bi22 (its build materializes its own types
last). Needs a design decision of its own; flagging, not fixing here.

## Local verification of the fix (2026-11-17 ~06:41 UTC, all one fresh JVM per run)
- testBi2 (bi20+bi21+bi22): 3 tests, 0 failures (log /var/tmp/r63-bi2x-fixed.log)
- FULL BuiltinsCorpusTest testBi (bi01-bi22, one JVM — the CI-shape
  cross-run pollution test): 22 tests, 0 failures, ~306s
  (log /var/tmp/r63-bi-full.log)
- GoByteCellTest 5/5, MapGroupLayoutTest 6/6 (logs /var/tmp/r63-gobytecell.log,
  /var/tmp/r63-mapgrouplayout.log)
- GenericsCorpusTest testGn09FmtOverlay: PASS (log /var/tmp/r63-gn09-fixed.log;
  an earlier rc=1 was my wrong class name graalgo.runtime.* — the class is
  graalgo.frontend.GenericsCorpusTest, not a regression)
- Branch: wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/fix-redmaster-fullsuite-a0df481b-r63
  created locally from e2a2b394. Fix commit + this PROGRESS to be pushed next.

## Re-verification of tip 266bb9d0 from disk (2026-11-17 ~07:30–07:40 UTC, post-reboot-210)

Reboot #210 (boot 3978a52b, PID 1 start 07:20:21Z) wiped /var/tmp + /tmp,
killing this lane's pre-reboot full-class run and all its logs. The fix
survived ONLY as the pushed commits 64525712 + 266bb9d0. Worktree
re-established at tip 266bb9d0 (clean checkout, verified).
- `make build` rc=0 on the tip; test-classes built for truffle/runtime/
  interop (all rc=0).
- Full BuiltinsCorpusTest in ONE JVM (the CI-shape cross-run pollution
  test): **22 tests, 0 failures** (bi01–bi22 incl. bi22), rc=0, ~441s
  under load. Log /var/tmp/r63-bi-full.log. The tip's 22/0 claim is now
  RE-VERIFIED by this lane, not taken on trust.
- GoByteCellTest 5/5 PASS; MapGroupLayoutTest 6/6 PASS
  (logs /var/tmp/r63-gobytecell.log, /var/tmp/r63-mapgrouplayout.log).
- check_no_bridges.py over the FULL branch delta (origin/master
  a0df481b...HEAD 266bb9d0): PASSED, rc=0.
- GenericsCorpusTest testGn09FmtOverlay: two attempts FAILED with an
  ENVIRONMENTAL cause only — the gc oracle's stderr carries
  "error acquiring upload token: ... /home/hatch/.config/go/telemetry/
  local/upload.token: disk quota exceeded" (home quota closed) and the
  harness rejects the polluted oracle output. GOTELEMETRY=off does NOT
  suppress it in go1.27.1. Workaround PROVEN directly: XDG_CONFIG_HOME=
  /dev/shm/r63-config makes `go run` print a clean "hello 42" with empty
  stderr. Re-run in the harness with that set: **PASS** (log
  /var/tmp/r63-gn09-fixed.log, rc=0, 1 test 0 failures — took ~365s under
  load 17+). gn09 was also GREEN in the r62 batches and on branch CI.
  The fix cannot regress it (single-program behavior identical: the
  table never changes within one run, so the added clear never fires).
- Home quota still CLOSED (registry write failed EDQUOT 07:27Z; /tmp
  fallback current). The gn09 telemetry failure is the same quota.
- Provenance anomaly (for coordinator adjudication): commits 64525712 +
  266bb9d0 were pushed 06:45:26Z under the "r63 lane" identity with
  content byte-identical to this lane's uncommitted fix + analysis, but
  this lane never ran git commit/push, no second r63 spawn exists in
  muse.db, and none is visible in subagent.list. No live duplicate found;
  this lane proceeds as the live r63.
