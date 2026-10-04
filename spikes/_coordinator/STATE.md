# Phase-0 coordinator state (updated 2026-10-03 ~05:42 UTC)

## Spike verdicts
| Spike | Worker | Status | Verdict | Kill fired? |
|---|---|---|---|---|
| 01 asm/cgo | 468654bb | COMPLETE | PASS (exhaustive classification; atomics/bytealg/mem/bits/crypto differential vs real asm, 0 mismatches; sync/atomic 95/95, bytes+strings suites green on stub; Panama cgo bit-identical) — BUT: gogo/mcall/morestack intrinsic target was vthread ops, re-test trigger pointed at spike 2; under re-scope the target becomes scheduler ops (re-validation owed) | no (conditional, see note) |
| 02 goroutines-on-Loom | b882011c | COMPLETE | **FAIL — kill trigger §8.2 FIRED** (see KILL section below) | **YES** |
| 03 version-tax dry run | not spawned | blocked on 01/02/04 | — | — |
| 04 reflect/unsafe | e70e818f | COMPLETE | PASS (302-use inventory all handle-table-formulable; linkname churn 2 items, 7.5x headroom under kill trigger) | no |
| 05 cmd/go coupling | de8cf63d | COMPLETE | PASS (52/52 txtar scripts; -toolexec seam sufficient, no fork; F6 response-file gaps fixed in wrapper v4 with zero driver changes; driver never modified — temp harness patch reverted+hash-verified) | no |
| 06 Sulong eval | 598b1de2 | COMPLETE | COMMIT-DIRECT (19 vs 6; REPLAN trigger not met) | no |
| 07 harness seed | 4abc4a0e | COMPLETE | PASS (45/45 corpus, 0 false positives, 9/9 mutation classes detected, 41 unit tests, run.sh exit 0) | no |
| SPEC.md + registry (GG-*) | 6861b02f | COMPLETE | SPEC.md 1040 lines, 60 reqs GG-0001..GG-0060 draft, lint clean (60 reqs/371 refs), [OPEN]=0 on go/no-go (only Appendix C version pin, non-go/no-go) | no |

## Done-gate checklist (PHASES.md §2)
1. SPEC.md closed, [OPEN]=0 on go/no-go — DONE by worker; coordinator full read-through review still owed before sign-off
2. GG registry, lint-clean — DONE (60 draft IDs, lint ok)
3. Spikes 1–6 verdicts + reproduction — 5/6 + spike 3 correctly not run (spikes 1,2,4,5,6 done; spike 3 moot against dead architecture)
4. Differential smoke harness in CI, mutation-validated — DONE (spike 7 PASS; peer review of verdict still owed per worker caveat)
5. ADR signed — pending (needs spikes 5+6; spike 6 done → frontend=direct Truffle AST)
6. Two independent peer reviews of spike verdicts — pending (after all spikes)

- Spike 4 reconciled T4's "243" (per-line, 2 files) vs 302 (per-occurrence, 9 files); 32/32 layout
  differential vs gc; Q3 recommendation: handle-table primary, raw-address only for
  foreign memory. Re-test triggers: new Go release, arenas experiment graduating.
- Spike 7 caught its own vacuous-validation bug (mutant-vs-mutant) and fixed it; residual accepted:
  address normalization blinds pointer-identity changes (correct: future backends may lay out
  the heap differently). Worker recommends 1–2 independent reviews before it gates real work.
- Spike 2 worker was interrupted once; resumed (interruption monitor), currently running.
- req lint full-root scan is slow (>10 min) due to spikes/06-sulong/dl/ (1.4 GB TinyGo tree);
  follow-up: add --exclude support to the req fork or accept slowness. /tmp is cleaned
  periodically — persistent binaries must live under the workspace, not /tmp.
- Spike 2 (Loom) interrupted twice transiently; resumed, currently running.
## KILL TRIGGER (2026-10-03 ~06:40 UTC) — program STOPPED as scoped
Spike 2 verdict: FAIL. Trigger §8.2: systematic scheduling-observable divergence
(unbounded starvation of latency goroutines under CPU saturation) that back-edge
yields cannot close. 10+ runs, 0 samples, all yield modes (OFF/POLL@10ms/POLL@1ms/
UNCOND/PARK), parallelism 1 and 2. Root cause structural: Loom's FJP carrier has
no woken-thread priority (no runnext equivalent); no public API to install a custom
scheduler on JDK 25 (coordinator verified: VirtualThreadBuilder(Executor) is
package-private "invoked by tests"; resubmission funnels through FJP queue discipline
with owner priority — JDK 25 src.zip). Workarounds tried and failed: 7x yield-density
increase, timed-park instead of yield, platform-thread unblocker. Breaks a fundamental
Go liveness property (idiomatic spin-wait hangs; silent hangs, not errors).
Coordinator assessment: verdict SOUND (see full analysis in coordinator handoff).
Program kill criterion FIRED: no scoped workaround within the 1:1-vthread architecture.
STOPPED: spike 3 (version-tax) NOT spawned — pointless against a dead architecture;
ADR NOT started; peer reviews NOT started. Spike 5 finished: PASS. All spike evidence collected. Awaiting Carlo's explicit decision: KILL the program
vs RE-SCOPE to interpreter-level M:N scheduler (coordinator's viability analysis in
handoff; needs its own kill-or-commit spike + SPEC/estimate rework before Phase 1).
## Notes
- Spike 6 ran before GraalVM install finished (no JVM on box); verdict overdetermined by IR
  version gap (TinyGo emits opaque-ptr LLVM 22 IR; Sulong ceiling ≤12.0.1) — setup gap does
  not weaken it. Re-test triggers recorded in its REPORT.md.
- GraalVM JDK 25.0.4 at ~/tools/graalvm-jdk-25 (sha256-verified).
