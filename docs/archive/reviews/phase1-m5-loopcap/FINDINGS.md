# FINDINGS — Phase-1 review round: M5 + WS-G + loop-capture (2026-10-04)

**Round:** program-owner directive 2026-10-04 (per-milestone review gate, PLAN.md §4).
**Baseline:** [snapshot/](snapshot/) (207 files, [SHA256SUMS.txt](SHA256SUMS.txt) all-OK per all three reviewers).
**Reviewers:** R1 semantics/design ([R1-design.md](R1-design.md)), R2 test adequacy/evidence honesty
([R2-testing.md](R2-testing.md)), R3 craft/consistency ([R3-craft.md](R3-craft.md)). Independent, read-only.
**Parent verification:** R1's gc-model thesis re-derived from gc's own
`cmd/compile/internal/loopvar/loopvar.go` (go1.27.1) — confirmed; all 5 P0 oracle
programs re-run under stock gc — outputs match R1's claims exactly. R2/R3's
`m5_green.log` / `m5_mutant_red.log` claims re-checked by parent — confirmed.

## Headline verdict

1. **The loop-capture design is a mis-model of gc (5 P0s, all oracle-confirmed).**
   The design uses two cells (`S_outer` for init/cond/post + header closures,
   `S_iter` for the body, glued by write-through). gc's `loopvar` desugar has
   **one variable per iteration** plus a carry with write-back:
   `z := z'` → post(z) → cond(z) → body(z) → `next: z' = z`.
   The two-cell model is observationally equivalent only under 5 assumptions
   (whole-variable body writes; no stale-cell writes; no cond/post closures;
   no `=`-range into header vars; direct-only capture) — each broken by a short
   legal Go program below. The 114 green tests cover none of these shapes.
2. **Acceptance evidence has filing failures (2 P0s).** `m5_green.log` is a
   70-pass run terminated mid-suite, not 102/102; the cited green log lived in
   `/tmp` and is gone. Loop-capture red-green + 114/114 + 4/4 self-attack are
   prose-only, no logs.
3. **Code craft is high; docs lag in 5 material places** (R3: no deprecated-API
   violations, untangle clean; freeze/wire docs contradict the code).
4. **Tests are adversarial in style but under-cover the fixed behavior**
   (16 P1 gaps, 17 gc-pinned adversarial programs supplied by R2).

## P0 — must fix before milestone sign-off

- **P0-D1** (R1): stale per-iteration cell writes corrupt the shared outer cell →
  wrong loop termination. `var g func(); for i:=0;i<4;i++ { if i==0 {g=func(){i+=100}}; if i==2 {g()}; println(i) }` — gc `0 1 2 3`, design traces to `0 1 2`.
- **P0-D2** (R1): struct/array field mutations in body invisible to post/cond.
  `for s := (T{x:0}); s.x<3; s.x++ { s.x=5; … }` — gc `n=1`, design traces to `n=3`.
  DESIGN_WIRING.md correction #1 ("post reads AND writes S_outer") is backwards vs gc.
- **P0-D3** (R1): closures created in cond/post capture `S_outer`; gc captures the
  per-iteration variable. Post-closure program — gc `1`, design traces to `2`.
- **P0-D4** (R1): `for i = range …` (`=` form) into an enclosing header variable
  writes `S_outer` while body reads `S_iter`. gc `11`, design traces to `0`.
- **P0-D5** (R1): transitive capture through a nested funclit doesn't propagate;
  inner literal reads the live slot. `fns[1]()()` — gc `1`, design traces to `2`.
- **P0-E1** (R2, confirmed by R3 + parent): `evidence/m5_green.log` documents a
  terminated 70-pass run (pn06 oracle SIGTERM, `make Terminated`), not "102/102
  green". The cited `/tmp/m5_verify.log` is absent. M5's headline claim is
  uncheckable as filed.
- **P0-E2** (R2): loop-capture "8 red-green + 114/114 + self-attack 4/4" have no
  log evidence (prose only). Predicted pre-fix values are exactly derivable and
  two are corroborated in-prose, but the runs can't be audited.

## P1 — fix before sign-off (23)

**Design/test:**
- **P1-D6** (R1): `cf16_loopcap_writethrough.go` is vacuous — no funclit, so no
  boxing; passes trivially while claiming to cover the write-through.
- **P1-D7** (R1): `BoxRefreshNode` copy depth unverifiable (values pkg absent from
  snapshot); refresh must be Go-assignment (field-wise, aliasing), not recursive
  deep copy. No corpus test pins struct-with-pointer-field.
- **P1-D8** (R1): dead 4-arg `ForRepeatingNode` constructor; its javadoc describes
  the more faithful design (prologue before cond) that `buildFor` doesn't use.
- **P1-T1** (R2+R3): `m5_mutant_red.log` shows 3 failures, checkpoint accounts for
  2 — pn14 trailer mismatch unexplained (no in-snapshot baseline).
- **P1-T2** (R2): WS-G mutant2 kill not credible — hex-path string rows all use
  `"hi"` (no a-f digits), LDIGITS→UDIGITS unobservable; matrix lacks any
  non-ASCII `%x` string case. Real matrix gap.
- **P1-T3** (R2+R3): freeze "37/37" denominator undefined — doc claims 58, tables
  list 63, test verifies 37, no mapping.
- **P1-T4** (R2): 8 loopcap tests pin per-iteration-vs-shared but NOT
  per-iteration-vs-per-closure (adv02: gc `0 1 1 10 11 11` vs per-closure `0 1 0 10 11 10`).
- **P1-T5..T16** (R2): 12 coverage gaps, each with a gc-pinned program in
  [R2-testing.md](R2-testing.md) §3: struct-header post write semantics (adv16), struct field
  write-through (adv17), range-over-map capture (adv04), while-loop capture
  (adv05), closure invoked during its iteration (adv03), inner-loop over outer
  body var (adv01), cross-frame per-frame defers (adv07), recover→named-result
  (adv08), defer-during-unwind (adv09), panic/recover×capture interplay (adv06),
  nested Goexit (adv11), fmt on boxed vars (adv10).

**Docs:**
- **P1-C1** (R3): `freeze-stub.md` "Builtin nodes" table documents dead class
  `PanicNode(GoExprNode)`; M5 dispatches `PanicBuiltinNode`/`RecoverBuiltinNode`.
- **P1-C2** (R3): `frontend-v1.md` (normative D1) §6/§7 omit `defer` + `funclit`
  wire kinds; §7 still M4-era ("rejects every builtin call"). No owner for the
  M5 doc gap (M6 brief covers only generics additions).
- **P1-C3** (R3): freeze doc omits the M5 runtime surface the AST calls
  (`GoErrors.*` ×13, `GoroutineState`, `Goexit.goexit`, `GoCalls.invoke`,
  `DeferredCall`, `DeferChain`) with no scoping rationale.
- **P1-C4** (R3): `corpus/panic/README.md` "Known limitations" still documents the
  fixed loop-capture bug as live, citing a pn13 workaround that no longer exists.

## P2 — fix opportunistically (15)

R1: redundant double copy on boxed writes; stale "(M6)" labels on loopcap code
(collide with generics milestone — rename). R2: mutant logs lack mutation
diffs; 99990000 vs 100000000 pre-fix pn13 across docs; mutant1 prose-only;
fmt live-gc-vs-tsv methodology ambiguous; controlflow README omits cf12–cf23;
6 smaller semantic gaps with adv programs (adv12–adv15 + nil-defer + panic(nil)
fatal rendering); oracle SIGTERM robustness (P2-7). R3: `Boxing.java` stale class
javadoc; dead `PanicNode` unmarked.

## Remediation brief (for the impl coordinator)

1. **Design rework** (P0-D1..D5): move to gc's model — init/cond/post/body all
   operate on the per-iteration cell; explicit end-of-iteration write-back
   `S_outer := S_iter` (including `continue` paths); refresh before cond
   (the dead 4-arg `ForRepeatingNode` already describes this); header-phase
   closures capture the per-iteration cell; range-`=` uses the phase-aware slot;
   transitive capture propagation through intermediate funclits. Keep the phase
   stack (sound for nesting). Red-green: the 5 P0 programs + R2 adv02/adv16/adv17
   must FAIL on the current tree with the traced values, PASS after.
2. **Tests**: land R2's 17 adversarial programs (adapted to `// observe:`);
   fix vacuous cf16 (add capturing funclit); add per-closure distinguisher
   (adv02), struct-header programs, range-map/string, while-loop.
3. **Evidence**: re-run full suite; archive logs durably under
   `phases/phase1/hidden_files/` (never `/tmp`); rename/replace `m5_green.log`;
   explain pn14 or fix the trailer comparison.
4. **Docs**: remediate P1-C1..C4 (5 small edits); reconcile freeze counts and
   mark test-covered rows; fix GG-0045 dangling refs (R3 P2).
5. **Sequencing**: single owner for `NodeFactory.java` + `frontend/export.go`
   (AGENTS.md one-owner rule). M6 (generics) is in flight on both files —
   coordinator sequences: finish or pause M6 before the rework worker starts.
6. **Sign-off**: re-run all review evidence (suite green, red-green logs,
   mutation), then record the M5+loopcap milestone sign-off in CHECKPOINT.md.

## Status

- [x] R1/R2/R3 reports delivered and consolidated here (2026-10-04)
- [x] Remediation dispatched → registry `graalgo-phase1-loopcap-rework`,
  worktree ~/workspace/graalgo-loopcap-rework branch rework/loopcap-gc-model
  (survived VM reboot; rework worker re-dispatched 2026-10-04, red-green phase
  complete per hidden_files/loopcap-rework/PROGRESS.md)
- [ ] Findings remediated + re-tested
- [ ] Milestone sign-off recorded
