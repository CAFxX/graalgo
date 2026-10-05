# ADR-0001 — Phase-3 stdlib adaptation strategy: overlay graduation + interface-dispatch placement

**Status:** decided (planning).
**Date:** 2026-10-06.
**Context:** Phase 1/2's frontend resolves exactly two synthetic packages
(`runtime`, `fmt`) via `overlayImporter` (phases/phase1/frontend/export.go);
every other import is a scope error naming "WS G import overlay". Phase 3
is that overlay's graduation. Separately, interface dispatch is unbuilt
(`GoInterface` is a data carrier only; INTERFACE_FREEZE.md §3.5), and the
Phase-2 plan noted polymorphism re-measurement as "(Phase 4 input)".

## Decision 1 — overlay graduation (P3-D1)

The frontend learns to compile whole package directories (`ExportDir`,
multi-file packages, build-tag filtering) and resolve imports across a
vendored go1.27.1 stdlib tree. The ~78 coupled packages are compiled from
real, lightly adapted sources. Synthetic overlays are retained ONLY for
`runtime` (Goexit/SetFinalizer/KeepAlive/GC) and `fmt` (seven D4 symbols)
until their real packages land — they are bootstrap devices, not the
stdlib strategy.

**Alternatives considered:**
- (a) Extend the synthetic-package trick to all of `sync`/`time`/
  `context`/…: rejected — it re-implements the stdlib by hand, which is
  exactly the unbounded toil the fork model exists to avoid; every
  hand-synthesized package is a permanent divergence liability.
- (b) Full clean-room stdlib: rejected — SPEC §2.8 measured 58% of
  package dirs compiling unchanged; discarding that is pure cost.

**Re-test trigger:** if the coupled-package re-measurement at P3-M6
drifts >15% from ~78, re-examine this boundary.

## Decision 2 — interface dispatch lands in Phase 3 (P3-D2)

The Phase-2 plan's "(Phase 4 input)" note covered only the polymorphism
*re-measurement*. Dispatch itself (PIC on itab identity, limit ~3,
megamorphic hash fallback per SPEC §3.3) is load-bearing for `context`,
`io`, `sort`, `reflect`, and most of the stdlib — it must precede the
package waves (P3-M3 before P3-M7). Deferring it to Phase 4 would make
gates 1–4 unachievable in Phase 3.

**Re-test trigger:** if the PIC design proves unworkable on the value
model, P3-M3 opens with a design spike + new ADR rather than a silent
scope cut; the package waves replan behind it.
