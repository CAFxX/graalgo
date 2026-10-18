# P2-D8 same-FQN classpath overlay — design record (RV22-P1-1)

**Status:** design for remediation lane `p1-1-overlay-inventory` (review 2026-10-18 22:00 CEST,
item RV22-P1-1). Master tip at design time: `56a3dd68`.

## 1. What the machinery is

Eight classes exist twice under identical fully-qualified names, once in
`internal/backend/truffle/src/main/java` and once in
`internal/backend/runtime/src/main/java`:

| FQN | truffle side | runtime side |
|---|---|---|
| `graalgo.runtime.ChanOps` | UOE stub (GG-0020 freeze) | real channel implementation |
| `graalgo.runtime.GcInterop` | UOE stub (GG-0020 freeze) | real GC interop |
| `graalgo.runtime.Netpoll` | UOE stub (GG-0020 freeze) | documented deletion (GG-0018, still UOE but with the deletion message) |
| `graalgo.runtime.Scheduler` | UOE stub (GG-0020 freeze) | real scheduler glue |
| `graalgo.runtime.Timers` | UOE stub + real `nanotime()` | real timer heap |
| `graalgo.runtime.Panic` | real Phase-1 implementation (M5, WS F) | Phase-2 replacement (+ panicnil emulation) |
| `graalgo.runtime.GoroutineState` | real Phase-1 static holder | Phase-2 replacement (vthread-scoped holder) |
| `graalgo.safepoint.Safepoint` | real Phase-1 no-op | Phase-2 replacement (scheduler quantum check) |

The loaded copy is decided by classpath order: every production classpath
puts `runtime/out/classes` before `truffle/out/classes`
([`internal/backend/graalgo-run`](../../internal/backend/graalgo-run),
[`internal/backend/graalgo-sync-run`](../../internal/backend/graalgo-sync-run),
the `RUN_CP`/`TEST_CP` variables of the
[runtime](../../internal/backend/runtime/Makefile) and
[interop](../../internal/backend/interop/Makefile) Makefiles, the truffle
[Makefile](../../internal/backend/truffle/Makefile)'s `run`/`test`
targets). The mechanism exists because one `javac` round cannot hold two
classes with the same FQN: the runtime module compiles the joint round
(its own sources + the truffle module's non-stub sources), and the truffle
module's `main` target compiles only the 8 stubs against the already-built
runtime classes (see the COMPILE CYCLE note in the truffle Makefile and
`STUB_REL` in the [runtime Makefile](../../internal/backend/runtime/Makefile)).

Note: the review said "seven classes"; the eighth (`Safepoint`) is a real
no-op vs real implementation pair with the same shadowing and the same
desync risk, so it is covered here too.

## 2. Adjudication: bridge (B-8), not adjudicated-non-bridge

**Decision: add a B-entry.** Rationale, evidence-based:

1. It matches the inventory's own bridge definition verbatim: "import
   overlays" ([`docs/arch/bridge-inventory.md`](bridge-inventory.md)).
   The code itself names the mechanism "the documented overlay mechanism"
   (truffle `GcInterop.java` javadoc) and "P2-D8 shadowing pattern"
   (runtime `Panic.java` javadoc).
2. It is invisible to the no-new-bridges gate:
   [`check_no_bridges.py`](../../.github/scripts/check_no_bridges.py) only
   inspects *added* lines, and this machinery predates the gate — the exact
   blind spot the review identified.
3. The failure mode is the classic bridge hazard: a new launcher, test
   runner, or native-image assembly that assembles the classpath in the
   wrong order silently resurrects fail-fast stubs (or, worse, the Phase-1
   static `GoroutineState` holder shared across goroutines) into user
   programs.

An "adjudicated non-bridge" rationale would have to argue the mechanism is
not an overlay, which contradicts both the definition and the code's own
words. So: B-8 in the inventory, with the retirement plan below.

Existing mitigations (kept, not replaced): the `STUB-SHADOW GATE` in the
runtime Makefile `main` target (fails the build if any of the 8 stubs or
its same-FQN replacement is missing from source), and the per-launcher
`assertSchedulerOverlay()` fail-fasts (now unified — see §4).

## 3. Deletion plan for the 5 stub files — NOT executed by this lane

The 5 pure-`UnsupportedOperationException` stubs
(`internal/backend/truffle/src/main/java/graalgo/runtime/{ChanOps,GcInterop,Netpoll,Scheduler,Timers}.java`)
are dead in every documented config: every production and test classpath
puts `runtime/out/classes` first, so the stub `.class` files are never
loaded. (The only classpath that loads them is the truffle smoke-test
classpath, which contains no `graalgo.runtime` references at all —
verified by grep over
[`internal/backend/truffle/src/smoke/`](../../internal/backend/truffle/src/smoke/).)
Deletion is therefore *mechanical*, not semantic — but it is not a one-line
change, and this lane does not execute it. What breaks if they are deleted
today, and what must change with them:

1. The [truffle Makefile](../../internal/backend/truffle/Makefile): the
   `STUBS` list names the 5 files explicitly — the `main` target would fail
   on missing sources. The 3 remaining stubs (`GoroutineState`, `Panic`,
   `Safepoint`) stay: they are the real Phase-1 originals, kept as the
   documented overlay contract.
2. The [runtime Makefile](../../internal/backend/runtime/Makefile): the
   `STUB-SHADOW GATE` in the `main` target requires each of the 8
   `STUB_REL` paths to exist on *both* sides. Deleting the 5 stubs requires
   shrinking `STUB_REL` to the 3 real pairs and adjusting the gate loop (or
   splitting it into a stub-pair gate and a real-pair gate).
3. The runtime Makefile header comment ("The 8 frozen phase-1 stubs…") and
   the truffle Makefile header/COMPILE CYCLE note ("the 8 frozen phase-1
   stubs", "Kept as an explicit list") must be updated to 3.
4. Javadoc/comment references to "the frozen phase-1 stubs" in the runtime
   module's `ChanOps`/`GcInterop`/`Netpoll`/`Scheduler`/`Timers.java`,
   [`SchedMain.java`](../../internal/backend/runtime/src/main/java/graalgo/sched/launcher/SchedMain.java),
   truffle [`NodeFactory.java`](../../internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java),
   and the truffle `GcInterop.java` ("the Phase-2 module shadows this
   class…") must be reworded — deleting the files without updating the
   comments is doc drift (a standing P2-class finding).
5. [`FreezeSignatureTest`](../../internal/backend/truffle/src/test/java/graalgo/runtime/FreezeSignatureTest.java)
   (truffle) keeps passing unchanged: the truffle `test` classpath already
   resolves those FQNs to the runtime classes (`RUNTIME_CLASSES` precedes
   `OUT_MAIN`), and it asserts signatures, not file locations.
6. The generalized `OverlayDriftTest` (see §5) must drop the 5 stub pairs
   in the same change — otherwise it fails on the missing truffle sources.
   (It loads them from `../truffle/out/classes`, which would no longer
   contain them.)

**Concrete steps for the deletion lane** (in order):

1. Shrink `STUB_REL` (runtime Makefile) and `STUBS` (truffle Makefile) to
   the 3 real pairs; update both Makefiles' header comments and the
   stub-shadow gate.
2. Delete the 5 stub `.java` files.
3. Update `OverlayDriftTest` to cover only the 3 real pairs; update the
   B-8 inventory entry (mark the stub-deletion sub-item done, keep the
   entry for the 3 lasting pairs).
4. Reword the "frozen stub" javadocs listed in (4).
5. Verify: `make main` in truffle, runtime, interop (cold `out/`); `make
   check` in all three; `SchedTestRunner`, truffle `Runner`, and
   `IfaceTestRunner` green; one `graalgo-run` and one `graalgo-sync-run`
   differential smoke program each.

**Preconditions (all true at design time, re-verify at deletion time):**
no test asserts the stub UOE messages (verified: the only UOE assertion in
the backend tests is `TimerTest.testNetpollDeletion`, which targets the
*runtime* Netpoll's GG-0018 deletion message); no smoke source references
`graalgo.runtime.*`; every documented classpath keeps
`runtime/out/classes` first.

The 3 real pairs (`Panic`, `GoroutineState`, `Safepoint`) are **not**
slated for deletion: they are the lasting overlay mechanism (Phase 2
genuinely replaces Phase-1 behavior), permanently drift-guarded.

## 4. Shared launcher guard (implemented by this lane)

`SchedMain` and `P3SyncMain` each carry a private, duplicated
`assertSchedulerOverlay()` that calls `Scheduler.isPhase2()` and converts
`NoSuchMethodError` (stub loaded) into a loud `IllegalStateException`.
This lane extracts one shared helper —
`graalgo.sched.launcher.OverlayGuard.assertPhase2Overlay()` in the runtime
module (visible to interop via `RUNTIME_CLASSES`, already on its compile
classpath) — and replaces both private copies with calls to it.

Wired into every entry point that requires the Phase-2 overlay:

- [`SchedMain`](../../internal/backend/runtime/src/main/java/graalgo/sched/launcher/SchedMain.java)
  (phase-2 `graalgo-run`) — was already checking; now via the helper.
- [`P3SyncMain`](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3SyncMain.java)
  (phase-3 `graalgo-sync-run`) — was already checking; now via the helper.
- `SchedTestRunner`, `TortureBattery` (runtime tests),
  [`IfaceTestRunner`](../../internal/backend/interop/src/test/java/graalgo/iface/IfaceTestRunner.java)
  (interop tests) — new; their classpaths already put runtime classes
  first, so the guard is a no-op today and a tripwire against future
  classpath edits.

Deliberately **not** wired:

- `graalgo.launcher.Main` (Phase-1 launcher): Phase 1's contract is
  scheduler-free; the stubs are Phase 1's legitimate fail-fast surface.
  Asserting `isPhase2()` there would couple the Phase-1 launcher to the
  Phase-2 module's presence and break a future pure-Phase-1 assembly.
- Truffle test runners/smoke/demo (`Runner`, `SingleTestLauncher`,
  `Smoke`, `DslSmoke`, `V2DecodeDemo`): Phase-1 scope; overlay not required.
- `PinAudit`, `FastCycleDaemon`: audit/dev tools that do not launch user
  programs on a fixed classpath (FastCycleDaemon loads user-supplied
  classpaths per `RUN` command — asserting there would be wrong).

The helper throws `IllegalStateException` with the classpath-order
diagnosis; it prints nothing (launcher stderr output would trip the
`make check` debug-leftover grep gate — the exception message is the
diagnostic). Per the truffle-language-performance skill, rule 5: the guard
executes once per launcher startup on the cold path — no hot-path or
steady-state performance impact to measure.

This is documentation plus a fail-fast relocation, not a new bridge: no
new dispatch on names, no synthetic reimplementation, no overlay logic —
the overlay itself is unchanged and now inventoried as B-8.

## 5. Generalized drift test (implemented by this lane)

`PanicOverlayDriftTest` was replaced by
[`OverlayDriftTest.java`](../../internal/backend/runtime/src/test/java/graalgo/runtime/OverlayDriftTest.java),
covering all 8 same-FQN pairs (the review asked for 7; `Safepoint` is the eighth, with identical risk). Per pair:

- Load the truffle copy child-first for the `graalgo.runtime.` /
  `graalgo.safepoint.` prefixes from `../truffle/out/classes`; everything
  else parent-first (unchanged mechanics from the Panic test).
- The overlay copy is `Class.forName(fqn)` on the test classloader
  (runtime classes first on `TEST_CP`, so this is the Phase-2 copy).
- Assert every **public** method of the truffle copy exists in the overlay
  with identical name, parameter types, return type, and static-ness.
  Type comparison is by type *name* (not `Class` identity) so inner types
  like `ChanOps$Chan`, which legitimately load in both loaders, compare
  equal.
- Reverse check (no silent widening): every public method of the overlay
  is either in the truffle copy or in the pair's documented additive
  exemption set. Exemptions (all deliberate, documented in the test):
  - `Panic`: `setPanicNilOldBehavior`, `isPanicNilOldBehavior` (panicnil emulation).
  - `Scheduler`: `isPhase2` (phase-2-only diagnostic, intentionally not frozen).
  - `ChanOps`: `makeChan(long,int,Object)`, `recvOk`, `selectRecvValue`, `selectRecvOk`.
  - `GcInterop`: `memStats`, `weakRefValue`, `newWeakRef`, `pauseNsRing`.
  - `Timers`: `after`, `tick`, `newTicker`, `newTimer`, `startTimerId`.
  - `Netpoll`, `GoroutineState`, `Safepoint`: no exemptions — exact match pinned.
- Also assert the declared public inner-class simple names match (catches a
  deleted `Chan`/`SelectCase`).

References updated: `SchedTestRunner` registration, the
`PanicOverlayDriftTest` javadoc pointer in runtime `Panic.java`,
[`docs/arch/phases/phase2/GODEBUG.md`](phases/phase2/GODEBUG.md). (The
phase2-m5 review snapshot mentioning the old name is a historical record —
left untouched.)

## 6. Inventory entry

New `### B-8` in [`docs/arch/bridge-inventory.md`](bridge-inventory.md)
("Active bridges"), stating what/why/root-cause-fix/phase per the
inventory's contract, and linking this design doc. The phased-removal order
gains the stub-deletion step under Phase 2. The entry is the grandfather
record: the no-new-bridges gate now has something to see, and the overlay
retires piece by piece (5 stubs deleted per §3; the 3 real pairs stay as
the documented, drift-guarded mechanism).
