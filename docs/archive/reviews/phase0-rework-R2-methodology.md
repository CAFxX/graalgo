# Peer review R2 (methodology/evidence) — graalgo Phase-0 rework

**Reviewer:** R2 (methodology/evidence lens) · **Date:** 2026-10-03 ·
**Scope:** spike-03 REPORT.md (PASS verdict), ADR-0001 (D1–D5),
tools/req namespaced-ID change, SPEC.md vs FEASIBILITY.md vs PHASES.md consistency
**Method:** full read of all in-scope documents; independent re-execution of
`go test ./...` (green) and `req lint` on the real registry (clean: 70 reqs,
396 refs); empirical red-green check of the namespaced-mint tests (fail without
the change — confirmed); spot-verification of spike-03 evidence artifacts
(32/32 matrix entries, 45/45 harness SAME, 12-item api diff, tarball SHA256,
script_test.go pristine-hash revert — all confirmed as claimed).

**Standards applied:** `~/workspace/skills/greenfield/SKILL.md` honesty bars
(§5 measurement honesty, §8 falsification/verdict-scope) and the standing
docs-consistency rule.

---

## P0 — blocks acceptance of the rework (not the spike-3 PASS)

### P0-1. Registry scanner is blind to namespaced IDs — the namespace feature is half-shipped

**Location:** `tools/req/scan.go:37-40` (`tokenRe`, `verifiesRe`, `sloppyRe`);
the namespace change touched `registry.go`/`commands.go` only.

**What's wrong:** The namespaced-ID change updated minting, loading, and `req add
--namespace`, but not the scanner. All three scan regexes require `GG-` followed
by digits:
- `tokenRe = \bGG-\d{4,}\b` never matches `GG-SCHED-0001`
- `verifiesRe = verifies:\s*(GG-\d{4,})` never matches `verifies: GG-SCHED-0001`
- `sloppyRe = (?i)\bGG-\d+\b` never matches malformed namespaced IDs (rule-d blind)

**Why it matters (proven empirically, not inferred):** I built a temp registry
with an *active* `GG-SCHED-0001` and a `// verifies: GG-SCHED-0001` annotation
in code — `req lint` reports `rule (c): active requirement GG-SCHED-0001 has
no verifies: annotation`. Consequences:
1. Rule (c) false-positives for every active namespaced requirement — the 10
   GG-SCHED/WAKE/FAIR IDs can *never* satisfy lint once they leave draft.
   (They're all draft today, which is why the real-registry lint is clean —
   the landmine is armed, not yet fired.)
2. `req supersede`/`retire` on a namespaced ID finds zero codebase hits
   (`codeHits` runs on scan refs) and silently skips the atomic codebase-wide
   reference update — voiding the Greenfield contract "retiring/superseding
   searches the whole codebase for the old ID so all references are updated
   atomically." Silent, not loud.
3. Rules (a)/(b): references to namespaced IDs in docs are unchecked — a doc
   citing nonexistent `GG-SCHED-9999` stays lint-green.
4. `req trace GG-SCHED-0001` shows no code sites or tests.

**Fix:** extend the three regexes to the namespaced form
(`GG-(?:[A-Z]+-)?\d{4,}` or equivalent, kept consistent with `idPattern` in
`registry.go`), add scan tests covering: namespaced `verifies:` recognition,
rule-(c) satisfaction via a namespaced annotation, rule-(a) on a dangling
namespaced ref, and a supersede dry-run counting namespaced hits. Re-run the
red-green check (I confirmed the mint tests fail without the mint change;
the scan half currently has no such test — that's the gap).

**Scoping note:** this does not block the spike-3 PASS (unrelated), and the
mint/load half of the change is sound (monotonic per namespace, no reuse —
verified by test and by re-reading `mintID`). It blocks *accepting the
registry rework as done*.

---

## P1 — must fix before the rework is accepted

### P1-1. REPORT §6 compares extrapolated P50 to a P95 bound

**Location:** `spikes/03-versiontax/REPORT.md` §6 ("Bound comparison").

**What's wrong:** The bound is P95 ≤ 200 agent-hours (GG-0004, SPEC §2.3,
FEASIBILITY §2 — consistent everywhere). §6's table header honestly labels the
extrapolation "P50", then compares it directly to the P95 bound and draws
"Headroom" conclusions ("~2–4×", "touches bound at top end"). For the heavy
class, P50 ≈ 100–200 h against a P95 ≤ 200 h bound: if the P50 already touches
the bound, the P95 is almost certainly *over* it (variance unknowable at n=1
with the dominant term unmeasured — see P1-2). The table as written implies
more comfort than established.

**Why it matters:** this is the report's bottom-line evidence for risk 3. A
P50-vs-P95 comparison that reads as "inside the bound" is exactly the
computed-presented-as-stronger-than-it-is pattern the honesty bar targets —
even though the numbers are labeled [ESTIMATE].

**Fix:** restate §6 as "P50 sits inside the bound; P95 compliance is *not
established* by this run." Add a re-test trigger: P95 calibration happens on
the first real upgrade executed through the workflow (PHASES §8 done-gate 1:
"one full release upgrade performed by the workflow within the bound") — until
then, no claim of bound compliance, only "no breach observed." This does not
overturn the PASS (the kill trigger is *breach* with no reduction path, and no
breach was measured), but the document must not be citable as proving the
bound holds.

### P1-2. A4's re-test trigger (§8.1) is not executable as written

**Location:** REPORT §5 assumption A4; §8.1 re-test trigger.

**What's wrong:** A4 is honest — "language-delta *implementation* toil is NOT
measured here... carried from T2's release-notes calibration as [ESTIMATE]" —
and language delta is the bound's dominant line item (25–45 h of the ~100–200 h
heavy total). But §8.1's trigger says: "re-run the dry run on a boundary where
a language feature requires new lowering work, and measure that toil on a real
prototype (candidate: generic methods, 1.27)." Two problems: (a) generic
methods already shipped in 1.27 — the boundary just crossed; (b) the Phase-0
prototype set contains *no lowering prototype* (it's tooling + harness +
scheduler), so there is nothing to measure lowering toil against. The trigger
cannot fire.

**Why it matters:** the verdict's evidentiary weight is concentrated in the
scoping machinery (diffs), while the bound's biggest line items remain T2's
LOW-confidence estimates. The one trigger meant to close that gap is
incoherent, so the gap has no credible closure path — which is precisely what
would matter if a future heavy release *did* breach.

**Fix:** rewrite §8.1 to name the real mechanism: "when Phase 1 has a working
frontend/lowering prototype, replay a historical language delta (e.g. the 1.27
generic-methods-on-`math/rand/v2.Rand` change, already [VERIFIED] in SPEC §3.2)
through the lowering workflow and measure toil; treat the result as the first
calibration point for the language-delta line item, replacing the T2
[ESTIMATE] for that component."

### P1-3. The F8 follow-up is recorded nowhere actionable

**Location:** REPORT §3.3 ("The 05 spike's R3 ... is extended: watch
`objabi.expandArgs`/`ParseArgs` too"); §8.7 re-test trigger.

**What's wrong:** (1) I checked `spikes/05-cmdgo/REPORT.md` — R3 (line ~238)
was *never actually extended*; the extension exists only in the 03 report.
(2) `SPEC.md` contains zero mentions of response-file / `expandArgs` /
`ParseArgs` / `objabi` — the recurring toil item this spike discovered
(per-release response-file re-sync + `cgo_long_cmd` conformance, ~0.5 h) is not
wired into SPEC §5.8 (the diff tooling — the toil-reduction engine), §6.1 Phase 2
(scoped diffs), or GG-0035. The only record is §8.7 of a spike report — a
historical document, not a workflow.

**Why it matters:** the single most valuable concrete finding of the dry run
(an unversioned internal wire format the wrapper is coupled to) will be lost
exactly when it's needed — at the first release where the format changes again.
The "extend R3" sentence in §3.3 asserts a cross-spike edit that didn't happen.

**Fix:** (a) actually extend 05's R3 (or add the watch to SPEC §5.8's tool list
and §6.1 Phase 2's scoped-diff checklist — the normative home — plus a line in
GG-0035's statement); (b) strengthen the conformance check: a single script
(`cgo_long_cmd`) only exercises the arg shapes that script happens to produce.
A format change affecting quoting of a new special char could pass
`cgo_long_cmd` while the wrapper is wrong. Add a small response-file arg-shape
battery (spaces, quotes, backslashes, unicode, very long single arg) as the
F8 conformance gate. On the ~0.5 h estimate itself: credible *for the mechanical
re-sync as scoped* (verbatim copy + one-script check); the battery above is
what makes the "conformance-gated" claim honest.

### P1-4. PHASES.md §2 still plans the killed spike-2 design; FEASIBILITY.md has no historical banner

**Location:** `PHASES.md` §2 spike 2; `FEASIBILITY.md` header, §1 problem #1,
§4(5), §8 risk 2, §9.

**What's wrong:** PHASES §2 lists "Goroutines-on-Loom spike — 1:1 virtual
threads + back-edge yields + custom carrier pool" with no note that this design
was KILLED and superseded by 2b/option C — while §4 (Phase 2) was rewritten
around option C and §10 carries the option-C re-estimate. The Phase-0
done-gates (§2) reference "spikes 1–6", so a reader plans/executes the killed
design. Separately, FEASIBILITY.md still *recommends* the killed design as
[PROPOSAL] (§1 hardest-problem #1, §4(5): "mandatory back-edge safepoint
yields... plus a custom carrier pool") and §8 risk 2's falsifying spike is the
killed design — with no banner stating this is the pre-2b study record.
SPEC §1.1 *does* declare itself normative ("replacing the study's
recommendations with decisions") and §10.2's ledger carries the spike-2 kill
supersession — so the normative declaration exists, but the deliberate
historical-ness is invisible at the point of use. Related staleness in the same
class: PHASES §10 and FEASIBILITY §9 show agent-leverage as `[OPEN]` ("quantify
or drop") while SPEC §7.14 records it `[DECIDED]` dropped.

**Why it matters:** the standing docs-consistency bar. A future argument will
cite FEASIBILITY §4(5) or PHASES §2 as the program's scheduler plan — the exact
failure mode the verdict-scope ledger exists to prevent.

**Fix:** (a) annotate PHASES §2 spike 2 as **KILLED** with a pointer to 2b
(keep the entry — it's the falsification record — but mark it unexecutable);
(b) add a banner to FEASIBILITY.md's header: "Historical study record as of
2026-10-03 pre-spike-2b. SPEC.md §1.1 is the normative record; the scheduler
recommendation (§1-#1, §4(5), §8-risk-2) was killed by spike 2 and superseded
by SPEC §3.5 / ADR-0001 D5 (option C); see SPEC §10.2 ledger." Plus one-line
supersession notes at §4(5) and §8-risk-2, and align the agent-leverage
[OPEN]s with SPEC §7.14 [DECIDED].

---

## P2 — nits / polish

### P2-1. "0 rebase conflicts" is vacuous, not a measurement
REPORT §4.2: "Toolchain build + regen … ~0.5 h verify (0 rebase conflicts this
boundary)." The prototype set carries no patch stack — there was nothing to
rebase — so GG-0044's ≤5-file budget is unexercised, not satisfied. Label it
[N/A — no patch stack in prototype] rather than presenting a clean rebase.

### P2-2. The sampled boundary is a favorable-tail boundary
The 1.26.8→1.27.1 delta *shrank* two coupled surfaces (memhash/strhash asm→Go,
checkASM deleted) — the report notes this ("Favorable to graalgo") but the
extrapolation doesn't discount for it. One-line caveat in §5/A6: the quiet-band
calibration point sits on a surface-shrinking boundary.

### P2-3. Weak power of the 16/929 txtar sample
§8.3 is honest that the seam verdict "assumes the sample's 0-divergence rate
generalizes," but with n=16, a true 5% divergence rate still yields 0 observed
with p≈0.44. The cost model folds full-matrix divergence triage into a "~1–2 h
review budget." Add an explicit contingency line for full-matrix divergence
triage (mechanical full-matrix cost *is* measured at 29.3 s/run; the triage
side isn't).

### P2-4. `idPattern` vs `idSplitRe` inconsistency
`registry.go`: `idSplitRe` accepts 1–3-digit classic IDs (`GG-001`) and any
sequence width, but `idPattern` (the load gate) requires `\d{4,}` classic /
exactly-4-digit namespaced — so a namespaced sequence ≥10000 would mint an ID
the loader rejects. Unreachable in practice; align the two patterns.

### P2-5. TESTING.md still references momo paths
`tools/req/TESTING.md` lines 10, 25, 40 point at `~/workspace/momo/...`
(copy-paste from the fork). Fix to graalgo paths. (README.md correctly notes
the fork provenance — fine.)

### P2-6. ADR D5's falsifier omits the residual-risk-hardening triggers
D5's falsifier ("systematic divergence the scheduler cannot close") is weaker
than SPEC §8-row-2's kill trigger, which additionally names residual risks
hardening into mechanism blockers (overhead unfixable by the 10ms quantum,
cold-drainer unexplained at server scale, scheduler-transparent NIO throughput
unbounded → re-scope). Align the wording; the ADR is where a future reader
looks first.

### P2-7. ADR sign-off doesn't reference SPEC acceptance
The sign-off procedure (peer-review remediation + Carlo's signature as the
Phase-0→1 gate) is adequate, but doesn't name SPEC acceptance / requirement-ID
lock as a precondition — the registry is still "draft, unlocked" (all 70).
One line tying ADR acceptance to the ID lock would close the loop.

### P2-8. Wall-clock accounting doesn't name the fuzz soak
§5's mechanical line (~5 h unattended) doesn't mention the 8–24 h unattended
fuzz soak (SPEC §5.4) or the 2–6 h attended oracle compute (SPEC §5 intro).
Still ≪ the 10-day bound under A5; naming them closes the loop explicitly.

---

## What I checked and found sound (no finding)

1. **[M]/[ESTIMATE] discipline:** exemplary. The TOIL.log timestamp slip is
   disclosed in §4.1 rather than hidden; the runner restart onto the wrong
   wrapper binary (§3.3) is disclosed; judgment figures are labeled
   single-agent estimates (§8.5).
2. **Evidence spot-checks all confirm the report:** 32/32 matrix entries in
   `matrix126_results.jsonl`; 45/45 SAME, 0 diverged in `harness_126.json`;
   the 12 additive `go/*` items in `api_diff.txt` match §3.4's list; tarball
   SHA256 in `SOURCES.sha256` matches the quoted go.dev hash; `script_test.go`
   in the 1.26.8 tree hashes to the pristine value (`f07bb8ee…`) — the §8.4
   MUST-revert was honored and verifiable.
3. **A1–A6 are each labeled and defensible as assumptions:** A1 (SPEC §6.3
   process exists), A2 (per-item pricing calibrated on the dry run), A3
   (coverage honestly split: 100% on four surfaces, 1.7% txtar sample, smoke
   corpus), A5 (agent-hour definition consistent with T2), A6 (order-of-
   magnitude stability + rc1 early warning). A4 is the honest weak point —
   see P1-2.
4. **ADR D1–D4 match SPEC** (§3.2/§3.1/§2.5+§3.9/§3.8) and the rejected
   alternatives are fairly characterized (D2's "52/52" verified in the 05
   report; D2's "one divergence … fixed in the wrapper" accurately describes
   spike 5's F6/cgo_long_cmd).
5. **ADR D5's "rejected (ii)" is fair:** "viable but unvalidated; dominated by
   option C on simplicity … and option C is empirically validated" mirrors the
   2b report's own §5 language and cites the numbers (15,603 vs 0, cap assert,
   50,558/50,558). The residual-risk list is **complete** against 2b REPORT
   §3–4 (overhead fired-on-mean + scoped fix, 38µs cold-drainer, 13× rate gap,
   F5 scheduler-bypass deviation, INIT-starvation fix recorded in the D5 body).
6. **Registry mint/load half is sound:** monotonic per namespace, no reuse
   (retired IDs still occupy their numbers — tested), `req lint` clean on the
   real tree (70/70 draft, 396 refs), and the new mint tests fail without the
   change (verified by reverting `mintID` in a /tmp copy: `TestMintID_Namespaced`
   and `TestMintID_BadNamespace` fail as expected).

---

## Overall judgment

1. **The spike-3 PASS verdict STANDS.** The kill trigger ("bound breached with
   no credible reduction path") did not fire: quiet P50 ~50–100 h and heavy
   P50 ~100–200 h sit at or inside the ≤200 h bound, the scoping machinery
   (the actual toil-reduction engine) is empirically validated at a real
   boundary, and the report's honesty discipline is high. "Language-delta toil
   was scoped but not measured" does not overturn the verdict — it *bounds*
   it: the PASS rests on the reduction engine plus T2's LOW-confidence
   estimates for the dominant line items, and must be read that way. P1-1
   (P50-vs-P95) and P1-2 (executable calibration trigger) must be fixed before
   the report is citable as the Phase-0 evidence for risk 3, but they don't
   change the binary verdict.
2. **ADR-0001 is committable AFTER remediation, not as-is.** D1–D5 are faithful
   to SPEC, rejections are fair, residual risks are complete, and the sign-off
   procedure is adequate. Blocking items: the P0 registry-scanner gap (the ADR
   normatively references the GG-SCHED/WAKE/FAIR IDs whose tooling is
   half-done) and P1-4 (the stale PHASES §2 / unbannnered FEASIBILITY.md, which
   ship in the same rework package and directly contradict D5's supersession
   story). P1-3 (F8 actionability) belongs with the SPEC, not the ADR, but
   should land in the same remediation pass.
3. **Recommended remediation order:** P0-1 (scanner) → P1-4 (stale docs, cheap)
   → P1-1/P1-2 (report §6/§8.1 wording + triggers) → P1-3 (SPEC §5.8/§6.1 +
   05-R3 + arg-shape battery) → P2 sweep. Then re-run `go test ./...`,
   full-tree `req lint`, and the P0 red-green check before the ADR moves to
   `accepted`.
