# Shadow-overlay retirement plan (review P1-3, lane C)

Status: Phase 1 complete (2026-11-01). 7 pairs remain; one retired.

## The problem in one paragraph

Several classes exist twice: a truffle-module copy and a runtime-module
replacement, with classpath order deciding which one loads. The two
backend trees form a compile cycle (the AST calls runtime intrinsics;
the runtime calls back into values/frontend), so the runtime module
compiles the joint round while the truffle module compiles only these
copies — and at load time the runtime classes precede the truffle
classes, so the replacements shadow the originals. That mechanism is
documented, but "documented" does not make two definitions of one class
sound: every tool that reads source (IDE, debugger, `javap`, stack
traces) shows one file while the JVM may load the other, and any future
launcher that builds its own classpath used to re-implement the ordering
by hand. This plan retires the mechanism pair by pair, with a hard end
state, instead of collapsing it in one risky change.

## Why not delegating facades

A same-FQN "facade" cannot delegate to the same-FQN runtime class: at
load time the facade IS the class the loader picks, so a call through it
never reaches the runtime copy. A differently-named delegate would
change every call site's fully-qualified name — that is the deletion
path with extra steps and a second class to retire later. So the
retirement mechanism is deletion of the truffle copy, one pair at a
time, once the runtime copy is proven to be the sole real provider for
that pair. No new bridge is created at any step: no new synthetic
package, no name-keyed special case, no import overlay, no new wire
node.

## Phase 1 — done (lane C, 2026-11-01)

1. **One shared check.** The per-launcher fail-fast probes were
   extracted into
   [OverlayCheck](../../internal/backend/runtime/src/main/java/graalgo/launcher/OverlayCheck.java)
   (runtime module, so the interop module can use it without a new
   compile cycle). It has two layers: the `Scheduler.isPhase2()` canary
   (pre-existing R-01 semantics, wording kept) and a resource-root
   comparison proving every overlay class resolves from the same
   classpath root as the canary — the layer that catches a fragmented
   classpath the canary alone cannot. No overlay class is loaded or
   initialized for the second layer, so static initializers are never
   triggered by the check. Failure is always loud (`IllegalStateException`
   with a `FATAL` message); a missing class or a missing check is a
   failure, never a skip.
2. **Every entry point uses it.** See the audit table below. The two
   scheduler launchers previously asserted only after a successful
   build (and after `--check` returned); all three Java launchers now
   assert before anything else, matching the main launcher's position.
3. **Gate tied to the inventory.** The duplicate-FQN gate
   ([check_duplicate_fqn.py](../../.github/scripts/check_duplicate_fqn.py),
   already wired into `make check` and CI) now fails unless three lists
   agree exactly: `STUB_REL` in the runtime Makefile, bridge-inventory
   entry B-8, and `OverlayCheck.OVERLAY_CLASSES`. A pair that exists in
   code but not in the inventory is now a build failure, not a
   documentation gap.
4. **Pilot retirement: Safepoint.** The truffle no-op
   (`internal/backend/truffle/src/main/java/graalgo/safepoint/Safepoint.java`)
   was deleted, leaving the runtime module as the sole definition. It
   was the safest pilot: a single static method, the runtime
   implementation is documented Phase-1 parity when no scheduler is
   installed (no-op then, quantum check otherwise), no drift test pins
   the truffle copy, and the freeze test resolves the class by name so
   it now pins the runtime copy. No wired classpath loads truffle
   classes without runtime classes first (see the audit table), so no
   loaded behaviour changes; the only behaviour change is that a
   hypothetical truffle-only classpath now fails loud with
   `NoClassDefFoundError` instead of silently no-op polling, and no
   wired path does that.
5. **Tests.** [OverlayCheckTest](../../internal/backend/runtime/src/test/java/graalgo/launcher/OverlayCheckTest.java)
   (registered in `SchedTestRunner`) covers the in-process pass, the
   reversed-order failure, and — the case the old probe missed — a
   split classpath where the canary passes but another class resolves
   elsewhere. The existing `LauncherOverlayTest` still covers the main
   launcher's fail-fast through its delegating seam.

## Launcher audit (all entry points, as of Phase 1)

| Entry point | Overlay check | Notes |
|---|---|---|
| `graalgo.launcher.Main` ([Main.java](../../internal/backend/runtime/src/main/java/graalgo/launcher/Main.java), via `graalgo-run`) | Shared check, top of `run()` (before `--help`/`--check`) | Pre-existing position; body now delegates |
| `graalgo.sched.launcher.SchedMain` ([SchedMain.java](../../internal/backend/runtime/src/main/java/graalgo/sched/launcher/SchedMain.java), via `graalgo-run --gmp`) | Shared check, moved to top of `run()` | Was after build + after `--check` return — the gap Phase 1 closed |
| `graalgo.p3sync.launcher.P3SyncMain` ([P3SyncMain.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3SyncMain.java), via `graalgo-sync-run`) | Shared check, moved to top of `run()` | Same gap as SchedMain; this is also the product path for `graalgo build` artifacts and the differential harness |
| `FastCycleDaemon` ([FastCycleDaemon.java](../../internal/backend/fast-cycle/FastCycleDaemon.java), `RUN` + `RUNTEST`) | Reflective per-invocation check through the child loader | The daemon is compiled standalone, so it cannot reference the check at compile time; it loads the child loader's copy — the shared implementation, not a second copy. A missing check class fails the invocation. Script-driven invocations always pass `FULL_CP` (runtime first) |
| `SchedTestRunner`, `graalgo.test.Runner`, `IfaceTestRunner` | Shared check, top of `main()` | Runner classpaths all put runtime classes first |
| Artifact launcher ([artifact.go](../../internal/driver/artifact.go) template) | Inherited | Emits a program that execs `graalgo-sync-run`, i.e. P3SyncMain; no separate check needed or added |
| `graalgo` driver ([main.go](../../cmd/graalgo/main.go)) | Inherited | Execs `graalgo-sync-run`; no separate check |
| Differential harness ([run.sh](../../test/differential/run.sh)) | Inherited | Uses `graalgo-sync-run` as the backend; no separate check |
| `PinAudit` main | None, by audit | Reads JFR recording files; it imports no shadow class, so there is nothing to guard |
| Truffle smoke tests (`Smoke`, `DslSmoke`) | None, by audit | Run with truffle jars only and no runtime classes; they touch no shadow class, and adding the check there would fail for want of the check class itself |

## Remaining pairs (retire in this order)

Order rationale: frozen stubs first (their runtime copies are strict
supersets or behaviour-preserving replacements), forked implementations
next (each needs its API-delta audit), the canary carrier last (its
retirement also retires the check's first layer).

| Pair | Truffle side | Runtime side | Retirement precondition (beyond the recipe below) |
|---|---|---|---|
| `graalgo.runtime.Netpoll` | Phase-1 UOE stub | Deleted-API UOE stub (different messages) | Grep tests for the Phase-1 message; none can pass on wired (runtime-first) classpaths today, but the deleting lane confirms it rather than assuming it |
| `graalgo.runtime.ChanOps` | Phase-1 UOE stub | Real implementation | None beyond the recipe; the joint round already compiles callers against the runtime copy |
| `graalgo.runtime.GcInterop` | Phase-1 UOE stub | Real implementation | None beyond the recipe |
| `graalgo.runtime.Timers` | Real `nanotime`, stubbed sleep/timer-heap | Real everything | Grep tests for stub-`sleep` UOE expectations; same argument as Netpoll |
| `graalgo.runtime.GoroutineState` | Static-holder lookup | `ThreadLocal` vthread-scoped lookup, byte-identical instance API | `javap` API diff of both copies + audit of holder-accessor callers in the joint round; the runtime lookup is the one already verified live around goroutine execution |
| `graalgo.runtime.Panic` | Original implementation | Original + panicnil knob + bug-019 descriptor-inference port (superset) | `PanicOverlayDriftTest` compares the runtime copy against the truffle original by reflection — after deletion it must be re-homed to the frozen spec ([freeze-stub.md](../spec/freeze-stub.md) + `FreezeSignatureTest`) or deleted once that spec coverage is shown; the deleting lane decides, with review |
| `graalgo.runtime.Scheduler` (LAST) | Phase-1 UOE stub | Real implementation + `isPhase2()` canary | Retiring the canary carrier retires layer 1 of the check: layer 2 (resource roots) already suffices once `Scheduler` is single-definition, so the last change deletes `OverlayCheck` with the pair and removes its call sites in the same commit |

## Deletion recipe (every pair, no exceptions)

1. `javap` API diff of the truffle copy against the runtime copy;
   every public member of the truffle copy must exist in the runtime
   copy with an identical signature, or the difference must be a
   documented, reviewed, behaviour-preserving delta.
2. Grep for path-specific references (the truffle source path, the
   truffle classes dir) and message-specific references (the truffle
   copy's exception texts) in code, tests, scripts, and docs.
3. Delete the truffle source file and update ALL list sites in the same
   commit: `STUB_REL` (runtime Makefile), `STUBS` (truffle Makefile),
   `STUBS` (fast-cycle script), inventory B-8 bullets,
   `OverlayCheck.OVERLAY_CLASSES`, [REPO_STRUCTURE.md](../REPO_STRUCTURE.md)
   counts, and the Makefile header comments. A partial update is a
   build failure by construction (the gate's three-way tie), not a
   silent drift.
4. Run the differential probe (`make snapshot-check` plus the corpus
   differential for the affected area) and show no drift. Never
   re-baseline a snapshot to make a red check green: a drifted
   snapshot after a deletion is a failed deletion until explained.
5. Independent review before merge (standing order). The deleting lane
   does not integrate itself.

## Hard end state

- `STUB_REL` is empty and the truffle module's main target compiles no
  stubs (the target itself is removed when the last pair goes).
- `OverlayCheck` is deleted with the last pair, and every call site is
  removed in the same change. The overlay needs no guard once there is
  nothing to shadow.
- `check_duplicate_fqn.py` stays, with an empty allowlist: it still
  fails on ANY same-FQN class across backend modules, so a future
  re-introduction of a shadow pair fails the build instead of
  returning quietly.
- Inventory B-8 moves to the Retired section; the
  [REPO_STRUCTURE.md](../REPO_STRUCTURE.md) overlay section is deleted.
- The freeze spec ([freeze-stub.md](../spec/freeze-stub.md)) stays: it
  pins signatures, and after retirement those signatures live in
  exactly one place each.

## Design decision: no new leaf module (recorded)

An alternative end state extracts the 7 classes into a new leaf module
both backend modules depend on. Rejected: the joint round already
compiles truffle sources against the runtime copies, so a leaf module
would not change any compiled call site — it would only add a third
build unit and a second freeze surface to keep in sync, in exchange for
removing a load-order dependence that deletion removes entirely. If a
future consumer ever needs truffle classes without runtime classes, that
consumer reopens this decision with its concrete classpath in hand;
until then, deletion is the design.
