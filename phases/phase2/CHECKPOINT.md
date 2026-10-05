# Phase 2 checkpoint — Runtime: goroutines, channels, GC interop

Append-only. Newest state at the TAIL. Each entry: date, milestone, what
landed (commits), evidence paths, review status, blockers, next.

## 2026-10-05 — Phase-2 planning

Plan written: `phases/phase2/PLAN.md` (draft for parent review). Milestones
P2-M1…P2-M10; done-gates 1–5 with binary criteria; kill criteria verbatim from
PHASES.md §4; review gate carried from Phase 1; Phase-1 lessons recorded.
[OPEN] items: GC interop heap layout (P2-M4 design spike first), `sync`
package scope for gate 1, model-checking tool choice, repo layout.

## 2026-10-05 — P2-M1: [OPEN] repo layout resolved (P2-D8)

**Decision: CONFIRM supervisor's direction — scheduler lives in
`phases/phase2/src/` as its own build module, mirroring the Phase-1 layout;
Phase 1 is consumed as a FROZEN dependency via
`phases/phase1/INTERFACE_FREEZE.md` — with ONE explicit, narrow, documented
exception (below).**

Verified facts driving the decision:
- The `Safepoint.poll()` seam EXISTS in the Phase-1 AST (`GoRootNode`
  function entries, `GoLoopNode`/etc. back-edges) — confirmed by grep, not
  assumed. Phase-1 impl is a static no-op.
- Phase 1's own docs designate three classes as "Phase 2 owns":
  `graalgo.safepoint.Safepoint` ("Phase 2 gives it semantics"),
  `graalgo.runtime.Scheduler` ("Phase 2 owns goroutine scheduling",
  stub methods throwing UOE), `graalgo.runtime.GoroutineState`
  ("Phase 2 replaces the static holder with virtual-thread-scoped holders").
- Freeze §3.5: method *bodies* are INTERNAL, signatures FROZEN.
  `FreezeSignatureTest` pins name/params/return-type/static-shape.
- `NodeFactory.registerStmtKind` is PUBLIC and documented as the WS D/E
  extension hook ("WS E registers new statement kinds") — Phase 2 can
  register the `go`-statement builder without touching NodeFactory.java.
- `*ast.GoStmt` is currently REJECTED by `frontend/export.go`
  ("Phase 2 (goroutines; M5 task brief)").

Mechanism (the designed extension, not entanglement):
1. `phases/phase2/src/` — own Makefile, own `out/`; compiles against
   Phase-1 `out/classes` + Truffle jars. New package `graalgo.sched`
   (scheduler core, no FQN collisions).
2. Same-FQN replacements for exactly the three "Phase 2 owns" classes,
   with byte-identical signatures and real bodies. Runtime classpath puts
   `phase2/out/classes` FIRST; a startup fail-fast asserts the real
   `Scheduler` (not the stub) is loaded. Bodies-INTERNAL per freeze §3.5,
   so no ADR/version bump is required.
3. The `go` statement: Phase-2 `GoStatementNode` calling the frozen
   `Scheduler.spawn(GoFunc)`, registered via the public
   `registerStmtKind` hook from the Phase-2 launcher.
4. **The one exception:** `frontend/export.go` must emit the `GoStmt` wire
   node instead of rejecting it (~10 lines). Done as a single documented
   commit; `gofrontend` toolVersion bump per freeze §5 (JSON behavior
   change); wire stays v1 (additive node kind; old consumers ignore it per
   §2.3 forward-compatibility). No ADR needed. Rationale for not avoiding
   it: source-to-source rewriting of `go f()` in the launcher would break
   SourceSections and be exactly the silent entanglement the direction
   forbids.

P2-D8 recorded. Next: scheduler core implementation (WS A).
