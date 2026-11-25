# ADR-0022 — Keep the AST interpreter; no Bytecode DSL migration on 25.0

**Date:** 2026-11-25
**Status:** accepted 2026-11-25 by the coordinator: no migration now
(lane research-tsbc, WP-TR-AUDIT-12 of
[truffle-audit.md](../../arch/truffle-audit.md) §4 and TR-13). Revisit
trigger: after the Truffle upgrade to ≥ 25.6 (WP-TR-AUDIT-8), and only if
a heap census of a large program shows the AST dominates retained heap
**Scope:** the execution model of the new path (SPEC §3.3, GG-0014; would
need amendment A5). Evidence:
[tstring-bytecode-study.md](../../arch/tstring-bytecode-study.md) §B.

## Context

The audit's greenfield stance (§4) proposed deciding the Bytecode DSL by a
bake-off: graalgo has large mostly-cold code, high RSS, a slow interpreter,
`goto` state machines and an existing IR. A representative slice (arithmetic
loop, calls, closures, `switch`, `defer`/`panic`/`recover`) was built from
one IR as a graalgo-shaped AST and as a `@GenerateBytecode` interpreter on
the pinned 25.0.4.1.1 jars.

## Decision (proposed)

Keep the AST interpreter. Do not start a Bytecode DSL migration on 25.0.
Re-evaluate only after WP-TR-AUDIT-8 reaches ≥ 25.6, and only if a heap
census of a realistic program shows AST nodes are a dominant share of the
retained heap or a warmup profile shows interpreter dispatch dominating.

## Reasons (measured)

- **No interpreter win**: before JIT, with realistic (polluted) host
  profiles, the AST is 1.5–2.5× faster on 3 of 5 kernels; first calls are
  2–12× slower in bytecode.
- **Peak**: parity on loop/calls/closures/switch; `defer`/`panic` 9× slower
  in bytecode (a deopt cycle in the deferring function, not root-caused).
- **Footprint**: −20 % per function only while it runs in the uncached tier;
  +7–30 % once specialized. graalgo already builds a function's AST lazily
  at its first call, so uncalled functions cost nothing either way.
- **Control flow**: the builder rejects backward branches, so Go `goto` loops
  become `While` + a runtime dispatch that compiled code cannot fold (no
  `mergeExplodeKey` before 25.5); counted-loop strip-mining (what vectorizes
  `loop` to 0.2× gc) has no equivalent; a panic escaping a deferring frame
  reports the handler's position, not the `panic`'s, until 25.6's
  bytecode-index exception constructor.
- **Cost and stability**: XL migration (`NodeFactory` and the node package
  rewritten, every PE-specific design redone) on an API documented as
  experimental that changes in each of 25.1–25.6; on HotSpot the generated
  interpreter for ≈ 260 node kinds risks exceeding `HugeMethodLimit`.

## Consequences

- Goroutines are unaffected either way (carrier continuations yield Java
  stacks; bytecode frames are Java frames). `@Yield` stays a candidate for
  a thread-free `iter.Pull`/range-over-func, independently of this ADR.
- Tags/instrumentation come from F-12.1 work on the AST, not from the DSL.
