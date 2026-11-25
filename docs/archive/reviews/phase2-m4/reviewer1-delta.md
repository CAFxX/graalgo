# Delta re-verification — ADR-0002, reviewer 1 of 2

**Reviewer:** reviewer 1 (architecture correctness) · **Date:** 2026-10-05
**Scope:** `git diff 8044d2a 3d94f0f -- phases/phase2/adr/ADR-0002-gc-interop.md phases/phase2/PLAN.md` only (265 lines). Read-only; no repo file modified.
**Verdict: DELTA-GO** — all 7 P1 + 5 P2 + the PLAN cross-doc note are genuinely addressed with substance, not hand-waving. The ADR may proceed to P2-M4 implementation.

## Per-finding check

**P1-1 (SetFinalizer divergences): CLOSED.** Both required parts landed with substance beyond the minimum: (a) table row now mandates "**single shared** `java.lang.ref.Cleaner` (one thread → restores gc's serialized finalizer execution)"; (b) an explicit "SetFinalizer accepted divergences" section with separate rows for *resurrection impossible* and *finalizer thread identity* (constrain bodies, or a scheduler trampoline so they run as real goroutines — "P2-M4 decides; default: constrain + document"). The author also added two rows I did not demand (SetFinalizer(x,nil) removal via cancelable registration; exporter-boxing residual risk with a driven-through-the-real-exporter differential test). The "emulate" stance is now sound.

**P1-2 (GC() blocking semantics): CLOSED.** Author took the stronger remediation branch: `System.gc()` **+ poll GC MXBeans until collection counts advance, bounded timeout**; on timeout "return with the divergence documented". Stance downgraded to "documented approximation — callers must not depend on reclamation having completed". The mechanism is exactly what was asked for.

**P1-3 (ReadMemStats field table): CLOSED.** Row now reads "field-by-field table (P2-M4 deliverable): exact / approximated-with-mechanism / unavailable-with-justification per `MemStats` field"; unmappable fields route to `DIVERGENCES.md`. Also wired into Consequences (P2-M4 implementation list) and closeout checklist item 1 (blocking). The three-way classification is the required substance.

**P1-4 (heap profiles + FreeOSMemory missing): CLOSED.** Both rows added: `runtime/pprof` heap profiles → JFR allocation profiling / heap dump, "stance TBD in P2-M4 (emulate or unsupported-with-justification)"; `debug.FreeOSMemory()` → "no JVM analog (`System.gc()` is not it)" with stance unsupported-with-justification (P2-M4 confirms). The pprof stance is bounded to two explicit options and pinned by closeout checklist item 1, which requires "pprof + FreeOSMemory stances" as blocking — not hand-waving.

**P1-5 (F2 remedy incoherence + missing measurement vehicle + collector choice): CLOSED.** F2 rewritten as "p99/max GC pause — measured via JFR `jdk.GCPhasePause` / GC-MXBean notifications on the conformance corpus — breaks the scheduler pause budget derived from GG-FAIR-0002, *and* no supported collector/tuning closes it → re-open D1", with an explicit note that the old remedy clause "was incoherent". A new "Supported collector decision" paragraph records default = JVM default (G1 on GraalVM 25), expected-to-work set = {G1, ZGC, Shenandoah}, G1 tested.

**P1-6 (F3 grep gate + semantic backstop): CLOSED.** Regression guards are now two layers: (1) the grep gate gains exactly the requested patterns — `allocateDirect`, `theUnsafe`-style reflective acquisition, `import sun.misc.*` / `jdk.internal.misc.*`; (2) the semantic backstop is explicit — "a GC-visibility differential per value kind: allocate a graph, retain it *only* through the carrier under test, force GC, weakref-verify survival", and F3's text now says "The GC-visibility differential is the semantic backstop."

**P1-7 (missing D2 falsifier): CLOSED.** **F4 (roots)** added verbatim to spec: "a standing GC-torture differential — weakref-verified survival of graphs retained only via parked-goroutine stacks across *all* park kinds (channel/timer/select/quantum/IO-enter), run per supported collector — whose failure re-opens D2. New park paths must preserve the token-before-park funnel invariant (code-review checklist item)."

**P2-1 (funnel invariant argument): CLOSED.** D2 section now carries the "structural funnel invariant" paragraph: single `LockSupport.park()` site (`SchedulerCore.java:273`), both callers enqueue the handle-holding token before parking, `close()` unparks before clearing queues — explicitly generalizing beyond the spike with the invariant as the basis.

**P2-2 (supported configurations undefined): CLOSED.** See P1-5: collector set enumerated; "spike re-run per collector is a P2-M4 verification-backlog item"; re-test trigger changed from "change of default JVM GC" to "change of supported collector set".

**P2-3 (cgo/FFM trigger): CLOSED.** Trigger added verbatim with the rationale: "cgo/FFM boundary landing (any design letting native code retain a Go reference across a call — breaks D2's all-references-are-JVM-references invariant; P2-M7's pinning audit does not cover root visibility)".

**P2-4 (KeepAlive vehicle): CLOSED.** Row now names "`Reference.reachabilityFence(x)` at the intrinsic".

**P2-5 (allocateInstance rationale): CLOSED.** "One line per pattern stating what it guards (off-heap storage / barrier bypass / raw-address access). Rationale discipline: `Unsafe.allocateInstance` creates an ordinary *heap* object (no barrier hole) — it is not listed under the barrier gate."

**Cross-doc note (PLAN §2 WS D stale wording): CLOSED.** PLAN.md D row now reads "write-barrier integrity — JVM barriers cover all Go stores, no Truffle-level barrier (ADR-0002 D3)" and the §3 open item is annotated "[OPEN: heap layout — see §3; closed by ADR-0002]". The stale contradiction is gone.

## Status-line honesty: CONFIRMED

The ADR status line reads "remediated 2026-10-05 — … all 13 P1 + 9 P2 findings are addressed in this revision. **Delta re-verification by both reviewers pending — implementation starts after they confirm.**" It does not claim re-verification is complete; it asserts remediation and explicitly gates implementation on reviewer confirmation. Honest.

## Residual observations (non-blocking)

1. The "Verification backlog" under D2 honestly records two known gaps (Truffle-frame root coverage needs its own weakref-torture case; spike re-run per collector) as P2-M4 work — both are covered by the new closeout checklist's evidence requirements, so they are tracked, not dropped.
2. The new "P2-M4 closeout checklist (blocking)" is a strong process addition: three items with named evidence files. It converts the ADR's promises into auditable gates.

**Bottom line:** the remediation is real. Every finding got its required substance, and two (P1-1, P1-5) got more than was asked. **DELTA-GO** — ADR-0002 may proceed to P2-M4 implementation.
