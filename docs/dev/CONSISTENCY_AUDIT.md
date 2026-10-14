# graalgo consistency audit — [CONSISTENCY_AUDIT.md](CONSISTENCY_AUDIT.md)

**Date:** 2026-10-03 · **Trigger:** Carlo: "Are the spec fully consistent and updated?"
**Scope:** [SPEC.md](SPEC.md) (1194 lines) cross-checked against [FEASIBILITY.md](FEASIBILITY.md), [PHASES.md](PHASES.md),
[adr/ADR-0001.md](adr/ADR-0001.md), [requirements/](../../requirements/) (70 `GG-*.yaml`), `[spikes/](../arch/spikes/)*/REPORT.md`, [reviews/](../arch/reviews/).
**Mode:** read-only. Findings ranked P0 (contradiction affecting a gate/decision/requirement),
P1 (stale text or wrong number), P2 (cosmetic).

> Ground truth: spike 02-loom KILLED (1:1 vthreads on Loom FJP scheduler + back-edge yields:
> 0 samples vs 200,000 stock Go). Spike 02b-userspace-scheduler PASSED — option C (1:1 vthreads
> retained, user-space scheduling: park-on-quantum-expiry, FIFO+runnext token queue,
> platform-thread drainer, in-flight cap = GOMAXPROCS, FJP as dumb pool) is the decided design
> (SPEC §3.5, ADR-0001).

Audit ran as 5 parallel tracks (A: SPEC internal · B: SPEC↔FEASIBILITY · C: SPEC↔PHASES/ADR ·
D: SPEC↔registry · E: SPEC↔spikes/reviews). Findings below are synthesized from all five.

---

## Findings

### Track B — SPEC ↔ [FEASIBILITY.md](FEASIBILITY.md) (staleness) — COMPLETE

**[P0] B1 — [FEASIBILITY.md](FEASIBILITY.md):26 (headline "Recommended architecture" paragraph)**
> "goroutines map 1:1 to virtual threads with mandatory back-edge safepoint yields (Loom never preempts)"
Stale: the second half is the killed mechanism. Under option C, back-edge yields are gone —
preemption is indefinite `LockSupport.park()` on quantum expiry. First half (1:1 mapping) still true.
Fix: replace "with mandatory back-edge safepoint yields (Loom never preempts)" with "with a
user-space quantum scheduler (option C: indefinite park-on-quantum-expiry; back-edge yields
superseded 2026-10-03, see ADR-0001 / SPEC §3.5; Loom never preempts)".

**[P0] B2 — [FEASIBILITY.md](FEASIBILITY.md):37–39 (hardest-problem #1)**
> "Go has M:N work-stealing with async preemption; Loom gives 1:1 virtual threads on carriers with
> *no* preemption. Faithful `GOMAXPROCS`, fairness, and preemption-equivalent behavior require
> back-edge safepoint yields throughout the Truffle AST plus a custom carrier pool — the single
> most semantics-sensitive piece of the runtime, and the one most likely to produce Heisenbugs
> that differential testing must catch."
Stale: both named mechanisms are dead (yields killed by spike 2; custom carrier pool doubly dead —
fairness is drainer-owned and SPEC §3.5 requires the *default* Loom scheduler, custom
`jdk.virtualThreadScheduler` unsupported). GOMAXPROCS is now the drainer's in-flight cap
= min(GOMAXPROCS, carrier parallelism) via hard assert.
Fix: rewrite mechanism as user-space scheduler (park-on-quantum-expiry, FIFO+runnext token queues,
platform-thread drainer, in-flight cap); tag "superseded 2026-10-03 by spike 2b / ADR-0001".
"Heisenbugs" tail stays.

**[P0] B3 — [FEASIBILITY.md](FEASIBILITY.md):140–141 (§4 recommended architecture, item 5)**
> "**Loom never preempts**: back-edge safepoint yields are mandatory in the Truffle AST;
> `GOMAXPROCS` maps to a custom carrier `ForkJoinPool`."
Stale: "mandatory" is false (yields gone; `Safepoint.poll()` at back-edges survives only as the
quantum-expiry trigger). "GOMAXPROCS maps to a custom carrier ForkJoinPool" false on both counts
(no custom pool; GOMAXPROCS = drainer in-flight cap).
Fix: replace with "**Loom never preempts**: `Safepoint.poll()` at back-edges triggers
quantum-expiry parks (`LockSupport.park()`; yields superseded 2026-10-03, ADR-0001);
`GOMAXPROCS` maps to the scheduler's in-flight cap = min(GOMAXPROCS, carrier parallelism) on the
default Loom carrier pool."

**[P1] B4 — amendment pointer exists but doesn't reach the stale text ([FEASIBILITY.md](FEASIBILITY.md))**
Pointer exists only at §8 risk #2 (lines 321–325, historical note pointing to SPEC §3.5) and
§11 ledger line 405 (strikethrough). The three stale spots (B1–B3: §1 headline, §1 hardest-problems,
§4 expansion — the most-read sections) carry no inline pointer; a reader of §1–§4 gets the wrong
architecture entirely; no top-of-document errata banner.
Fix: add errata banner under the header ("Erratum 2026-10-03: spike 2 KILLED the 1:1-yield-to-FJP
design; spike 2b PASS decided option C. Concurrency-mechanism text in §1 and §4(5) is superseded;
[SPEC.md](SPEC.md) §3.5 is normative; ADR-0001") and/or annotate B1–B3 inline.

Track B explicitly NOT stale (verified true under option C): 1:1 mapping claims (lines 26, 140);
"(Loom never preempts)"; netpoller deletion (GG-0018, refined via slot-releasing NIO wrapper);
§8 risk #2 body reads as history with accurate kill note; §11 line 405 strikethrough;
§10 open question #2 still OPEN/relevant; GOMAXPROCS=1 determinism (line 288); §5 safepoint-overhead
falsifier still relevant; Panama/-toolexec/Truffle modes/value model/panic-defer untouched.
Verdict: **[FEASIBILITY.md](FEASIBILITY.md) consistent with decided architecture: NO.**

### Track C — SPEC ↔ [PHASES.md](PHASES.md) / ADR-0001 — COMPLETE

**[P2] C1 — [adr/ADR-0001.md](adr/ADR-0001.md):52 (D2 "Consequence") wrong section citation**
> "Kill trigger ([PHASES.md](PHASES.md) §2): argv-interception gaps that force the fork *and* whose merge
> burden breaks the toil bound."
The argv-interception trigger is not named in [PHASES.md](PHASES.md) §2 (generic Phase-0 criteria only); it is
named in [PHASES.md](PHASES.md) §6 (Phase 4 kill criteria) and [SPEC.md](SPEC.md):241. Substance correct; pointer off.
Fix: cite `[PHASES.md](PHASES.md) §6` (or `SPEC §3.1`).

**[P2] C2 — [PHASES.md](PHASES.md):232 (§9 table, Phase 2 row) ambiguous parenthetical**
> "2 | Preemption-torture + pinning-audit suites (Loom-specific)"
Under option C fairness is drainer-owned, not Loom's ([PHASES.md](PHASES.md) §4 done-gate (2) itself says so).
"(Loom-specific)" invites the stale reading.
Fix: "Preemption-torture (user-space scheduler) + pinning-audit suites (JDK/Loom pinning events,
cgo-budget check)".

**Observation C-O1 (not a finding):** [SPEC.md](SPEC.md) contains no mention of Carlo's signature as the
Phase-0→1 gate or SPEC acceptance (ID lock) as the Phase-1 entry gate — those live only in
[adr/ADR-0001.md](adr/ADR-0001.md). SPEC's header says "Requirements: requirements/GG-0001–GG-0060 (draft,
unlocked)" — consistent with the draft-registry part. A one-line pointer in SPEC §1.2 to the ADR's
gate statement would close the loop.

**Observation C-O2:** Phase-2 re-estimate arithmetic verified, not hand-waving: baseline M:N
re-scope counterfactual 7–12 PY built component-by-component (3–5.5 + 1.5–2.5 + 1–1.5 + 0.5–1 +
1–1.5 ✔); 0.60–0.70 × (7–12) = 4.2–8.4 ≈ 4–8 PY ✔; "30–40% below" = 1 − 0.60/0.70 ✔.
Doc itself discloses the baseline was never independently estimated and everything is [ESTIMATE]
at LOW confidence (±50%).

Track C clean checks: [PHASES.md](PHASES.md) reflects option C in all phases 0–5 (killed design struck through
with kill recorded at §2:29; M:N re-scope marked "not pursued"; Phase 2 §4:84–87 names option C
with scope tokens/drainer/cap/F5 class; only remaining "yield" at §9:242 is a correct reference
to the old scope's cost component in the counterfactual). All four ADR D5 residuals carried in
SPEC §3.5 with matching numbers/triggers (overhead 29.9µs = 0.30% at 10ms, SPEC:426–432;
38µs wakeup + server-hardware re-test + risk-row-10 kill, SPEC:432–438; 13× gap no-production-claims,
SPEC:439–444; slot-releasing NIO wrapper + Phase-2 throughput trigger, SPEC:405–414;
never_scheduled=0, SPEC:337–339). Kill triggers match SPEC §8 row 2 verbatim. Effort tables
SPEC §9 ↔ PHASES §10 agree on every row; done-gates consistent. Verdicts: **[PHASES.md](PHASES.md) reflects
option C: YES; ADR-0001 ↔ SPEC consistent: YES.**

### Track D — SPEC ↔ requirements registry — COMPLETE

**[P2] D1 — GG-SCHED-0003 weaker than SPEC's effective-cap rule (no contradiction)**
[requirements/GG-SCHED-0003.yaml:3](../../requirements/GG-SCHED-0003.yaml): "The number of admitted-but-not-yet-parked virtual threads
SHALL never exceed GOMAXPROCS; the drainer SHALL enforce this as a hard assert."
SPEC §3.5 (~:352): effective cap is `min(GOMAXPROCS, carrier parallelism)`, clamped in the
constructor. `min(GOMAXPROCS, carrier)` ≤ GOMAXPROCS, so no contradiction — but the requirement
doesn't pin the tighter formula SPEC treats as the mechanism.
Fix: revise GG-SCHED-0003's statement (keep the ID) to "…SHALL never exceed
`min(GOMAXPROCS, carrier parallelism)`…".

**[P2] D2 (informational)** — the audit's stated ID pattern `GG-[A-Z]+-[0-9]+` covers only 10/70
files (scheduler families); the other 60 are `GG-0001`–`GG-0060`. Both schemes audited. Use
`GG-(?:[A-Z]+-)?[0-9]+` in future instructions.

Track D clean checks: 70 files; 70 distinct GG-IDs cited in SPEC ↔ exact bijection with files,
0 missing, 0 orphans; Appendix B rows = 70, titles match yaml titles verbatim; 0 dangling §N /
parent / GG-ID refs; **0 stale killed-design hits** — all mechanism text is option C (e.g.
GG-0017: "Loom ForkJoinPool SHALL be treated as an untrusted thread pool with no scheduling
discipline assumed"; GG-SCHED-0001: park + resume token; GG-WAKE-0003: wakeFifo/initFifo lanes),
with explicit kill history notes (GG-0017:11 "revised for option C (spike 2b PASS); yield-to-FJP
design removed"); no mutual-exclusion between requirements or vs SPEC §3.5 (quantum 10ms, 2x
fairness bound vs measured 1.05, 1% overhead budget vs measured 0.30% — all consistent); 0
duplicate IDs; all 70 `status: draft`, none superseded — consistent with SPEC acceptance/ID lock
still pending. Verdict: **registry consistent with SPEC + option C: YES.**

### Track A — SPEC internal consistency — COMPLETE

Check 1 PASSED CLEAN: no surviving FJP-reliance or yield-as-fairness text. Every "yield" mention
(SPEC:299,301,303,320,445,1050,1078) is a historical reference to the killed spike-2 design or the
explicit repudiation at SPEC:320 ("back-edge *yields* are gone, parking is the preemption primitive
`[DECIDED]`"). §3.5: FJP "demoted to an **untrusted thread pool** — no scheduling discipline is
assumed from it" (SPEC:298–299). The option-C narrative is coherent.

**Systematic defect (checks 2): the SPEC rework copied numbers from the superseded old draft,
not the archived rework battery.**

**[P1] A1 — SPEC:347** `"observed drainer utilization 1.34% at 10,000 goroutines [VERIFIED]"`.
REPORT F3: drainer util 1.91% (confirmed `logs/quiescence_10k.log`: `drainerUtilPct=1.91`);
"1.34%" occurs nowhere in the REPORT. Fix: `1.91%`.

**[P1] A2 — SPEC:367** `"50,558 parks / 50,558 admissions, 40,617 of 40,617 wakeups won, zero lost
[VERIFIED]"`. REPORT F6: `parks=48140, admissions=48140`; `wakeupCalls=40588, wakeupWon=40588`
(confirmed `logs/stress.log`). Neither 50,558 nor 40,617 occurs in the current REPORT.
Fix: `48,140 parks / 48,140 admissions, 40,588 of 40,588 wakeups won, zero lost`.

**[P1] A3 — SPEC:434** `"15,603 samples/5s vs stock Go's 200,000 (13×)"`. REPORT F1 explicitly
marks 15,603 as the superseded old draft ("On the old draft's 15,603 vs the coordinator's 12,036
vs this run's 11,824 (§3.6): machine variance under VM contention"); archived legs: 11,824 /
13,337 / 13,919. 200,000 traces to [spikes/02-loom/REPORT.md](../arch/spikes/02-loom/REPORT.md). 200,000/11,824 ≈ 16.9×. The REPORT's
own "~13x" (line 102) is itself derived from stale 15,603.
Fix: `"11,824 samples/5s vs stock Go's 200,000 (~17×)"`; fix REPORT:102 at the source.

**[P1] A4 — SPEC:434** `"RTT p50 37µs vs <1µs"`. The REPORT never quotes a 2b RTT number; archived
O1 logs give rtt_ns p50 = 56,104 / 55,453 / 63,615 / 60,391 / 276,184 ns — none is 37µs. "<1µs"
traces to spike-2's stock-Go RTT p50 771ns. Fix: drop "37µs" or cite archived per-leg RTT p50s
with configs.

**[P1] A5 — SPEC:323–324, SPEC:423–425** `"mean 27–41µs/hop (run-to-run range on the contended
2-vCPU VM) ... (p50 5–10µs, at/under)"`. 27µs is the rounded superseded old-draft mean (26.8);
REPORT's honest restatement (§3.6): "26.8 → 41 → 33-70 us wander"; archived battery: 33.0µs mean
/ 10.0µs p50 (1ms), 29.9µs / 10.0µs (10ms), p50 "pinned at ~10 us" post-fix.
Fix: state current battery numbers; describe variance as contention-driven.

**[P1] A6 — SPEC:1000 (risk-row 10)** `"(prototype: 26.8µs mean/hop measured on the 2-vCPU VM)"`.
26.8µs is the old draft's number, explicitly superseded.
Fix: `"(prototype: 33.0µs mean/hop at 1ms quanta on the contended 2-vCPU VM; 29.9µs at 10ms)"`.

**[P1] A7 — SPEC:1051 (provenance ledger)** `"Spike 2b (option C): PASS — 15,603 vs 0 samples,
in-flight cap assert never tripped, 50,558/50,558 wakeups, scoped fixes (initFifo, 10ms
quantum)"`. Per A2/A3: should read `"11,824 vs 0 samples ... 48,140/48,140 wakeups"`. (PASS
verdict stands.)

**[P1] A8 — SPEC:1079–1081 (verdict-scope ledger)** `"Spike-2b fairness (15,603 samples/5s,
CV 0.0123)"`; `"Spike-2b overhead (26.8µs mean/hop)"`; `"Spike-2b lost-wakeup freedom
(50,558/50,558)"`. Current F1 CVs: 0.0093/0.0348/0.0305/0.0101/0.0016 — 0.0123 occurs nowhere;
26.8µs → 33.0; 50,558 → 48,140. Fix accordingly.

**[P1] A9 — SPEC:302–303, SPEC:1078** scope note `"(scoped: JDK 25.0.4, 2-vCPU VM, 1ms/100µs
quanta)"` on the spike-2 kill. `[spikes/02-loom/REPORT.md](../arch/spikes/02-loom/REPORT.md)` tested POLL (10 ms) and POLL (1 ms
watchdog); 1ms/100µs are spike-2b's O1 legs, not spike-2's configs. JDK/VM parts check out.
Fix: `"(scoped: JDK 25.0.4, 2-vCPU VM, POLL 10ms / 1ms-watchdog configs)"`.

**[P1] A10 — SPEC:513, :916, :1063** cite `"§8.2"` — §8 is a single 13-row table with no
subsections; "the six §8.2 experiments" = rows 8–13. Fix: "rows 8–13 of the §8 table".
(:1063's "§8.2 rewritten around option C" is additionally ambiguous — row 2 is the option-C row;
disambiguate.)

**[P2] A11 — SPEC:1187** — 7 literal `[OPEN]` occurrences; exactly one substantive marker at
:1187 (Appendix **C**, GraalVM/JDK distribution pin, explicitly non-go/no-go); other six are prose
mentions (legend :6; "zero [OPEN]" :43, :59, :844; glossary :1105; :1190's "the single [OPEN] in
this appendix"). Zero TODO/FIXME/TBD/placeholder/XXX. BUT: the SPEC has **no Appendix D** — the
pin is in Appendix C; the rework's "Appendix D" locator (used in the done-gate record) is wrong.
The SPEC itself is internally consistent (:1190 names "this appendix"). Fix: correct the gate
record to "Appendix C".

**[P2] A12 — Appendix A glossary** says a go/no-go item "must end
`[DECIDED]`/`[VERIFIED]`/`[HIST]`, never `[OPEN]`", but none of the 13 §8 risk rows carries a
terminal evidence label. Fix: add terminal labels to §8 rows or narrow the glossary rule.

**[P2] A13 — Appendix B** lists GG-0057's "Primary spec home" as §8, but the substantive
normative content (pinning budget, cgo off-pool, FFM/file-I/O extension) lives in §3.5.
Fix: home "§3.5, §8".

**[P2] A14 — §3.5 pinning paragraph**: `"the FJP's queue discipline re-engages (mildly) —
asserted safe, not shown"`. A safety assertion about an untested edge case inside a `[DECIDED]`
section (self-flagged `[UNVERIFIED]` next sentence — not a contradiction, but "asserted safe"
overstates). Fix: "hypothesized safe; unmeasured until the Phase-2 pinning matrix".

Track A clean checks: cross-references otherwise resolve (all 70 GG-* IDs → Appendix B; all spike
citations → existing dirs; all §X/Appendix X refs resolve; research T1–T8 and "T5 §2.3"/"T5 §4.4"
resolve under the section.item convention; [PHASES.md](PHASES.md) §2/§3/§11 and [FEASIBILITY.md](FEASIBILITY.md) §5/§11 exist;
no literal "ADR-0001" citation in SPEC — only "the companion ADR" — and [adr/ADR-0001.md](adr/ADR-0001.md) exists).
Traceable numbers: 200,000; 0 samples; 2,643; never_scheduled=0; 1.05; 29.9µs/0.30%; 38µs/4.3µs/5µs;
derived 2.7–4.1% and 0.38% arithmetically consistent.
**Not traceable to the current REPORT: 15,603, CV 0.0123, 26.8µs, 50,558, 40,617, 1.34%, RTT p50
37µs.**
Side note: this audit's own brief listed "quanta max/mean 1.01" — both SPEC and REPORT say
**1.05**; 1.01 appears in neither (brief error, not a SPEC defect).
Verdict: **[SPEC.md](SPEC.md) internally consistent: NO** — superseded old-draft numbers presented as
`[VERIFIED]`, misattributed spike-2 configs, non-existent §8.2 cited 3×. The option-C narrative is
coherent; no verdict or `[DECIDED]` item changes (spike-2b PASS, 10ms-quantum decision on the
current 29.9µs = 0.30%, kill triggers unaffected).

### Track E — SPEC ↔ spike reports / peer reviews — COMPLETE

**[P1] E1 — spike 6's verdict (COMMIT-DIRECT) missing; SPEC presents it as still-open**
[SPEC.md](SPEC.md):846–854 (§7.1): "the fork is decided by the Phase-0 scored spike verdict (spike 6), which
must produce a *scored comparison* … not an assumption `[DECIDED]`. The default remains the direct
Truffle AST `[PROPOSAL]`. If Sulong wins the comparison **and** the direct-AST estimate doubles,
the program replans… **Falsifier of the default:** spike 6 scores Sulong higher… (GG-0047)".
Same stale framing at [SPEC.md](SPEC.md):266 (§3.3) and [SPEC.md](SPEC.md):996 (§8 row 6, experiment in pending tense).
REPORT [spikes/06-sulong/REPORT.md:3–5](spikes/06-sulong/REPORT.md): "**Verdict: COMMIT-DIRECT** — proceed with the direct
Truffle AST route. … Sulong does not win the scored comparison (6 vs 19), so the [PHASES.md](PHASES.md) REPLAN
trigger (Sulong wins AND direct-AST estimate doubles) is not met." §6 closes the option unless (a)
Sulong's bitcode support reaches current LLVM, (b) a maintained full-language Go→LLVM frontend
appears, (c) Sulong gains precise-GC/threading/signal primitives. Root cause: Sulong ceiling
bitcode 4.0–12.0.1 vs TinyGo emitting LLVM 22.
Fix: rewrite §7.1 to record the landed verdict (COMMIT-DIRECT, 6 vs 19, root cause) with the three
re-test triggers; §8 row 6 → "completed 2026-10-03: Sulong 6 vs direct 19; replan trigger not met";
§3.3 falsifier → past tense.

**[P1→dedupe] E2** — old-draft 02b numbers cited as `[VERIFIED]` at SPEC:1051, §3.5, :1079–1081,
§8 row 10: same defect as A1–A8 above (superseded old draft vs archived rework battery). Track E
independently confirms: REPORT states "Every number below is from those archived logs. The old
draft's terminal-only numbers are superseded and marked as such where they differ." Archived
battery: F1 O1 = 11,824 / 7,014 / 13,919 / 13,337 / 11,786, CVs
0.0093/0.0348/0.0305/0.0101/0.0016; F3 never_scheduled=0, max/mean 1.05; F4 33.0µs mean @1ms /
29.9µs @10ms (0.30%); F6 parks=48140/admissions=48140, wakeupWon=40588. Fixes per A1–A8.
**[P1→dedupe] E3** — spike-2 kill scope "1ms/100µs quanta": same as A9. REPORT §2: OFF / POLL
(10 ms watchdog) / POLL (1 ms watchdog) / UNCOND / PARK (1µs–1ms timed park); quanta are a 2b
concept. Fix per A9.

**[P2] E4 — spike-2 WHY omits the report's root causes; ledger drops re-test triggers**
[SPEC.md](SPEC.md):299–302 WHY = "systematic starvation the yields could not close" — nothing else. REPORT
§3 (verified in JDK 25 sources): Loom carrier is an FJP with LIFO local queues and no runnext
equivalent; no public API to install a custom carrier scheduler (`Thread.ofVirtual().scheduler`
is package-private); carrier sizing is per-JVM via system properties only (GOMAXPROCS fidelity a
launcher concern); 7 workaround families failed (FAIRNESS_SEARCH.md). REPORT §6 re-test triggers:
(a) public per-builder vthread scheduler API; (b) carrier queue-discipline change for yielded
continuations; (c) JDK-change-free workaround demonstrated (platform-thread unblocker tried,
failed). Kill itself is correctly recorded everywhere; the causal detail the ledger exists to
preserve is absent.
Fix: record the four root-cause elements in §3.5's kill paragraph or the §10.2 row; carry (a)/(b)/(c)
in the ledger annotated "superseded by option C as a program decision".

**[P2] E5 — §10.1 provenance ledger has no verdict rows for spikes 1, 3, 4, 5, 6, 7**
[SPEC.md](SPEC.md):1049–1051 records verdicts only for spike 2 (KILLED) and 2b (PASS); spikes 1/3/4/5/7 cited
only as falsifiers with no verdict stated; spike 6 missing entirely (E1). §8 rows 1–7 describe
falsifying experiments in pending tense though all seven spikes have landed.
Fix: add verdict rows to §10.1 for all 7 spikes (verdict + one-line evidence pointer); mark §8
rows 1–7 completed with verdicts.

**[P2] E6 — R2 P1-4 only partially remediated (stale-doc supersession)**
R2 P1-4 demanded: mark PHASES §2 spike 2 KILLED ✓ (`[PHASES.md](PHASES.md):30`); FEASIBILITY header banner;
one-line notes at §4(5) and §8-risk-2 ✓ (:321–326, :405); align agent-leverage `[OPEN]`s with SPEC
§7.14 `[DECIDED]`. Missing: `[FEASIBILITY.md](FEASIBILITY.md):1–6` header — **no banner** (fix asked for "Historical
study record as of 2026-10-03 pre-spike-2b. [SPEC.md](SPEC.md) §1.1 is the normative record; …");
`[FEASIBILITY.md](FEASIBILITY.md):390` (§9 #14) still "`14. [OPEN] Agent-leverage discount on the 20–40 PY
estimate — quantify or drop.`"; `[PHASES.md](PHASES.md):236` (§10) still "No agent-leverage discount applied
(`[OPEN]` — quantify or drop)."
Fix: add the header banner; align both agent-leverage items with SPEC §7.14 `[DECIDED]`
(discount dropped).

**[P2] E7 — R2 P1-3 partial: GG-0035's statement lacks the respdiff line**
R2 P1-3's fix: extend 05's R3 ✓ ([spikes/05-cmdgo/REPORT.md:238–246](spikes/05-cmdgo/REPORT.md)) + watch in SPEC §5.8 tool
list ✓ (:713) and §6.1 checklist ✓ (:768) **plus a line in GG-0035's statement**. Missing: the
GG-0035 line — [requirements/GG-0035.yaml](../../requirements/GG-0035.yaml) statement still lists "apidiff, go/types signature
diff, rtlink, flagdiff, godebug-diff" with no respdiff.
Fix: add respdiff to GG-0035's statement (one line, ID kept).

**[P2] E8 — SPEC omits spike 3's P95-calibration trigger (R2 P1-1 remediation)**
`[spikes/03-versiontax/REPORT.md](spikes/03-versiontax/REPORT.md):§6` honesty note: the table compares extrapolated P50 against a
P95 bound — category mismatch; heavy-class P95 likely over the bound. **Trigger:** the first real
upgrade SHALL measure the per-component toil distribution and recalibrate; if measured P95
breaches ≤200 agent-h, the toil-reduction sprint (SPEC §6.5) fires before the next release.
[SPEC.md](SPEC.md):117–119 (§2.3) promotes the bound `[PROPOSAL]`→`[DECIDED]` and [SPEC.md](SPEC.md):801 (§6.4) covers
toil accounting, but neither the trigger nor the "no breach observed, not bound compliance"
qualification appears.
Fix: add the P95-calibration trigger to §6.4 or the §10.2 verdict-scope ledger.

**[P2] E9 (informational) — two option-C additions not in the 02b REPORT**
(a) SPEC §3.5: "graalgo requires the default Loom carrier scheduler; custom
`jdk.virtualThreadScheduler` settings are unsupported `[DECIDED]`" — not in the 02b report; is
R1 P2-6's suggested sentence adopted verbatim (review-driven, plausibly intentional).
(b) SPEC §3.5: "`Safepoint.poll()` at loop back-edges **and function entries**" — 02b report §1.2
says back-edges only.
Fix: confirm both additions are intentional (they read as deliberate spec-level decisions, not
drift). No verdict impact either way.

Track E clean checks: verdicts for spikes 1/2/2b/3/4/5/7 cited correctly (no KILL cited as PASS;
no citations to nonexistent spikes); spike-2 scoping sentence ("indicts the yield-to-FJP design,
not 1:1 vthreads as the execution vehicle") matches the report; option-C mechanism matches the
report on all five elements (park-on-quantum-expiry; runFifo/wakeFifo/initFifo lanes; single
drainer; in-flight cap = GOMAXPROCS hard assert; FJP demoted); slot-releasing NIO, the
min(GOMAXPROCS, carrier) clamp, 10ms quantum at 29.9µs = 0.30%, drainer non-termination all match
the rework report. **All R1 findings traceable** (P0-1 archived-provenance battery; P0-2
slot-releasing wrapper + red-green CapExhaustion; P1-1…P1-10 each to SPEC §3.5 or requirement
files). **R2 traceable except E6/E7/E8.** Spike-3 bound citation accurate (P95 ≤200 agent-h, ≤10
days; PASS, trigger not fired). Spike-7 citation accurate (45/45 SAME, 9/9 mutation classes
DETECTED, K1–K3 not fired; gc-vs-gc scope since graalgo doesn't exist yet).

---

## Bottom line

**consistent and updated: NO.**

Finding counts (deduplicated): **P0 = 3 · P1 = 11 · P2 = 14** — 28 total.

- P0 (all Track B, [FEASIBILITY.md](FEASIBILITY.md)): B1 (headline recommendation still "mandatory back-edge
  safepoint yields"), B2 (hardest-problem #1: yields + custom carrier pool), B3 (§4(5):
  "GOMAXPROCS maps to a custom carrier ForkJoinPool") — the study's three most-read concurrency
  statements present the killed design as the recommendation with no amendment pointer.
- P1 (11): A1–A10 (superseded old-draft 2b numbers cited as `[VERIFIED]` in §3.5/§8/§10;
  spike-2 configs misattributed; non-existent "§8.2" cited 3×) + E1 (spike 6 COMMIT-DIRECT
  verdict absent, decision framed as pending in §7.1/§3.3/§8-row-6). E2/E3 duplicate A-findings.
- P2 (14): A11–A14, C1–C2, D1–D2, E4–E9.

What is NOT broken: the option-C design narrative in SPEC §3.5 is coherent and internally
consistent; the registry (70 GG IDs ↔ SPEC) is an exact bijection with zero stale killed-design
text; [PHASES.md](PHASES.md) reflects option C in all phases with kill recorded; ADR-0001 ↔ SPEC residuals
and kill triggers match verbatim; every spike verdict except spike 6's is cited correctly; all R1
review findings and all but three R2 items are traceably remediated. No verdict or `[DECIDED]`
item changes as a result of this audit — spike-2b PASS, the 10ms-quantum decision (rests on the
current 29.9µs = 0.30%), and all kill triggers are unaffected.

### Minimal fix list to reach YES

1. [FEASIBILITY.md](FEASIBILITY.md): add top-of-document errata banner ("spike 2 KILLED the 1:1-yield-to-FJP
   design 2026-10-03; spike 2b PASS decided option C; SPEC §3.5 is normative"); rewrite or inline-
   annotate the three stale concurrency spots (B1–B3: :26, :37–39, :140–141). [P0]
2. [SPEC.md](SPEC.md): replace every superseded old-draft 2b number with the archived rework-battery
   numbers — §3.5, §8 row 10, §10.1, §10.2 ledger: 15,603→11,824 (O1 legs 7,014–13,919);
   26.8µs mean→33.0µs @1ms / 29.9µs @10ms; 50,558/50,558→48,140/48,140;
   40,617/40,617→40,588/40,588; CV 0.0123→archived F1 CVs; 1.34%→1.91%; RTT p50 37µs→archived
   per-leg values (55–64µs) or drop; "~13×"→"~17×". Also fix `[spikes/02b-userspace-scheduler/](spikes/02b-userspace-scheduler/)
   REPORT.md:102` "~13x" at the source (derived from the stale 15,603). [P1]
3. [SPEC.md](SPEC.md): fix spike-2 kill scope to the tested configs (POLL 10ms / 1ms-watchdog / PARK
   1µs–1ms; JDK 25.0.4, 2-vCPU VM) — not "1ms/100µs quanta". [P1]
4. [SPEC.md](SPEC.md): "§8.2" → "§8 rows 8–13" (3 locations: :513, :916, :1063). [P1]
5. [SPEC.md](SPEC.md): record spike 6's COMMIT-DIRECT verdict (6 vs 19; root cause TinyGo LLVM 22 vs Sulong
   ≤12.0.1; re-test triggers a/b/c) at §7.1, §3.3, §8 row 6; add §10.1 verdict rows for spikes
   1, 3, 4, 5, 6, 7; mark §8 rows 1–7 completed. [P1]
6. [SPEC.md](SPEC.md) done-gate record: "Appendix D" → "Appendix C" (the single [OPEN] lives at :1187,
   Appendix C). [P2]
7. [FEASIBILITY.md](FEASIBILITY.md): add the R2-mandated header banner; align the two agent-leverage `[OPEN]`s
   (`[FEASIBILITY.md](FEASIBILITY.md):390`, `[PHASES.md](PHASES.md):236`) with SPEC §7.14 `[DECIDED]` (discount dropped). [P2]
8. [requirements/GG-0035.yaml](../../requirements/GG-0035.yaml): add respdiff to the statement (keep the ID). [P2]
9. [SPEC.md](SPEC.md): add spike 3's P95-calibration trigger to §6.4 or the §10.2 verdict-scope ledger. [P2]
10. [SPEC.md](SPEC.md) §3.5/§10.2: record spike-2's four root causes (LIFO/no-runnext, no public scheduler
    API, JVM-global carrier sizing, 7 workaround families) and carry the (a)/(b)/(c) re-test
    triggers annotated "superseded by option C as a program decision". [P2]
11. P2 sweep: GG-SCHED-0003 pin `min(GOMAXPROCS, carrier parallelism)` (D1); ADR:52 citation
    → [PHASES.md](PHASES.md) §6 (C1); [PHASES.md](PHASES.md):232 "(Loom-specific)" reword (C2); Appendix A glossary vs §8
    labels (A12); GG-0057 primary home "§3.5, §8" (A13); "asserted safe" → "hypothesized safe;
    unmeasured until the Phase-2 pinning matrix" (A14); confirm E9's two additions intentional.

### Process observations (for the program, not the docs)

- The SPEC rework copied numbers from the **superseded same-day draft**, not the archived rework
  battery — even though the REPORT explicitly marks the old numbers superseded and the rework
  battery was the coordinator-verified artifact. The greenfield skill's coordinator-verification
  gate ("PASS numbers must trace to archived checksummed logs") covered the REPORT but not the
  downstream SPEC transcription. Recommended patch: every number the SPEC presents as
  `[VERIFIED]` must cite its archived log line (the REPORT's own provenance rule), enforced by
  the same lint that checks evidence labels.
- This audit's own brief carried the superseded numbers (15,603; 0.0123; 26.8µs; 50,558; 1.01) —
  the rework's number supersession did not propagate to the coordinator's summary. A
  single "current numbers" table in the spike REPORT's §1 (or a `numbers.lock` file) would give
  every downstream consumer one place to copy from.
- Two R2-mandated fixes (FEASIBILITY banner, agent-leverage alignment) and one R2-mandated
  requirement line (GG-0035 respdiff) were reported "remediated" but are absent — the
  remediation check was scoped to [SPEC.md](SPEC.md) and missed the named files. Remediation verification
  should grep the *named targets*, not the *usual* target.

---

## REMEDIATION 2026-10-03 (fix pass, read-write)

Applied by the remediation subagent. Every fix below was re-verified by grep
against the named target (the process gap this audit flagged). Numbers were
re-verified against the archived logs themselves before copying (F1 legs,
`drainerUtilPct=1.91` in `logs/quiescence_10k.log`, F6 in `logs/stress.log`,
F4 in `logs/overhead.log`/`overhead_q10ms.log`), not just against the audit's
quotes. Evidence labels per the greenfield skill: `[VERIFIED]` only where a
log line is cited in-text.

**Fixed: 27 of 28. Open: 0. Not-applicable: 1.**

### P0 — all FIXED

- **B1** — FIXED: [FEASIBILITY.md](FEASIBILITY.md) headline now reads "…with a user-space quantum
  scheduler (option C: indefinite park-on-quantum-expiry; back-edge *yields*
  superseded 2026-10-03 — see [SPEC.md](SPEC.md) §3.5 / ADR-0001; Loom never preempts)".
- **B2** — FIXED: hardest-problem #1 rewritten around the user-space scheduler
  (park-on-quantum-expiry, FIFO+runnext token queues, platform-thread drainer,
  in-flight cap = min(GOMAXPROCS, carrier parallelism)), tagged "superseded
  2026-10-03 by spike 2b / ADR-0001"; "Heisenbugs" tail kept; label now
  `[DECIDED]`.
- **B3** — FIXED: §4(5) now reads "`Safepoint.poll()` at back-edges (and
  function entries) triggers quantum-expiry parks … the yield-based mechanism
  was superseded 2026-10-03 by spike 2b / ADR-0001); `GOMAXPROCS` maps to the
  scheduler's in-flight cap = min(GOMAXPROCS, carrier parallelism) on the
  default Loom carrier pool (no custom carrier pool)".
- **B4** — FIXED: top-of-document errata banner added under the header
  (spike-2 kill, spike-2b PASS/option C, SPEC §3.5 normative, ADR-0001;
  "Historical study record as of 2026-10-03 pre-spike-2b; [SPEC.md](SPEC.md) §1.1 is the
  normative record").

### P1 — all FIXED

- **A1** — FIXED: SPEC §3.5 drainer utilization 1.34% → **1.91%** with the
  archived log line cited (`logs/quiescence_10k.log`: `drainerUtilPct=1.91`).
- **A2** — FIXED: 50,558/50,558, 40,617/40,617 → **48,140/48,140 parks/admissions,
  40,588/40,588 wakeups won** with the archived line cited
  (`logs/stress.log`: `parks=48140 admissions=48140 wakeupCalls=40588 wakeupWon=40588`).
- **A3** — FIXED: 15,603 samples/5s (13×) → **11,824 samples/5s (~17×)**
  with the archived leg cited; [spikes/02b-userspace-scheduler/REPORT.md:102](spikes/02b-userspace-scheduler/REPORT.md)
  fixed at the source ("~13x" → "~17x (200,000/11,824 on the archived 1 ms leg)").
- **A4** — FIXED: RTT p50 "37µs" → **~55–64µs** with the archived per-leg
  values noted (one noisier leg 276µs, contention-driven).
- **A5** — FIXED (both sites): 27–41µs/hop range → **33.0µs mean / 10.0µs p50
  at 1ms quanta** (3.3% of quantum) with `logs/overhead.log` cited; the old
  draft's 26.8µs is named as superseded with the report's
  26.8→41→33–70µs wander.
- **A6** — FIXED: §8 row 10 parenthetical → "(prototype: 33.0µs mean/hop at 1ms
  quanta on the contended 2-vCPU VM; 29.9µs at 10ms)".
- **A7** — FIXED: §10.1 spike-2b row → "11,824 vs 0 samples … 48,140/48,140
  parks/admissions, 40,588/40,588 wakeups won".
- **A8** — FIXED: verdict-scope rows → "11,824 samples/5s at 1ms; 13,337 at 10ms;
  CV 0.0016–0.0348 across legs"; "33.0µs mean/hop at 1ms; 29.9µs at 10ms =
  0.30%"; "48,140/48,140 parks/admissions; 40,588/40,588 wakeups won".
- **A9** — FIXED (both sites): spike-2 kill scope → "JDK 25.0.4, 2-vCPU VM,
  OFF / POLL-10ms / POLL-1ms-watchdog / UNCOND / PARK 1µs–1ms configs"
  (per [spikes/02-loom/REPORT.md](../arch/spikes/02-loom/REPORT.md) §2).
- **A10** — FIXED (3 sites): "§8.2" → "§8 rows 8–13" (:513, :916); the §10.1
  ledger row → "§8 row 2" (:1063).
- **E1** — FIXED: spike 6's COMMIT-DIRECT verdict recorded at §7.1 (rewritten:
  6 vs 19, root cause LLVM 22 vs Sulong ≤12.0.1, re-test triggers (a)/(b)/(c)),
  §3.3 (falsifier → past tense), §8 row 6 (completed). Default is now the
  direct Truffle AST, full stop.
- **E2/E3** — dedupe of A1–A9; fixed via the same edits.

### P2 — FIXED except one not-applicable

- **A11** — NOT-APPLICABLE: the string "Appendix D" occurs in **zero** program
  files ([SPEC.md](SPEC.md), [PHASES.md](PHASES.md), [FEASIBILITY.md](FEASIBILITY.md), [reviews/](../arch/reviews/), [adr/](adr/), [requirements/](../../requirements/)
  all grepped) — the audit's claim about the done-gate record could not be
  located, so there is nothing to correct. The single `[OPEN]` is at
  SPEC:1187 in Appendix C and the SPEC is internally consistent about it.
- **A12** — FIXED: Appendix A glossary narrowed — "(§8 risk rows carry their
  resolution in the §10.1 provenance ledger rather than a terminal label.)"
- **A13** — FIXED: Appendix B GG-0057 primary home → "§3.5, §8".
- **A14** — FIXED: "asserted safe, not shown" → "hypothesized safe;
  unmeasured until the Phase-2 pinning matrix".
- **C1** — FIXED: ADR-0001 D2 "Kill trigger ([PHASES.md](PHASES.md) §2)" → "([PHASES.md](PHASES.md) §6)"
  (pointer correction only; no decision text changed).
- **C2** — FIXED: [PHASES.md](PHASES.md) §9 row 2 → "Preemption-torture (user-space
  scheduler) + pinning-audit suites (JDK/Loom pinning events, cgo-budget check)".
- **D1** — FIXED: GG-SCHED-0003 statement pins
  `min(GOMAXPROCS, carrier parallelism)` (ID kept; history line added).
- **D2** — informational; no fix (audit's own brief pattern corrected in the
  handoff).
- **E4** — FIXED: spike-2 root causes recorded in SPEC §3.5's kill paragraph
  (four elements, with the LIFO→FIFO correction sourced to FAIRNESS_SEARCH.md)
  and re-test triggers (a)/(b)/(c) carried in §10.2 annotated "superseded by
  option C as a program decision".
- **E5** — FIXED: §10.1 gained verdict rows for spikes 1, 3, 4, 5, 6, 7
  (verdict + one-line evidence pointer each); §8 rows 1, 3, 4, 5, 7 marked
  "completed 2026-10-03: PASS" (rows 2 and 6 already carried their verdicts).
- **E6** — FIXED: [FEASIBILITY.md](FEASIBILITY.md) header banner (doubles as the R2-mandated
  banner); [FEASIBILITY.md](FEASIBILITY.md) §9 #14 and [PHASES.md](PHASES.md) §10 agent-leverage items
  aligned with SPEC §7.14 `[DECIDED]` (discount dropped); additionally fixed
  the §1 estimate paragraph's "currently `[OPEN]`" (same family, unaudited).
- **E7** — FIXED: GG-0035 statement gains respdiff (ID kept; history line added).
- **E8** — FIXED: spike-3 P95-calibration trigger added to SPEC §6.4
  (honesty note: dry-run compared extrapolated P50 against a P95 bound —
  category mismatch; first real upgrade recalibrates; breach fires the
  §6.5 toil-reduction sprint).
- **E9** — confirmed INTENTIONAL: the two option-C additions (default-scheduler
  requirement, function-entry safepoints) are deliberate spec-level decisions
  beyond the 02b report; SPEC kept as-is.

### Verification

- Stale-number sweep over [SPEC.md](SPEC.md): `15,603`, `0.0123`, `50,558`, `40,617`,
  `1.34%`, `37µs`, `§8.2`, `Appendix D`, `1ms/100µs quanta` — **zero hits**.
  `26.8µs` survives only in the historical-supersession note ("the old draft's
  26.8µs is superseded").
- All 70 requirement yamls parse; IDs unique; the two edited files carry
  history lines; no IDs changed.
- The §10 "open questions" `[OPEN]`s remaining in [FEASIBILITY.md](../arch/FEASIBILITY.md) are the
  historical study record's own list — left intact per the audit's clean
  check; the errata banner points readers to SPEC §1.1 as normative.

### Follow-ups for Carlo (not applied — need his call)

1. **ADR residual number:** the ADR's residuals still say "~13×" absolute rate
   gap; the archived battery gives **~17×** (200,000/11,824). The ADR is
   accepted/signed — the number was not touched. Needs his sign-off to amend
   (or to restate as "an order of magnitude").
2. **SPEC acceptance (ID lock):** the 70 GG IDs remain draft; the Phase-1
   entry gate is still his call.
