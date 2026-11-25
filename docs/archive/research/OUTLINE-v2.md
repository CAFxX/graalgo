# Document structure outline — v2 (fixpoint; amendments from review rounds 2–3)

Amendments applied:
- (c) §6.2 states explicitly what drop-in does NOT require (byte-identical binaries, cross-backend cache sharing).
- (d) §6.1 covers GOTOOLCHAIN interplay (inherited from stock go via the wrapper).
- (e) PHASES.md Phase 4 states the Phase-1 interface-freeze dependency explicitly.
- (i) §6 deep dives are condensed (5–8 bullets each) with pointers to full research notes; no duplication.
- Verdict fixed: FEASIBLE as a multi-year R&D program (conditional), confidence MODERATE on technical feasibility / LOW on cost-schedule.
- 3 hardest problems fixed: (1) goroutine scheduler semantics on virtual threads; (2) perpetual version-tracking toil (lifetime cost driver); (3) semantic long tail (reflect/unsafe/linkname + cmd/go behavioral parity).

## FEASIBILITY.md (~4500 words)

1. Verdict (1 page): CONDITIONAL FEASIBLE; confidence split; one-paragraph architecture; 3 hardest problems; headline cost 20–40 PY + 2–4 PY/yr (LOW confidence), critical path ~4–5 yrs.
2. Requirements as refined (6 + 3 refinements: corrected hybrid = JIT-in-image; agent-driven upgrades, bounded ≤200 agent-hrs P95; differential-testing strategy first-class, built early).
3. Prior art & effort calibration (T1): greenfield; gccgo/gollvm/llgo/TinyGo lessons; TruffleRuby decade calibration; Sulong → Phase-0 spike verdict.
4. Recommended architecture (expansion; one-para version in §1): wrap real go via -toolexec; go/parser+go/types frontend service; one Truffle AST → three modes; value model; goroutine=virtual thread; stdlib maximal reuse + ~78-pkg adapted fork; linkname names-not-ABI; cgo→Panama; isolates↔momo.
5. Three-mode matrix (table + default = hybrid (c); falsifying experiments; cut-the-mode meta-gate).
6. Deep dives, condensed with pointers (6.1 frontend/version-tracking incl. GOTOOLCHAIN; 6.2 toolchain drop-in incl. what drop-in does NOT require; 6.3 stdlib reuse; 6.4 Truffle implementation; 6.5 optimizations; 6.6 JVM integration).
7. Testing strategy (first-class; 7 points; per-commit vs per-release layers; falsifiability bar).
8. Risks: top 5 P×cost + program kill criteria.
9. Estimates: 20–40 PY + 2–4 PY/yr; LOW confidence ±50%; agent-leverage [OPEN].
10. Open questions ([OPEN]/[PROPOSAL], ~15 decision-relevant).
11. Claim provenance & verdict-scope ledger (claim → track file → label).

## PHASES.md (~3000 words)

1. Contract & conventions (evidence labels; binary done-gates; escape protocol; "a phase that cannot fail is badly designed").
2. Phase 0 — Spec + kill-or-commit spikes (6 spikes: asm/cgo, goroutines-on-Loom, version-tax probe, reflect/unsafe, cmd/go coupling, Sulong eval + testing-harness seed). Done-gates (binary). Kill criteria.
3. Phase 1 — Language core (Truffle AST + value model; stdlib-free; differential smoke harness live). Done-gates.
4. Phase 2 — Runtime (goroutines/channels/GC interop on Loom; back-edge yields; netpoller→NIO).
5. Phase 3 — Stdlib bring-up (intrinsics; adapted fork; asm strategy; Linux-first; full conformance gates).
6. Phase 4 — Driver drop-in (-toolexec wrapper; 929 txtar conformance; starts after Phase-1 interface freeze; parallelizable).
7. Phase 5 — Optimizations + three modes (hybrid default; falsifying benchmarks; cut-the-mode gate).
8. Phase 6 — Upgrade pipeline completion (runbook tooling; release-watch agent; first real upgrade as the gate).
9. Cross-cutting: testing strategy build-early map (Phase 0 seed → Phase 1 corpus → Phase 3 full gates → Phase 6 upgrade tooling).
10. Estimates table + staffing + parallelization + critical path 0→1→2→3→5.
11. Per-phase kill criteria summary table.
