# Wave-6 SURV-024 watch: #157943 trip conditions (W6)

**Worker:** surv024-wave6-watch · **Fetch date:** 2026-10-04 (all checks ~12:37–12:50 CEST)
**Baseline:** wave-5 results as of 2026-10-04 ~12:00 CEST (see [track-t3-wave5-triggers.md](track-t3-wave5-triggers.md) §2).
**Mission:** check the three named wave-6 trip conditions for rust-lang/rust#157943 (MIR move elimination).
Any one firing triggers a transfer-ruling + Stage-0-prior refresh for SURV-024.
**Scope:** notes only — no SURVEY.md/CHECKPOINT.md edits, no SURV- IDs. All vendor/project
performance numbers are their claims, never ours.

---

## Trip condition 1: Has #163338 (MoveElimination) merged? — NOT FIRED

**Check performed (2026-10-04):**
- Fetched `https://github.com/rust-lang/rust/pull/163338` (PR page).
- Fetched `https://api.github.com/repos/rust-lang/rust/pulls/163338` (state endpoint).

**Result:** **OPEN, not merged.** API fields: `"state":"open"`, `"merged_at":null`,
`"updated_at":"2026-09-30T13:03:50Z"` (last activity = author's head-ref force-push to
`e61dd0db`, 2026-09-30). Labels: `T-compiler`, `S-waiting-on-author`, `T-libs`, `T-clippy`,
`llm-assisted`. Assignee: `tmiasko`. It still depends on #163336 (merged) and #163337
(still open).
**Fired: NO.** No transfer-ruling change.

---

## Trip condition 2: Has a try-perf run landed ON #163338 (not #163359)? — NOT FIRED

**Check performed (2026-10-04):**
- Fetched `https://api.github.com/repos/rust-lang/rust/issues/163338/comments?per_page=100`
  (issue comments).
- Fetched `https://api.github.com/repos/rust-lang/rust/issues/163338/timeline?per_page=100`
  (full timeline), searching for `rust-timer` comments, `bors` events, `"commented"`,
  `"synchronize"`, `"reviewed"` events, and any `2026-10-` dated events.

**Result:** **No try-perf run on #163338.** The PR has exactly one comment — the author's
LLM-disclosure comment of 2026-09-25 (issue comment 5835514430). The timeline contains
no bors try/CI/perf events, no `rust-timer` comments, and no review events; the newest
timeline activity is the 2026-09-30 force-push. The #163359 (ZST prep) try-perf run
recorded in wave 5 (their claims: instr −0.4%, RSS −0.6%, binary −0.9%) remains the
newest measured number in the #157943 family, and it was already ingested in wave 5.
**Fired: NO.** No new measured numbers to refresh the Stage-0 priors beyond what wave 5
already recorded.

---

## Trip condition 3: Has RFC #3943 been accepted (or materially advanced)? — NOT FIRED

**Check performed (2026-10-04):**
- Fetched `https://github.com/rust-lang/rfcs/pull/3943` (RFC page).
- Fetched `https://api.github.com/repos/rust-lang/rfcs/pulls/3943` (state endpoint).
- Fetched `https://api.github.com/repos/rust-lang/rfcs/issues/3943/timeline?per_page=100`
  (198-line timeline) and `.../issues/3943/comments?per_page=100` (issue comments).
- Fetched `https://api.github.com/repos/rust-lang/rfcs/pulls/3943/comments?per_page=100`
  (review-thread comments), searching for `"created_at":"2026-10` and latest
  `"created_at":"2026-09-3` entries.

**Result:**
- **NOT accepted.** State endpoint: `"state":"open"`, `"merged_at":null`.
  The RFC remains **Open, under active review** — unchanged from wave 5.
- "Materially advanced"? The newest review-thread comments are 2026-09-30 (a thread
  between Amanieu and reviewers about call-operand evaluation order and address reuse).
  No review comment has been created since 2026-09-30 (no `2026-10-*` review-comment
  timestamps). The newest *issue* comment is Amanieu's 2026-09-24 RFC-text update
  (StorageAlloc statement, call-argument evaluation order). The PR-level
  `updated_at` of 2026-10-02T15:30:51Z is attributable to review-thread resolution
  churn (already folded into the wave-5 "under active review" characterization).
- Lang-team posture unchanged since the 2026-07-08 joshtriplett comment: lang does not
  need to look at the RFC until the experiment progresses further; an FCP comes only
  *after* #157943 lands under `-Zmir-move-elimination`. No FCP has been proposed.
**Fired: NO.** The "technically unsound at the moment" caveat (Amanieu 2026-06-22)
remains live; the RFC is still not the soundness foundation for anything.

---

## Sibling movement (#163335 / #163336 / #163337 / #163339 / #163340) — no material change

Checked 2026-10-04 via the same API state endpoints:

| PR | Title | Status (2026-10-04) | Change vs wave-5 baseline |
|---|---|---|---|
| #163335 | [1/6] MIR preparation | OPEN, under review | none recorded |
| #163359 | [1.5/6] ZST initialization | OPEN | none (its try-perf run already ingested in wave 5) |
| #163336 | [2/6] TailCopyToMove | **merged_at 2026-09-30T12:11:35Z — stayed merged** | none (no revert observed) |
| #163337 | [3/6] PreciseLiveness | OPEN, `S-waiting-on-review`, assignee tmiasko | `updated_at` 2026-10-03T21:33:54Z — same 2026-10-03 activity wave 5 noted (Amanieu building on tmiasko's dataflow pretty-printing, #163721); still unmerged |
| #163339 | [5/6] Storage reconstruction | OPEN (depends on #163338) | none |
| #163340 | [6/6] Miri support | OPEN, `S-waiting-on-author` | none (last update 2026-09-30; RalfJung review thread from wave 5 stands) |

Landed: still 1 of 7 (#163336, stable). The critical-path pair #163337 → #163338 is
active but unmerged. Nothing in the sibling set changes the transferable facts:
no new measured numbers, no new gates, no design change to the guarded-speculative
arm (the static-soundness core stays struck regardless).

---

## Net result for the wave-6 coordinator

1. **0 of 3 trip conditions fired.** The SURV-024 transfer ruling stands exactly as in
   wave 5: static core struck (no borrow checker in Go); the guarded-speculative arm
   (E3) remains the only viable transfer path; the wave-4 prototype-gated spike
   design (§3) stands as written.
2. **No Stage-0 prior refresh is triggered.** The newest measured numbers remain the
   wave-5 ingestions: wave-4 priors from the 2026-08-29 monolithic #157943 run
   (instr −0.3..−0.7%, cycles −2.2..−2.5%, bootstrap −3.2% — their claims) plus the
   #163359 prep-PR try-perf (instr −0.4%, RSS −0.6%, binary −0.9% — their claims).
3. **Watch continues for wave 7** with the same trip conditions: (a) #163338 merges
   → re-read its MIR test corpus for the SURV-024 spike; (b) try-perf lands on
   #163338 → first measured number on the actual pass, superseding the 08-29 run;
   (c) RFC #3943 accepted (or FCP proposed by lang) → Rust-precedent maturity change.
   Note for the next worker: the GitHub API (`https://api.github.com/repos/rust-lang/rust/pulls/<n>`
   for `state`/`merged_at`, `.../issues/<n>/timeline` for comments+bots) is faster and
   more precise than page fetches for these binary status checks.
