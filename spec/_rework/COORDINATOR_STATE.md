# Phase-0 rework coordinator state

## Done
- SPEC rewrite agent: §3.5 full rewrite + §7.2/§7.4/§8.2/§10/§2 touch-ups, outlines r1/r2 in spec/_rework/. Verified: §3.5 header + requirements-track note now cite minted IDs; Appendix B index updated (GG-0017 title, 10 new rows).
- PHASES.md: Phase 2 scope/done-gates/kill criteria rewritten for option C; §10 estimates note (M:N counterfactual 7-12 PY [ESTIMATE] -> option C 4-8 PY); §11 Phase-2 kill row updated.
- tools/req: extended for GG-<NS>-<NNNN> IDs (SCHED/WAKE/FAIR); mintID(reg, ns); supersede stays in-namespace; go vet + go test green (new TestMintID_Namespaced/BadNamespace, TestSplitID, TestLoadRegistry_NamespacedIDs); binary rebuilt.
- Registry: 10 new reqs minted (GG-SCHED-0001..0004, GG-WAKE-0001..0003, GG-FAIR-0001..0003); GG-0017 + GG-0048 revised in place with history; req lint clean (70 reqs, 183 refs).
- ADR: adr/ADR-0001.md drafted (D1 frontend service, D2 -toolexec wrapper, D3 three modes, D4 Linux-first, D5 user-space scheduler); status=proposed, sign-off pending peer-review remediation.

## Running
- Spike 3 agent (55109d67): version-tax dry run, go1.26.x -> go1.27.1, ≤200 agent-hour bound. Reports to spikes/03-versiontax/.

## Pending (sequenced after spike 3 lands)
- Two independent peer reviews: SPEC §3.5 rewrite, registry delta (tool change + 10 new + 2 revised), spike 2b verdict, spike 3 verdict, ADR-0001. Remediate all findings, re-test.
- Sign ADR-0001 (status -> accepted).
- Final gate checklist -> COMMIT or KILL report to parent.

## Done-gates (PHASES.md §2)
- [x] SPEC closed, [OPEN]=0 on go/no-go (rewrite agent: zero new [OPEN]; verify at final check)
- [x] Registry lint-clean (70 reqs)
- [ ] Spikes 1-6 verdicts + reproductions (2b replaces 2 [done]; 3 running)
- [x] Differential harness in CI (spike 7, done)
- [ ] ADR signed (drafted; sign after reviews)
- [ ] Two peer reviews remediated

## 2026-10-03 ~13:55 — spike 3 PASS received; peer reviews launched
- Spike 3 verdict: PASS (kill trigger did not fire). Quiet-band ~50-100 agent-h vs ≤200 bound (2-4x headroom); heavy-band ~100-200 touches bound at top end (LOW confidence, residual risk). F8: response-file wire-format change 1.26->1.27 caught and fixed; recurring toil ~0.5h/format-change, mechanical.
- R1 (scheduler lens) reviewing: SPEC §3.5/§7.2/§8.2, spike 2b REPORT, 10 new + 2 revised reqs. -> reviews/phase0-rework-R1-scheduler.md
- R2 (methodology lens) reviewing: spike 3 REPORT (§5 extrapolation, F8), ADR-0001, req tool change + tests, cross-doc consistency. -> reviews/phase0-rework-R2-methodology.md
- Next: remediate all findings + re-test -> sign ADR -> final gate checklist -> COMMIT/KILL.

## 2026-10-03 ~13:58 — R1 received (2 P0, 10 P1, 10 P2); remediation planned
- P0-1 (provenance): battery never re-run after the initFifo/O5 fixes; post-fix PASS numbers exist only in REPORT.md. FIX: re-running battery now (detached, logs backed up to logs-prefix-20261003; classes verified post-fix: built 10:58:53-54, no source newer).
- P0-2 (NIO slot accounting): N NIO-blocked goroutines at cap=N indefinitely postpone runnable goroutines — contradicts §7.2(ii) + GG-FAIR-0003. FIX: slot-releasing NIO wrapper design (extend GG-SCHED-0004 to NIO; ioEnter releases slot, ioExit re-acquires via wakeFifo) + cap-exhaustion starvation falsifier in prototype. To be implemented after R2 lands (consolidated pass).
- P1s noted for the same pass: (a) clamp cap ≤ carrier count / GOMAXPROCS>parallelism untested / pinning beyond cgo; (b) 10ms quantum never tested — add o1_q10ms run; (c) drainer liveness requirement; (d) SCHED-0002/WAKE-0003 dedup; (e) FAIR-0003 bound; (f) empty parent: fields.
- Process patch (escape protocol): Greenfield skill gets an explicit coordinator gate — verify (sources, logs) timestamp/checksum consistency before accepting any spike verdict; a number appearing only in REPORT.md is not evidence.

## 2026-10-03 ~14:10 — clean re-run CONTRADICTS spike-2b REPORT; rework agent spawned
- Re-run (post-fix sources, clean provenance): O1 12,036 samples (starvation eliminated, ok); O2/O3/O4/O5/stress/gomaxprocs PASS; quiescence@100us FAIL (never_scheduled=4535); quiescence@1ms FAIL (never_scheduled=168); overhead FAIL (41us mean vs 26.8us reported).
- REPORT F3 (never_scheduled=0) and F4 (26.8us) do NOT reproduce. Verdict now CONTESTED pending triage (real tail-starvation vs drain-window artifact).
- Spawned spike-2b rework agent (2f6ff192): triage quiescence + overhead with discriminating experiments; implement R1 P0-2 (slot-releasing NIO wrapper + cap-exhaustion falsifier); P1(a) cap clamp; P1(b) 10ms legs; full battery re-run with same-run SOURCES.sha256; updated REPORT.md; kill criteria armed.
- R2 (methodology) still running. Remediation of SPEC/registry/ADR findings waits for R2 + rework results (one consolidated pass).

## 2026-10-03 ~14:30 — R2 received; remediation in progress
- R2: spike-3 PASS STANDS; ADR committable after remediation. Findings:
  - P0 (registry scanner blind to namespaced IDs): FIXED — scan.go tokenRe/verifiesRe/sloppyRe now use shared idBody; idPattern/idSplitRe unified; new tests (TestScan_NamespacedIDs); go vet/test green; binary rebuilt; full-tree lint running.
  - P1-4 (PHASES §2 kill note; FEASIBILITY banner): FIXED both.
  - P1-1 (P50 vs P95): FIXED — §6 restated with P95-calibration trigger.
  - P1-2 (A4 re-test trigger): FIXED — concrete executable trigger.
  - P1-3 (F8 follow-up): FIXED — 05's R3 actually extended; SPEC §5.8 respdiff; §6.1 arg-shape battery.
  - P2s: P2-1 (vacuous rebase) FIXED; P2-4 (idPattern inconsistency) FIXED; P2-5 (TESTING.md momo paths) FIXED; P2-6 (ADR D5 falsifier) FIXED; P2-7 (SPEC-acceptance precondition) FIXED; P2-2/P2-3/P2-8 honesty caveats added to REPORT §8 item 8.
- R1 P1s on registry: P1(d) SCHED-0002 defers to WAKE-0003 FIXED; P1(e) FAIR-0003 bound (4xN admissions) FIXED; P1(f) parents=GG-0017 FIXED.
- SPEC §3.5 line 330 never_scheduled=0 claim is STALE pending 2b rework triage — final consistency pass after rework lands.
- Still running: 2b rework agent (quiescence triage, P0-2 slot-release, P1(a)(b)); full-tree req lint.
- Then: sign ADR, final gate checklist, COMMIT/KILL.

## 2026-10-03 ~14:45 — full-tree lint clean (70 reqs, 475 refs, EXIT 0). P0 fix verified.
- All R1/R2 remediation on the coordinator track is complete. Blocked on: 2b rework agent (quiescence triage, P0-2 slot-release, 10ms legs, REPORT update).
- After rework lands: final SPEC §3.5 consistency pass (stale never_scheduled=0) -> ADR sign -> gate checklist -> COMMIT/KILL.

## 2026-10-03 ~15:00 — ALL REMEDIATION COMPLETE. ADR coordinator-signed.
- 2b rework agent: quiescence = drain-window artifact (fixed via latch drain; F3 PASS, max/mean 1.05); overhead = VM contention (10ms fix MEASURED 0.30%); P0-2 slot-releasing NIO implemented + red-green validated; P1(a) cap clamp measured; P1(b) 10ms legs measured. Battery: 19 legs, 18 PASS, 1 expected FAIL (1ms overhead = fired trigger). Provenance: SOURCES.sha256, 28/28 clean.
- Coordinator track: SPEC §3.5 fully updated (P0-2 amendment, P1-2/P1-4/P1-5, P2-2/2-3/2-4/2-6/2-7/2-8/2-9, corrected F3/F4 numbers); registry updated (SCHED-0002 non-termination, SCHED-0004 slot-release, FAIR-0002 bound-only, WAKE-0002 model-check scope, GG-0057 FFM/file-I/O extension, FAIR-* parents to GG-0048); skill label registry extended ([ESTIMATE]/[UNVERIFIED]); ADR D5 current; ADR coordinator-signed.
- Process incident: truncated REPORT item-6 mid-sentence during list append; recovered original ending; AGENTS.md lesson added.
- Final: req lint clean (70 reqs, 494 refs); go test/vet green; [OPEN]=0 on go/no-go (1 actual [OPEN], explicitly non-go/no-go).
- VERDICT: COMMIT (Phase 1 go-ahead), contingent on Carlo's ADR signature.
