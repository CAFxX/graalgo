# Shape-coverage negative tests (R2-P1-3)

Negative E2E tests for the 315 scope-panic traps inventoried in
`docs/dev/SHAPE_COVERAGE_MATRIX.md`: one test per uncovered call shape,
asserting the trap fails loud with its pinned message instead of silently
misbehaving.

## Layout

- `01_method_value_tar_read.go` (+ `.divergent` + `.panic`) — the method-value
  shape. The `.panic` file pins graalgo's first filtered stderr line; the
  `.divergent` marker skips the exit-code comparison (gc succeeds, graalgo
  panics) while stdout must still match (both legs print nothing).
- `draft_iface_dispatch_tar_next.go`, `draft_reflect_tar_read.go` — DRAFTS for
  the interface-dispatch and reflect shapes. Their expected outcome is
  probe-owed (probe P-IF-1 / P-RF-1 in the matrix doc §7): runtime panic vs
  compile-time scope error is not yet established. They carry no `NN_` prefix
  so the runner skips them; promote to `02_…` / `03_…` + `.divergent` +
  `.panic` once the probe pins the expected output. Do NOT rename them to
  `NN_` form before then — an unpinned negative test would fail the suite.
- `run_differential.sh` — thin wrapper (`exec ../run_wave.sh shape-coverage`).

## Conventions

Cross-package by design (unlike the per-package wave-2 dirs): each program
targets one (shape × method) cell of the matrix. Programs print nothing on
the success leg — `.divergent` programs must have identical stdout on both
legs. When a shape class gains a general mechanism, its test flips to a
positive differential test (or is deleted) in the same commit that retires
the matrix rows — never silently.
