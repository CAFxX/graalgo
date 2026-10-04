# Phase-1 review round: M5 + loop-capture (program-owner directive 2026-10-04)

Carlo asked "How are changes being reviewed?" and approved, 2026-10-04:
1. Review per milestone (not per edit, not only at M10).
2. This round: 2–3 independent reviewers on M5 + loop-capture.
3. PLAN.md carries per-milestone review gates from now on (§4 review gate).

## Scope under review

- M5: defer/panic/recover/Goexit (GG-0019, `defer` per-goroutine chains,
  `panic` = AbstractTruffleException, `recover`, `Goexit` = ControlFlowException,
  panic(nil) handling). Accepted 2026-10-04 with 102/102 tests green + mutation.
- WS-G: builtins + runtime intrinsics + minimal `fmt` overlay (122/122 verb
  conformance vs gc) + interface freeze (37/37 signatures) (GG-0020).
- Loop-capture remediation (2026-10-04): captured loop variables used to share
  one frame slot across iterations; fix = factory pre-scan + `GoBox` cells, no
  exporter wire-protocol change. 8 red-green corpus programs (cf12–cf19, incl.
  restored pn13) fail on pre-fix tree, pass on fixed tree. Full suite 114/114.

## Baseline

`snapshot/` is a read-only copy of the tree at review time (2026-10-04 ~13:02
CEST), checksummed in `SHA256SUMS.txt` (207 files). Reviewers work ONLY from
the snapshot — never from the live tree.

CAUTION: the M6 worker (generics) was actively editing
`frontend/export.go`/`src/.../NodeFactory.java` when the snapshot was taken
(export.go mtime 13:01:45). Generics-scoped code (typeParam/instantiate/
monomorphization machinery) is **out of scope** — M6 in-flight. Reviewers
flag only generics code that *interacts* with M5/loopcap behavior.

## Reviewers

- R1 — semantics/design lens: loop-capture design, defer/panic/Goexit machinery.
  Report: `R1-design.md`.
- R2 — adversarial/testing lens: test adequacy, red-green/mutation evidence
  non-tautology, new edge-case programs.
  Report: `R2-testing.md`.
- R3 — craft/consistency lens: code quality, deprecated-API directive
  (`-Xlint:deprecation -Werror`), docs consistency (frontend-v1.md, freeze-stub.md),
  interface-freeze alignment.
  Report: `R3-craft.md`.

Rules: read-only (reports are the only writes, in this directory — no shared
files, one owner per report). Findings graded P0/P1/P2 with file:line evidence.
State explicitly what could NOT be verified. Do not trust prior reports;
verify from the snapshot.

## After the round

Parent consolidates findings into `FINDINGS.md`, dispatches remediation,
re-runs evidence, and records the milestone sign-off in CHECKPOINT.md.
Work registry: `graalgo-phase1-review-m5-loopcap`.
