# PROGRESS-r62 — fix-redmaster-fullsuite-a0df481b-r62

Lane r62 (replaces r54/r55/r56/r57/r59/r60/r61 — ONLY fixer on this signature).
Master: a0df481bd8ea809f2ea3a87dd77ecf368ccb4b38 (RED, unchanged since Nov 9).
Authoritative CI: scheduled `ci` run 43867020672, job 133298085212 FAILED,
269 tests, 28 failures, `make test` exit 2.
Branch: wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/fix-redmaster-fullsuite-a0df481b-r62
Base: 0533c7cc (r60 tip = r61 tip; verified on GitHub via ls-remote).
Chat: f7467b85-7da6-4da0-b9ec-94631a60dafe.

## Environment (2026-11-17 ~05:35 UTC)
- Reboot ~05:16–05:18 UTC wiped /var/tmp + /tmp. Home quota FLAPPING:
  hash-object probes OK at 05:35, but wt.py registry write failed EDQUOT
  (Errno 122) at 05:36 — `wt.py new` FAILED twice, no home worktree.
- FALLBACK IN USE: fresh clone at /var/tmp/r62-graalgo (385M, 1.7G free on
  /var/tmp tmpfs). Build outputs stay in /var/tmp; TMPDIR=/var/tmp.
  NEVER write build outputs to home except inside a worktree.
  NEVER delete any other lane's worktree.
- Load ~9-10 (1-min) at start; run everything under `nice -n 10`.
- r61 registry status=done (hb 2026-11-17T05:27:02Z). r55–r60 registry
  files MISSING; no live predecessor fixer. r62 registry pre-created,
  status=active, hb 2026-11-17T05:32:00Z.

## Done
- [x] 05:36 UTC: r62 branch created on GitHub at 0533c7cc (API POST
  git/refs, 201) and verified via `git ls-remote github` (SHA-anchored).
- [x] 05:36 UTC: /var/tmp/r62-graalgo cloned from ~/workspace/graalgo,
  branch wt/.../r62 checked out at 0533c7cc.
- [x] This file (PROGRESS-r62.md) committed + pushed (see below).

## Next
1. Understand the F1 remediation state at 0533c7cc: read RECONCILIATION-r59.md
   (in 942650ef), r59/r60/r61 commit diffs, MapHeaderView/ReflectRefs/
   UnsafeToPtrNode code; find what is unfinished.
2. Verify the two new tests from 0533c7cc (GoByteCellTest, MapGroupLayoutTest)
   compile and pass.
3. Run the failing-test subset (the 28 tests) locally; find ROOT CAUSE of
   each remaining failure; implement general fixes (no bodges/bridges).
4. Re-run check_no_bridges.py after any .go commit.
5. Push every increment immediately via push_repo.py (PAT Git Data API).
   Push state: /var/tmp/r62-push-state.json (copy of r61 state).
   Command: nice -n 10 python3 ~/workspace/custom-connectors/custom.legacy-github-pat/bin/push_repo.py --repo graalgo --local /var/tmp/r62-graalgo --branches wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/fix-redmaster-fullsuite-a0df481b-r62 --state /var/tmp/r62-push-state.json
6. Trigger/wait for branch CI (`ci` workflow on r62 branch); report commit
   hash, CI run URL + result, remaining failures of the 28.
7. Report for independent review; NEVER merge (master is red).

## Resume commands (if stopped/rebooted)
- cd /var/tmp/r62-graalgo (if wiped: re-clone from ~/workspace/graalgo,
  checkout -b wt/.../r62 0533c7cc — or fetch r62 branch state from GitHub
  via push_repo.py state; re-create branch from last pushed SHA in this file)
- git log --oneline -5; git status
- Heartbeat: update ~/workspace/system/work-registry/fix-redmaster-fullsuite-a0df481b-r62.json
  AND /tmp/fix-redmaster-fullsuite-a0df481b-r62.json (heartbeat field, ISO UTC)
- Re-check quota: unique-content probe `echo -n "probe-r62-<ts>-unique" | git -C ~/workspace/graalgo hash-object -w --stdin`
- Re-check master SHA + CI before acting on stale state.

## The 28 failures (master a0df481b, full-suite job 133298085212)
- MainTest x5: testCheckRejectsScopeError, testRunNonAsciiSource,
  testCheckRejectsClearBuiltin, testFmtInvalidUtf8Bytes, testFmtPointerStructValue
- GenericsCorpusTest.testGn09FmtOverlay (ifacemethod)
- VarsCorpusTest gv01–gv10
- BuiltinsCorpusTest bi12/bi17/bi18/bi19/bi20/bi21/bi22
- TypeparamDifferentialTest x2
- SectionClampTest x3

## Push log
- 2026-11-17 ~05:36 UTC: branch created on GitHub at 0533c7cc (no objects
  uploaded — SHA already present as r60 tip).

## Update 2026-11-17 ~05:44 UTC — Go toolchain broken in module mode (ENV, not repo)
- `make build` FAILED at the gofrontend step: `package fmt/unsafe/... is not in std`
  (modload ImportMissingError). Root cause: Go module mode is broken in this
  VM post-reboot — `go list fmt` fails even in a fresh module AND with
  GOTOOLCHAIN=local, on BOTH ~/tools/go and ~/tools/go2 (both go1.27.1).
  `GO111MODULE=off go list unsafe` / `go run` WORK (hello-world prints).
  Module cache ~/go/pkg/mod is near-empty (sumdb only); proxy reachable via
  curl. Cause of the module-mode breakage NOT yet established.
- LOCAL WORKAROUND (verification only, no repo change): build with
  `GO111MODULE=off` in env — gofrontend builds clean under it
  (13.5MB test binary /var/tmp/gotest-gofrontend, rc=0). `make build`
  re-run 05:43 UTC with GO111MODULE=off, log /var/tmp/r62-build2.log.
  CI is unaffected (own Go toolchain). If a successor resumes: re-check
  `go list fmt` in module mode first; if still broken, keep GO111MODULE=off
  for local builds only.
- Load 1-min hit 14.40 at 05:43 — near shedder freeze (>=16). Back off
  before heavy parallel work; a T-state process is FROZEN, not hung.

## Push log (continued)
- 2026-11-17 ~05:37 UTC: ca37837a (PROGRESS-r62.md) pushed via push_repo.py
  (state /var/tmp/r62-push-state.json); ls-remote confirms r62 = ca37837a.

## Update 2026-11-17 ~05:53 UTC — build green, unit tests green, bi21 GREEN
- `make build` rc=0 (GO111MODULE=off env; see toolchain note above).
- Interop full IfaceTestRunner OOM-killed (Error 137) after 41 PASSes —
  box memory pressure (another lane's 1GB JVM + go compiles running).
  Ran the r61 unit tests standalone with -Xmx512m instead:
  GoByteCellTest 5/5 PASS, MapGroupLayoutTest 6/6 PASS (r60's claim verified).
- **bi21 (BuiltinsCorpusTest.testBi21NestedMaps) PASSES** on this tip
  (SingleTestLauncher, -Xmx768m, 1 test, 0 failures, ~96s) — the F1
  remediation's core signal is green locally. Log: /var/tmp/r62-bi21.log.
- NEXT: remaining 27 of the 28 (MainTest x5, gn09, gv01-10, bi12/17/18/19/
  20/22, TypeparamDifferential x2, SectionClamp x3). Run per-class via
  SingleTestLauncher with -Xmx768m; each differential test takes ~60-90s.
  If the session is killed, resume with the class batches below.

## Update 2026-11-17 ~05:56 UTC — CI log re-fetched; batch running
- Re-fetched authoritative full-suite job 133298085212 log via Actions API
  (logs endpoint 302 -> blob URL fetched WITHOUT auth header; first attempt
  401 because the surrogate was sent to Azure). Saved raw text:
  /var/tmp/r62-ci-log.zip (91,654 B, plain text despite the name).
  Confirms the 28 with exact messages (gv/bi all "exit code gc=0 graalgo=1";
  gn09 "unsupported expression kind ifacemethod"; SectionClamp
  "basiclit.pos: missing required key file"; Typeparam x2 =
  testZeroUnexplainedDivergences NPE + testReadExclusionsEdgeCases
  NoSuchFileException tools/diffharness/testdata/exclusions-edge.yaml).
- ClearBuiltin mystery SOLVED: master a0df481b MainTest.java:301 HAS
  testCheckRejectsClearBuiltin; the r54 stack renamed/refreshed it to
  testClearMinMaxRun at this tip. Batch runs testClearMinMaxRun as its
  successor. NOTE: batch does NOT run testReadExclusionsEdgeCases (the
  second Typeparam failure) — successor must run it separately.
- Batch /var/tmp/r62-batch.sh running since 05:53 (setsid nohup, sequential,
  -Xmx768m, per-test logs /var/tmp/r62-<label>.log, summary
  /var/tmp/r62-batch-summary.log). bi01-bi05 PASS so far (~30s/test).
  Order: bi, gn09, gv, main x5, sectionclamp, typeparam x2.
- Home quota FAILED again on registry write 05:53 (EDQUOT) — /tmp copy is
  the fresh heartbeat (05:53:20Z); home registry stale at 05:37:30Z.
  GitHub branch tip is the durable record (cf3908a8).

## Push log (continued)
- 2026-11-17 ~05:54 UTC: cf3908a8 (bi21 GREEN + unit-test verification)
  pushed via push_repo.py; ls-remote confirms r62 = cf3908a8.

## Update 2026-11-17 ~06:04 UTC — batch partial results (in progress)
- bi01-bi11 ALL PASS (batch1, /var/tmp/r62-bi.log). bi12+ pending.
- Batch2 (parallel, own tmpdir /var/tmp/r62-test-tmp2) running gn09 since
  05:59 — slow under contention: the box has several lanes' JVMs + go
  compiles running concurrently (review-1115-14, review-1116-01,
  batch-net, another lane's JVM PID 7044 at ~47% CPU since 05:26).
  My cell has 1.5 shared CPUs. Both r62 JVMs are alive (S state, NOT
  frozen — shedder frozen:{}; load 1.64), in gc-oracle `go build` phases.
  NOT stuck; just slow. Do NOT kill them.
- Two batches run in parallel (batch1 /var/tmp/r62-batch.sh,
  batch2 /var/tmp/r62-batch2.sh, both setsid nohup). Their test lists
  OVERLAP (gn09/gv/main/sectionclamp/typeparam run twice) — harmless:
  separate tmpdirs, separate logs (r62-*.log vs r62b-*.log). If resuming,
  read BOTH summaries (/var/tmp/r62-batch-summary.log,
  /var/tmp/r62-batch2-summary.log) and deduplicate.
- Reminder: batch does NOT cover TypeparamDifferentialTest.
  testReadExclusionsEdgeCases in batch1 — batch2 DOES (r62b-typeparam-
  readexcl.log). Successor: check that log first.
- Home quota still flapping (registry write EDQUOT 05:53); /tmp heartbeat
  fresh. GitHub tip is the durable record.

## Push log (continued)
- 2026-11-17 ~05:57 UTC: b993d5c1 (CI-log + ClearBuiltin findings, batch
  running) pushed via push_repo.py; ls-remote confirms r62 = b993d5c1.
