# P3-M1 reviews — record (supervisor reconstruction)

**Snapshot reviewed:** `7bf5e4e..1664fa9` (fixed diff of the milestone scope).
**Sign-off commit:** `ab76200`.

## What the coordinator reported (from its sign-off report, 2026-10-06)

Two independent reviewers ran against the fixed snapshot:

- **Code reviewer: NO-GO.** 1 P0 + 3 P1 + 4 P2.
  - P0-1: embedded `struct{ T }` silently rebound to the wrong type (reproducer:
    gc `h.T` vs merged `main.T`, both exit 0) → remediated `86b0a61`.
  - P1-2: `// +build` without blank line wrongly honored → remediated `8b87455`.
  - P1-3: import cache ignoring anchor dir → remediated `8b87455`.
  - P1-4: no subprocess timeout on the exporter → remediated `f11ba64`.
  - P2-1..4: remediated across `86b0a61`/`8b87455`/`f11ba64` (per sign-off report).
- **Methodology reviewer: GO-WITH-FINDINGS.** 4 P1 + 7 P2.
  - P1s: GG-0013/0023 never activated in registry → remediated `f8269ee`;
    no quiet-divergence red-green → remediated `ab98a09` (third mutation);
    `init()` fail-fast and stdlib scope-error paths untested → tests added `ab98a09`.
  - P2-1..7: remediated per sign-off report.

All findings were remediated in 6 one-change-per-commit commits and re-tested
before sign-off. The P0 reproducer is a real red-green catch (quiet divergence
proven, then fixed).

## Process gap (supervisor finding, 2026-10-06)

The per-reviewer report files were **never archived** — no `reviews/phase3-m1/`
was committed by the coordinator, and no copies exist on disk. The findings
above are reconstructed from the coordinator's sign-off report and the
remediation commit messages; the original reviewer prose is lost.

This violates the Phase-3 plan's review gate ("review evidence (snapshot
checksums, per-reviewer reports, findings list with remediation at named
targets) lives under `reviews/`"). The substance (findings + remediations +
re-tests) is verifiable in the commits, so P3-M1 stands — but from P3-M2 on,
the milestone brief explicitly requires the coordinator to commit
`reviews/phase3-m*/reviewer1.md` + `reviewer2.md` before sign-off, and the
supervisor will reject a sign-off missing them.

Escape protocol: fix instance (this record), hunt class (all future
milestones — P3-M2 brief patched), patch process (this paragraph is the
patch).
