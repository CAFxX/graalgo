# ADR-0014 — Per-node unit and PE tests (T1–T4) waived for M1a; the corpus gate stands in

**Date:** 2026-11-18
**Status:** accepted 2026-11-18 at the M1a close-out, as a *temporary* waiver
with a tracked work item (WP-NODETEST below). No requirement or SPEC text
changes.
**Scope:** [truffle-design.md](../../arch/truffle-design.md) §11 (test rules
T1–T4) and §15 (W4 as built); the M1a acceptance column of
[toolexec-design.md](../../arch/toolexec-design.md) §8 ("per node class the
unit and PE tests of truffle-design §11 (T1–T4)");
[RECOVERY.md](../../arch/RECOVERY.md) (work package WP-NODETEST).

## Context

truffle-design §11 asks, for every node class of the new node package
([graalgo/go/](../../../internal/backend/truffle/src/main/java/graalgo/go/)):
an interpreter unit test and a PE test that compiles a root containing the
node with compilation failures fatal (T1), through the polyglot API or a real
`GoLanguage` (T2), with `partialEvaluationConstant` assertions (T3), and for
dynamic sites coverage of monomorphic, polymorphic, megamorphic and nil
cases with the specialization asserted through DSL introspection (T4).
toolexec-design §8 lists these tests in M1a's acceptance column.

W4 landed without them (truffle-design §15, "Not built in this slice"). The
node package has about 200 node classes; the factory, not the nodes, makes
every type decision, so most nodes are only meaningful inside trees the
factory builds from real GIR. What exists instead:

- `make test-m1a`: every import-free corpus program, the
  [test/m1a/programs/](../../../test/m1a/programs/) set written for the gaps
  the corpus leaves (defers on both paths, Goexit, wrappers and recover,
  range-over-func, bounds and conversion edge cases, interface satisfaction),
  the multi-package modules and the reference interpreter's programs, each
  differential against live gc (stdout, normalized stderr, exit status);
  173 programs, all passing at the close-out. The reference interpreter
  (girinterp) runs the same programs against gc.
- [test/m1a/defer_path.sh](../../../test/m1a/defer_path.sh): GG-0071's
  boundary at the open-coding limit and the forced-chain differential.
- `make pe-check-m1a`: the benchmark set compiled with
  `CompilationFailureAction=ExitVM` and synchronous compilation — any
  bailout fails it.

## Decision

1. **T1–T4 are waived for M1a.** The corpus gate, `defer_path.sh` and
   `pe-check-m1a` are M1a's evidence for the node package. M1a is closed
   without per-node unit/PE test classes.
2. **The waiver is not the destination.** Work package **WP-NODETEST**
   (RECOVERY.md) builds them, prioritised by what the corpus gate cannot
   show:
   - T4 for the dynamic sites first (`Calls.Dispatch`, `Calls.IfaceDispatch`
     and the DSL-specialized arithmetic/conversion nodes): the corpus never
     drives a site megamorphic on purpose and never asserts which
     specialization it reached;
   - GG-0071's compiled-graph check: a function with no defers or only
     open-coded defers performs no `G` access and no allocation beyond the
     deferred call's argument array on the non-panic path (red when the
     chain is forced) — today verified by reading `OpenDeferRoot` and by
     `pe-check`-style runs, not by an assertion;
   - T1's PE half for every node reachable from a hot loop (a seeded missing
     `@TruffleBoundary` must turn it red).
3. **Exit:** the waiver ends when WP-NODETEST's gate holds; until then
   truffle-design §15 and toolexec-design §8 cite this ADR.

## Consequences

- A node-level regression that keeps every corpus program's output intact
  (a lost specialization, a new allocation on a fast path, a boundary that
  moved into compiled code without a bailout) is not caught per push.
  `pe-check-m1a` catches bailouts on the benchmark set only.
- The acceptance column of M1a in toolexec-design §8 is met with this waiver
  recorded, not silently.
