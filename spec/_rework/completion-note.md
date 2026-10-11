# SPEC rewrite completion — option C (2026-10-03)

## Process followed
- Read: greenfield SKILL.md, spike 2b REPORT.md (full), SPEC.md (full, 1,040 lines),
  FEASIBILITY.md §8 risk 2, PHASES.md §2.
- Structure fixpoint: [outline-r1.md](outline-r1.md) → self-review (6 questions) → [outline-r2.md](outline-r2.md) (clean).
- Wrote new §3.5, edited §7.2/§7.4/§8.2 rows 2,10,11/§10.1/§10.2, editorial pass
  (one cross-reference fix: "F5 deviation" → "the deviation documented above").

## Every changed section (all in [SPEC.md](../../SPEC.md))
1. §3.5 Concurrency runtime — full rewrite (lines ~293–407 post-edit): goroutine =
   vthread + scheduler token; FJP demoted to untrusted pool; quantum park preemption
   (10ms default, computed 0.27% marked [ESTIMATE]); initFifo/wakeFifo/runFifo + drainer;
   in-flight cap = GOMAXPROCS hard assert; armed-ticket park/unpark safety
   (50,558/50,558); scheduler-aware blocking SHALL (F5 limitation); cgo/pinning under
   option C (verified-vs-assumed split); residual risks (a)(b)(c); falsifier supersedes
   spike 2. Unchanged: netpoller-deleted, channels/select/defer/panic/Goexit/panic(nil),
   -Xss limit.
2. §7.2 — five observables kept verbatim; mechanism language → scheduler parks;
   falsifier → preemption-torture re-run against spike-2b design (Phase 2).
3. §7.4 — falsifier no longer "spike 2's differential run"; now Phase-2 run against
   the §3.5 scheduler. Closure unchanged.
4. §8.2 — row 2 retired-risk record (spike 2 KILLED, spike 2b PASS; residual kill
   triggers named); row 10 → park/quantum overhead budget at 10ms; row 11 → pinning
   as transient dumb-pool capacity consumption, GG-0057 audit = Phase-2 done-gate.
5. §10.1 — §3.5 row replaced; spike-2 KILLED and spike-2b PASS rows added; this-rewrite
   row added.
6. §10.2 — four Appendix-B verdict-scope rows added (kill scope; fairness/overhead
   re-test triggers; lost-wakeup formal-verification trigger; cap assert production
   obligation).

## Labeling
- Every claim labeled [DECIDED]/[VERIFIED]/[HIST]/[ESTIMATE]/[UNVERIFIED].
- Zero new [OPEN] on go/no-go items. The only unlabelable-as-decided items (10ms
  overhead projection, measured pinning behavior) are marked [ESTIMATE]/[UNVERIFIED]
  with re-test triggers — never presented as measured.
- GG-SCHED-*/GG-WAKE-*/GG-FAIR-* are NOT minted here (registry track owns ID minting);
  §3.5 cross-references them as "being minted" with the GG-PREEMPT-* revision note.

## Deliberately NOT touched (flagged for coordinator)
- FEASIBILITY.md §8 risk 2 — left as the historical study record; SPEC §1.1 says the
  spec is the normative record replacing study recommendations; §10 ledger carries
  the supersession. No inconsistency: the study describes the tested-then-killed design.
- PHASES.md §2 — Phase-2 re-estimate (~30–40% below M:N per spike 2b §6.3) and the
  Phase-0 spike-2b listing belong to the coordinator's separate estimate/track work.
- §2.7 — neutral reference ("the goroutine mapping, §3.5"); no killed-design assumption.
- §9 estimates — not in the touch surface; Phase-2 re-estimate is a separate track.
