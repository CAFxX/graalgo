# R3 — Code craft, directive compliance, docs consistency (M5 + loop-capture)

Reviewer: R3 (craft/consistency lens). Read-only review of the fixed snapshot at
[reviews/phase1-m5-loopcap/snapshot/](snapshot/) (207 files).
Report written to [reviews/phase1-m5-loopcap/R3-craft.md](R3-craft.md) (only write).

## 1. Files audited + checksums verified

- `sha256sum -c ../SHA256SUMS.txt` run from inside [snapshot/](snapshot/): **207/207 OK**, zero failures.
- Audited in full: `java/frontend/*.java` (86 files incl. `NodeFactory.java`, `Json.java`,
  `Boxing.java`, all `Box*`/`Cap*`/`Panic*`/`Defer*` nodes), `export.go` (2916 lines),
  `evidence/frontend-v1.md` (252 lines), `evidence/freeze-stub.md` (224 lines),
  `evidence/CHECKPOINT.md` (§§ M5 + loopcap, lines 569–801),
  `evidence/loopcap/{COMPLETE,CHECKPOINT_LOOPCAP,DESIGN_WIRING,PROGRESS}.md`,
  `evidence/m5_green.log`, `evidence/m5_mutant_red.log`, `evidence/ws-g/*.log`,
  `evidence/loopcap/pristine/*` (md5 of pristine `NodeFactory.java` =
  `3c4b7949129926ebbf67f44e4e53e36f`, matching the CHECKPOINT claim),
  `corpus/{controlflow,panic}/README.md` + corpus file listings, `java/frontend/README.md`.
- Deliberately NOT read (out of scope / unavailable): the live tree
  [phases/phase1/](../../phases/phase1/); `graalgo.values`/`graalgo.runtime`/`graalgo.safepoint`
  sources (absent from the snapshot except as call targets); test sources
  (`FreezeSignatureTest`, corpus tests); `Makefile`/build files (absent from the snapshot).

## 2. Findings

### Deprecated-API directive — COMPLIANT in the reviewed scope

No violations found. `grep` over all snapshot Java sources found:
- zero `@Deprecated` annotations, zero `@SuppressWarnings`,
- zero references to `sun.misc` / `jdk.internal`,
- zero calls to commonly-deprecated JDK APIs (`new Integer(...)`-style boxing ctors,
  `Thread.stop/suspend/resume`, `finalize()`, `SecurityManager`, etc.).
- The only deprecation warnings in evidence are from **Truffle's own**
  `HotSpotTruffleRuntime` calling `sun.misc.Unsafe::objectFieldOffset`
  (`evidence/m5_green.log`: "WARNING: A terminally deprecated method in
  sun.misc.Unsafe has been called ... by com.oracle.truffle.runtime.hotspot.HotSpotTruffleRuntime").
  That is the pinned GraalVM 25.0.4.1.1 runtime's internal use, not the project's code;
  not a directive violation (nothing the project can or should change).
- Enforcement evidence: the M5 build log (`evidence/m5_green.log`, line 4 and the
  test-compile line) shows the actual javac invocation carrying
  `-Xlint:deprecation -Werror`. The gate was live at build time.
- **Caveat (could not verify, see §3):** no `Makefile` exists in the snapshot, so I
  could not confirm the flag is still present in the current build config (the M6
  worker owns the live tree). The loopcap-added sources (`GoBox.java`, `Box*Node.java`)
  are not all present in the snapshot's build-file list either (see §3.2).

### Two-writer debris audit — CLEAN

- `export.go` contains **zero** `"boxed"` wire-protocol markers (the 8 dead-design markers
  were reverted; `grep -n '"boxed"' export.go` → no hits).
- No `headerMode`/`postMode` identifiers anywhere; `NodeFactory.java:200` explicitly
  documents why the loop-phase stack replaced the boolean design.
- No `prescanBoxed` identifier; the surviving design is `prescanForBoxes` /
  `prescanRangeBoxes` (`NodeFactory.java:1140,1171`), called from `buildFor` (2429) and
  the range builder; `boxKinds` is live and read at three sites (170, 2437, 2481).
- No `pristine` references in code; no commented-out code blocks; no duplicate method
  definitions in `NodeFactory.java`.
- The phase-stack push/pop in `buildFor` (2441–2472) uses try/finally correctly, and
  `readBoxedVar`/`boxWriteSlot`/`snapshotBoxedVar` (995–1055) implement exactly the
  DESIGN_WIRING.md claims (post reads AND writes S_outer; body uses S_iter; header
  closures snapshot S_outer, body closures S_iter). Comments name the gc-oracle
  verification at each decision point. Naming is clear: `Box*` = per-iteration cells,
  `Cap*` = funclit ancestor-frame capture.

### P1 — freeze-stub.md "Builtin nodes" table documents a dead class

- `evidence/freeze-stub.md:193` (section "Builtin nodes — `graalgo.frontend` (REAL;
  Truffle nodes)"): row `` `PanicNode(GoExprNode)` / `PanicNode()` `` for `panic`/`recover`.
- Code: `java/frontend/PanicNode.java` exists but has **zero** `new PanicNode(...)` call
  sites anywhere in the snapshot (only self-reference + a README mention). The M5
  `buildCall` dispatch (`NodeFactory.java:1418,1424`) constructs
  `PanicBuiltinNode(GoExprNode, GoTypeDescriptor, String, boolean)` (31) and
  `RecoverBuiltinNode()` (21) instead.
- The freeze doc is normative ("Signatures other workstreams depend on. Do not change
  without a cross-workstream review"), and its Panic section correctly freezes the
  runtime interface (`Panic.throwPanic(Object)`, `Panic.recover()`, and the 4-arg
  overload — all consistent with the 19 + 2 call sites). But the builtin-node table
  still describes the pre-M5 WS-G wiring. A reader implementing from the doc builds
  against the wrong class and a wrong constructor shape.
- Companion: the superseded `PanicNode` carries no marker (no `@Deprecated`, no
  "superseded by PanicBuiltinNode/RecoverBuiltinNode" comment) — see P2-7.

### P1 — frontend-v1.md (normative D1 wire doc) omits the M5 wire kinds

- `evidence/frontend-v1.md` §6 "Nodes" (lines 91–145): the node-kinds table has no
  `defer` and no `funclit` rows, yet `export.go:1653` emits `e.node("defer", …)` and
  `export.go:2677` emits `e.node("funclit", …)`, and `NodeFactory.java:250-251`
  registers `registerStmtKind("defer", …)` / `registerExprKind("funclit", …)`.
- §7 "Exporter scope (v1, M4 control-flow + composite subset)" (146–180) is M4-era:
  the statement list excludes `defer`; it claims "calls must be builtins;
  `NodeFactory` v1 rejects every builtin call with a precise error" — but M5/WS-G
  `buildCall` (1333–1428) dispatches builtins (`len`, `cap`, `panic`, `recover`,
  `print`/`println`, …).
- The doc header declares it "normative for Phase 1 (PLAN.md D1)". The only planned
  fix is the generics brief's "docs/frontend-v1.md §6/§7 updates"
  (`evidence/CHECKPOINT.md:801`), whose scope is the generics additions — the M5
  gaps (defer/funclit/builtins) have no explicit owner. Either the M6 doc pass must
  be scoped to include them, or a separate owner named.

### P1 — freeze doc is incomplete for the M5 runtime surface it depends on

- The M5 AST calls these `graalgo.runtime` members, none of which appear in
  `freeze-stub.md` (which claims to freeze "signatures other workstreams depend on"):
  `GoErrors.nilDereference/indexOutOfRange/negativeShift/integerDivideByZero/interfaceConversion/bounds/assignmentToNilMap`
  (13 call sites: `BinaryOpNode.java:191-213`, `IndexNode.java:58`,
  `TypeAssertNode.java:68`, `DeferNode.java:50`, `FuncValueCallNode.java:33`, …),
  `GoroutineState.current/install/restore` (`DeferNode.java:54`,
  `GoRootNode.java:46,71`, `GoFuncRootNode.java:106`),
  `Goexit.goexit()` (`GoexitCallNode.java:23`), `GoCalls.invoke`
  (`FuncValueCallNode.java`), `DeferredCall.func/builtin` (`DeferNode.java:44,52`),
  `DeferChain` methods (`GoRootNode.java:47`, `GoFuncRootNode.java:107`).
- The doc *does* freeze the D8 seam (`Safepoint.poll()`, call sites match) and the
  WS-F `Panic.throwPanic/recover` trio ("Frozen cross-workstream interface (verbatim;
  WS F implements)"), so the pattern exists — but `Goexit`/`DeferChain`/`GoCalls`/
  `GoErrors`, which Phase 2's scheduler work will depend on exactly as much as
  `Panic`, are absent without a stated scoping rationale. Either list them or write
  the scope rule (e.g. "only Panic is cross-workstream; the rest is WS-F-internal")
  — today the doc is silent, so a Phase-2 worker cannot tell what is stable.

### P1 — freeze doc's own arithmetic does not add up; test coverage unexplained

- `evidence/freeze-stub.md:202` ("Version history"): "initial freeze. **58 functions**
  + 11 builtin nodes."
- The doc's tables actually contain **65** `` | `public `` rows, of which 2 are
  `public record Arg` declarations → **63 function signatures** (counted per
  section: Atomics 12, Fmt 8 incl. record, ChanOps 7, Slice 6, Map 6, Netpoll 5,
  Timers 4, Scheduler 4, Random 3, Panic 3, BuiltinPrint 3 incl. record, Memory 2,
  Traceback 1, itabFor 1).
- Meanwhile `evidence/m5_mutant_red.log` prints "(freeze v1: 37 signatures verified)"
  and the review brief cites "37/37 signatures". So three numbers circulate for one
  freeze: **58** (doc claim), **63** (doc tables), **37** (test). The doc never says
  which subset the test verifies. For a freeze document whose entire job is precision,
  this is a defect: fix the count, and mark which rows `FreezeSignatureTest` covers.
  (The test source itself is not in the snapshot — see §3.4.)

### P1 — corpus/panic/README.md still documents the fixed bug as a live limitation

- `corpus/panic/README.md:93` "## Known limitations (M5)": "**Loop-variable capture**:
  Variables declared in a loop body and captured by a funclit share the frame slot
  across iterations (no per-iteration storage). Workaround: pass as explicit param
  (pn13 uses `func(j int) { defer ... }(i)`). Full per-iteration capture is WS D."
- The loopcap remediation fixed exactly this (`evidence/loopcap/COMPLETE.md`;
  `corpus/panic/pn13_defer_storm.go` in the snapshot is restored to direct `i`
  capture with an "M6: per-iteration capture" comment). The README now asserts a
  false limitation and documents a workaround that no longer exists in the file it
  cites. Update or delete the bullet.

### P1 — evidence/m5_green.log is misnamed: it is the interrupted run, not the green run

- `evidence/m5_green.log` contains 70 PASS / 1 FAIL and ends with
  `make: *** [Makefile:66: test] Terminated` plus
  `FAIL PanicCorpusTest.testPn06PanicNil: ... stock go run failed (exit 143)` —
  i.e. the M5 worker's session-timeout-interrupted run described in
  `evidence/CHECKPOINT.md:714-718`, not a green run.
- The actual M5 acceptance evidence (coordinator's 102/102 re-run,
  `/tmp/m5_verify.log`) is referenced by the CHECKPOINT but **not present in the
  snapshot**. A snapshot-only reader finds a file named "green" that shows a
  terminated, failing run. Rename/replace with the coordinator's log (or add it).

### P2 — m5_mutant_red.log shows 3 failures; the checkpoint accounts for 2

- `evidence/m5_mutant_red.log`: "102 tests, 3 failures" —
  `testPn07RecoverStops` (gc=7123 vs 712), `testPn09NestedPanic` (gc=321 vs 32),
  `testPn14OobIndex` (stderr trailer `// panic: trailer <…> does not match gc's
  first stderr line <panic: …>`).
- `evidence/CHECKPOINT.md:702-704` claims "reverting the DeferChain fix → pn07/pn09
  fail exactly as predicted". The pn14 trailer-format mismatch is unaddressed:
  unexplained whether it is a second-order effect of the mutant or an unrelated
  harness artifact. "Exactly as predicted" is not evidenced while a third,
  unpredicted failure sits in the log.

### P2 — dangling GG-0045 cross-references

- `java/frontend/TypeAssertNode.java:64-65` claims the int/int64 + uint/uint64/uintptr
  conflation limitation is "(documented in docs/freeze-stub.md GG-0045)" — but the
  GG-0045 section of `freeze-stub.md` ("Panic semantics", lines 205–224) documents
  only `panic(nil)` → `*PanicNilError`. The pointer is wrong.
- `java/frontend/README.md:103,155` invoke "the GG-0045 stance" for "Goexit in main
  is gc's fatal deadlock (exit 2)" — likewise undocumented in the freeze doc's
  GG-0045 section. Three distinct decisions now share one requirement ID's label;
  only one is written down.

### P2 — "(M6)" comment labels collide with the generics milestone

- The loop-capture machinery is labeled `(M6)` throughout: `BoxDeclNode`,
  `BoxReadNode`, `BoxRefreshNode`, `BoxWriteNode` class javadocs;
  `NodeFactory.java` fields `boxed` (146), `boxIterSlots` (152), `boxSnapSlots` (159),
  `boxSpec` (165), `boxKinds` (170), `loopStack` (203), `LP_*` (209),
  `prescanForBoxes` (1140), `prescanRangeBoxes` (1171), and the
  `// M6 loop-capture pre-scan` section header (~1058); `pn13_defer_storm.go`'s
  "M6: per-iteration capture" comment.
- But the program's milestone M6 is generics (review brief; `CHECKPOINT.md:801`),
  and the same file uses `(M6, WS H)` for generics (`buildIdent` ~1200, `funcTable`
  216, `buildCall` doc 1338). The `, WS H` suffix is the only disambiguator.
  Future readers (and the in-flight generics worker) cannot tell at a glance whether
  a bare `(M6)` is loopcap or generics. Rename the loopcap labels (e.g. `(M6-loopcap)`
  or `(loopcap)`) — cheap, mechanical, prevents real confusion in the exact files
  the generics worker is editing.

### P2 — corpus/controlflow/README.md omits the 12 new loopcap files

- The README's file table ends at `cf11_goto.go`; `cf12_loopcap_body.go` through
  `cf23_attack_multiheader.go` (the 8 red-green + 4 self-attack programs) are present
  in `corpus/controlflow/` but undocumented. Add rows (the panic README's per-file
  table is the pattern to follow).

### P2 — minor doc staleness

- `java/frontend/Boxing.java:12` class javadoc: "shared by the M4 nodes (WS E)" —
  the class now also serves M5 (`readSlot`) and the loopcap fix (`setBoxedCell`,
  which has a good method-level comment). Update the class line.
- Dead `PanicNode` (see P1) has no superseded-by marker; add a comment or remove it.

## 3. What I could NOT verify and why

1. **Current build config.** No `Makefile`/build files in the snapshot (the M6 worker
   owns the live tree; I never touched it). The `-Xlint:deprecation -Werror` flag is
   proven present at M5 build time by the javac command lines in
   `evidence/m5_green.log` / `evidence/m5_mutant_red.log`, but I cannot confirm it
   survives in the live tree today.
2. **Loopcap runtime-side sources.** `graalgo.values.GoBox` / `graalgo.runtime.*` /
   `graalgo.safepoint.*` sources are absent from the snapshot (only the pristine
   copies of `GoBox.java`/`GoFunc.java` exist under `evidence/loopcap/pristine/`
   for reference). I verified every *call site* in `graalgo.frontend` against the
   freeze doc, but not implementations, and not the deprecation-cleanliness of the
   loopcap-added files. (The pristine `GoBox.java` copy was not audited for API
   usage since it is a backup, not the built source.)
3. **114/114 and 4/4 self-attack claims.** Asserted in `evidence/CHECKPOINT.md:791-792`
   and `evidence/loopcap/COMPLETE.md:4-7`; no test log or self-attack output exists
   in the snapshot (`m5_green.log` is the M5-era interrupted run, §P1 above). The
   corpus programs (cf12–cf23) and the pristine-tree red/green claim could not be
   re-executed from the snapshot (no build files, no jars, no test sources).
4. **FreezeSignatureTest's 37.** The test source is not in the snapshot; I cannot map
   the "37 signatures verified" to the doc's 63 rows (§P1 above).
5. **Live-tree drift.** The snapshot was taken ~13:02 CEST with the generics worker
   mid-write (`export.go` mtime 13:01:45); the live tree has since moved on. All
   findings are against the snapshot only.
6. **Compilation.** Rebuilding the snapshot was impossible (no `lib/` jars, no
   `Makefile`, incomplete source set by design). All code judgments are from reading.

## 4. Verdict

**Code craft in the reviewed scope is high; the docs do not say what the code does
in five material places.**

- The deprecated-API directive is **complied with**: no deprecated-API use in any
  snapshot source, no suppressed warnings, and log evidence that `-Xlint:deprecation
  -Werror` was in the actual M5 build. The two-writer untangle is genuinely clean —
  no wire markers, no dead booleans, no orphaned markers, no commented-out code.
  Error handling is fail-fast with paths (unknown node kinds name the extension
  hook; JSON parse errors carry line/col). The box machinery is well-named and its
  comments explain the phase-stack design where future readers need it
  (`headerPhase`, `snapshotBoxedVar`, `BoxRefreshNode` all carry the gc-oracle
  rationale).
- But the documentation layer has real gaps: the normative wire doc omits the two
  M5 wire kinds (P1); the freeze doc's builtin table names a dead class (P1), its
  function count contradicts its own tables while a third number circulates for the
  test (P1), and it omits the M5 runtime surface the AST actually calls (P1); the
  corpus README documents a fixed bug as a live limitation (P1); and the archived
  "green" log shows a terminated run (P1). None of these is behavioral — the code
  itself held up everywhere I could check it — but documentation is part of the
  change per the program's own contract, and the freeze doc in particular is
  normative: it should be fixed before sign-off, not after.
- Recommended sign-off condition: remediate the five P1s (each is a small,
  well-located edit), add the missing coordinator 114/114 + self-attack logs to
  evidence, and re-verify the Makefile flag in the live tree once the M6 worker
  releases it.
