# P3-M7 wave-1 per-package worker checklist

Every package worker must produce exactly these deliverables. Nothing is
done until all eight are green and committed. Conventions live in
`phases/phase3/corpus/wave1/README.md`; the shared runner is
`phases/phase3/corpus/wave1/run_wave.sh`.

## 1. Phase-1 synthetic source — `phases/phase1/frontend/synth_<pkg>.go`

- `package main`, one `const synthetic<Pkg>Source = \`package <pkg> ...\``
  with REAL Go implementing the package (model: `synth_time_context.go`,
  `syntheticTimeSource` / `syntheticContextSource`).
- Frontend/v1 constructs only — NO generics. Must keep passing
  `go vet`-level type-check AND the exporter's scope rules.
- Out-of-scope members: exact gc 1.27.1 signatures, panic bodies
  reporting "not in scope" — fail fast, never silently.
- If the package needs intrinsics (clock-like entry points the Java side
  must own): declare them with exact gc signatures and panic bodies, and
  document the intrinsic set in the file header.

## 2. Exporter interception — `phases/phase1/frontend/export.go` (ONLY if intrinsics needed)

- Follow the `exportSyntheticPkgCall` pattern used for time/context
  (export.go ~line 4379): intercept the intrinsic call shapes, emit the
  documented wire nodes. No interception for packages implemented purely
  in real Go.
- The synthetic source must be type-checked on demand with the overlay
  importer so synthetic→synthetic imports resolve to the synthetic
  package (the `context` imports `time` pattern from M5).

## 3. Phase-3 mirror — `phases/phase3/src/p3frontend/synth_<pkg>.go` + `importer.go` case

- Mirror the frozen exporter's synthetic signatures (the importer cannot
  reuse them — unexported and frozen). Model: p3frontend
  `synth_time_context.go` / `syntheticTimePackage()`.
- Register the import path in `importer.go` next to the existing
  `time`/`context` cases.

## 4. Drift test — `phases/phase3/src/p3frontend/<pkg>_test.go`

- Method-by-method (type-by-type, member-by-member) comparison of the
  p3frontend mirror against the real phase-1 synthetic source, so mirror
  drift fails fast. Model: `timecontext_test.go`
  (`TestTimeSyntheticSignatures` et al.: package name/path, named types
  and underlying kinds, constants, method sets, function signatures).
- The test must be demonstrably capable of failing (mutate the mirror,
  watch it go red, revert).

## 5. Java nodes — `phases/phase3/src/main/java/graalgo/p3<pkg>/nodes/` (ONLY if intrinsics needed)

- One node class per intrinsic, modeled on
  `graalgo/p3time/nodes/TimeNowNode.java`. Pinned toolchain:
  GraalVM 25.0.4.1.1 LTS, plain javac, `-Xlint:deprecation -Werror`
  (standing directive 2026-10-04) — the phase-3 Makefile enforces this.
- Pure-Go packages get NO Java nodes.

## 6. Differential corpus — `phases/phase3/corpus/wave1/<pkg>/`

- Programs `<nn>_<name>.go` (nn = 00, 01, ...), adversarial +
  combination tests (esoteric 3+ feature intersections, malformed/hostile
  inputs, degenerate values), deterministic output only.
- Timing-sensitive programs print fixed strings only, with a
  `<base>.timing` marker (gmp=1 only); all others run gmp=1 and gmp=4.
- Panic programs get `<base>.panic` with the expected first stderr line.
- Thin `run_differential.sh` wrapper calling `../run_wave.sh <pkg>`
  (template in the wave1 README). Full corpus must be green:
  `bash phases/phase3/corpus/wave1/<pkg>/run_differential.sh`.

## 7. Red-green evidence — `phases/phase3/hidden_files/m7-<pkg>/redgreen.md`

- Every substantive behavior gets red-green evidence: show the test
  failing with the implementation broken/mutated (red), then passing
  with the fix (green). Non-tautological: the evidence must demonstrate
  the test CAN fail (kill the implementation, watch red; a test that
  never went red is not evidence).
- Record the exact commands, the failing/passing diffs, and the commit
  each side was run against.

## 8. Docs

- Update every doc that names the package's status: the phase-3
  p3frontend DESIGN.md synthetic-package table, the wave-1 README
  package list (if it enumerates packages), and any milestone status doc
  the coordinator maintains. A code change is not done until every
  document it touches is updated in the same change; docs must be
  consistent with each other and with the code.

## Standing discipline (applies to all of the above)

- **Tmp**: `/tmp` is NEVER used and NEVER cleaned (512 MB tmpfs shared
  with live work). Export for every go/java command:
  `TMPDIR=$HOME/workspace/graalgo/phases/phase3/hidden_files/m7-tmp/<pkg>`
  and `GOCACHE=$HOME/workspace/.gocache-m7-<pkg>` (per-package, never
  shared). See the wave1 README "Tmp discipline" section.
- **Suites stay green**: phase-1 (211), phase-2 (194), phase-3 (58+).
  Run the affected suites before claiming done; never infer one build
  config from another.
- **No placeholders.** Every listed member is implemented, intercepted,
  or explicitly panic-bodied as out of scope.
- **Commits**: hunk-by-hunk, only your own files; never `git add` a file
  another worker touched; verify with `git diff --cached --stat`.
