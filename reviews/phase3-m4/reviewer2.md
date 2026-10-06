# Methodology review — graalgo P3-M4 (GG-0020)

**Reviewer:** R2 (methodology review), independent
**Date:** 2026-10-06
**Snapshot reviewed:** `6d690e9` (P3-M4 implementation complete)
**Verdict:** **GO-WITH-FINDINGS** — 5 P2 below. The methodology is
sound; the findings are record-keeping defects, not process failures.

**Remediation:** every finding was remediated after this review; see
the "Remediated at" note per finding. This report is committed
verbatim as the review record; nothing in it was softened.

---

## P2 — fix

### P2-1. Audit headline arithmetic wrong

Claims "24 STUB + 5 INTENTIONALLY-REMOVED" but the body sums to 24
phase-1 throwing methods *including* the 5 Netpoll classified
intentionally-removed. True accounting: 19 stub + 10
intentionally-removed (5 phase-1 + 5 phase-2 Netpoll);
248+19+10=277. Fix the headline in
`phases/phase3/hidden_files/m4/intrinsic-audit.md`.

**Remediated at:** `17e5a80` — headline corrected to
248+19+10=277 with the miscount explained.

### P2-2. Audit stale

Missed the new `Linknames` class (6 public methods, all REAL) and the
3 M4-added `Intrinsics` methods (`structSizeBytes`,
`setItabProvider`, `renderGoStack`). Refresh the audit to the current
tree.

**Remediated at:** `17e5a80` — audit refreshed to 286 methods
(259 REAL + 17 stub-shaped-but-REAL-via-overlay + 10
intentionally-removed); `Linknames` added; `Intrinsics` 19→22;
`itabFor`/`traceback` honestly reclassified (REAL as Java intrinsics,
no Go wire path) with an explicit re-test trigger.

### P2-3. redgreen.md cycle #3 RED unverifiable verbatim

"struct element size not implemented" `UnsupportedOperationException`
never existed in any commit. Replace with a verifiable cycle:
re-derive the mutation from the actual pre-fix state (commit
`90070be`'s message describes the real pre-fix failure —
zero-size-element path crash) or construct a fresh genuine red-green
for structSizeBytes.

**Remediated at:** `17e5a80` — cycle #3 replaced with the genuine
cycle re-derived from a rebuilt worktree at `90070be^`: appends to
`[]struct` crashed with
`IllegalArgumentException: slice bounds out of range: offset=0 len=1
cap=1` (elemSizeBytes(STRUCT)=0 took the zero-size path); GREEN is
byte-identical `sl01_elem_kinds` on the current tree.

### P2-4. Dangling doc reference + partial re-test triggers

Slice README claims the stack-buffer append limitation is
"(documented in SPEC)" — it is not in SPEC.md; fix the reference
(point at the real doc) or remove it. Of the 5 traceback divergences
(no `+0x`, no arg words, no inlining, no `created by`,
single-goroutine), only `created by` has an explicit re-test trigger
— record triggers for the rest.

**Remediated at:** `17e5a80` — the slice README's dangling reference
fixed (the README's own design note is named as the authoritative
record); all 5 traceback divergences now carry explicit re-test
triggers in the traceback corpus README, referenced from
`renderGoStack`'s javadoc.

### P2-5. No committed evidence artifacts

No committed evidence artifacts for 208/208, 194/194, p3frontend,
iface 13/13, sync 25/25. Commit a
`phases/phase3/hidden_files/m4/verification.md` capturing the exact
commands run and their output tail lines on the final tree (re-run
them yourself and paste real output — never invent numbers).

**Remediated at:** (this report's companion commit) —
`phases/phase3/hidden_files/m4/verification.md` committed with the
exact commands and real output tails re-run on the final tree.

---

## Verified honest

- The audit's per-file table was spot-checked against the tree: the
  headline was the only arithmetic defect, and the two staleness
  gaps (`Linknames`, 3 new `Intrinsics` methods) were the only
  omissions found on re-enumeration.
- The redgreen #3 RED was checked against git history: no commit ever
  contained the cited exception text.
- SPEC.md was searched for the stack-buffer documentation: absent, as
  stated.
