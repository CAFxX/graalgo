# Document structure outline — v1 (draft for fixpoint iteration)

## FEASIBILITY.md

1. Verdict (1 page)
   - FEASIBLE / NOT / CONDITIONAL + confidence
   - Recommended architecture, one paragraph
   - 3 hardest problems (numbered)
   - Headline cost: 20–40 PY + 2–4 PY/yr (LOW confidence), critical path ~4–5 yrs
2. Requirements as refined (6 + 3 refinements: corrected hybrid, agent-driven upgrades, testing-first-class)
3. Prior art & effort calibration (T1)
   - No Go-on-Truffle ever published; gccgo/gollvm/llgo/TinyGo lessons
   - TruffleRuby calibration; Sulong alternative verdict-or-spike
4. Recommended architecture (expanded, then the one-paragraph version lives in §1)
   - Wrap real `go`, swap backend behind `-toolexec`; go/parser+go/types frontend service
   - One Truffle AST → three execution modes; value model; goroutine=virtual thread
   - Stdlib: maximal reuse + ~78-package adapted fork; linkname names-not-ABI
   - cgo→Panama; isolates↔momo
5. Three-mode matrix (table: JIT-only / AOT-only / AOT+JIT hybrid; rows: startup, peak, build cost, debuggability, effort, per-release toil; default = hybrid; falsifying experiments)
6. Deep dives (condensed, one subsection each)
   - 6.1 Frontend & version tracking (T2): agent-driven runbook, bounded ≤200 agent-hrs P95, release-watch agent
   - 6.2 Toolchain drop-in (T3): 19 commands, 929 txtar conformance, -toolexec seam, cgo link/ABI, build-cache namespacing
   - 6.3 Stdlib reuse (T4): 58% pure, ~60–70 intrinsics, 127k asm lines, Linux-first, linkname contract
   - 6.4 Truffle implementation (T5): AST design, value model, goroutines/Loom, defer/panic, adversarial PE
   - 6.5 Optimizations (T6): narrow wins, gc+PGO bar, falsifying benchmarks, Valhalla non-actionable
   - 6.6 JVM integration (T7): polyglot API, Panama, Loom/JEP 491, io_uring absent, JFR/Inspector, isolates↔momo
7. Testing strategy (first-class, 7 points from requirement)
8. Risks: top 5 P×cost + program kill criteria
9. Estimates (T8: 20–40 PY + 2–4 PY/yr, LOW confidence; agent-leverage [OPEN])
10. Open questions ([OPEN]/[PROPOSAL] labeled, ~15–20)
11. Claim provenance & verdict-scope ledger (per-track sources, [UNVERIFIED] carry-over)

## PHASES.md

1. Contract & conventions (evidence labels, binary done-gate style, escape protocol)
2. Phase 0 — Spec + kill-or-commit spikes (6 spikes; done-gates; kill criteria)
3. Phase 1 — Language core (Truffle AST + value model; stdlib-free)
4. Phase 2 — Runtime (goroutines/channels/GC interop on Loom)
5. Phase 3 — Stdlib bring-up (intrinsics, adapted fork, asm strategy, Linux-first)
6. Phase 4 — Driver drop-in (-toolexec wrapper; txtar conformance; parallelizable)
7. Phase 5 — Optimizations + three modes (hybrid default; falsifying benchmarks; cut-the-mode gate)
8. Phase 6 — Upgrade pipeline completion (runbook tooling; release-watch agent)
9. Cross-cutting: testing strategy built early (harness seed in Phase 0, differential corpus in Phase 1, full gates by Phase 3)
10. Estimates table + staffing + parallelization + critical path
11. Per-phase kill criteria summary table
