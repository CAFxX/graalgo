# Recovery plan (started 2026-11-17)

[SPEC.md](SPEC.md) is authoritative. This plan brings the implementation back
to it. Baseline audit: three independent audits of every `GG-*` requirement
against the code at `a0df481b` (findings summarized below; each claim was
verified by running programs, not by reading docs).

## Baseline (a0df481b)

| Area | State |
|---|---|
| Scheduler core (quantum park, drainer, wake lanes, fairness) | Good; unit-tested and measured |
| Value model (`GoStruct`/`GoSlice`/`GoString`/`GoMap`/…) | Mostly matches SPEC §3.4 |
| Vendored `go/*` | Byte-identical to go1.27.1 |
| Process lifecycle | Broken: hangs when main returns with live goroutines or a goroutine panics; no deadlock detection |
| Panic model | Runtime errors are not `error`; recovered `panic(nil)` crashes; panic print skips `Error()`/`String()` |
| JIT | Never compiles: `PermanentBailoutException: Too deep inlining` on every function (no `@TruffleBoundary`); ~50–500× slower than gc |
| Frontend v1 | Single-file JSON IR; cannot build multi-package modules; 140 hand-written `synth_*.go` stdlib replacements (violates GG-0009) |
| Frontend v2 | ~5% prototype |
| Driver | Custom 5-command reimplementation (SPEC GG-0011 requires a `-toolexec` wrapper) |
| `TruffleLanguage`/polyglot `Context` | Absent (blocks interop, debugging, native-image) |
| AOT / hybrid modes, isolates, cgo→Panama | Absent |
| Test gates | `snapshot-check` records graalgo output (SPEC §5.9: gc); `%`→`floorMod` mutant survives every per-push gate; layer 3/4 scripts reference the deleted `phases/` tree |
| Upgrade tooling (GG-0035, 0039–0043, 0056) | Absent |

## Tracks

**T0 — make what exists honest** (no architecture dependency; start now)
- T0.A runtime lifecycle: exit when main returns, fatal on goroutine panic,
  deadlock detection, panic value model, GOMAXPROCS wiring.
- T0.B JIT enablement: PE boundaries, PE-friendly `Safepoint.poll`, measured
  speedup, regression guard.
- T0.C test infrastructure: snapshots under gc, working mutation gate,
  per-push gate that catches the `floorMod` mutant, remove `phases/` paths,
  widen layer-2 triggers, prune stale exclusions.
- T0.D flatten the same-FQN stub overlay (truffle+runtime → one module).
  Done 2026-11-18: [internal/backend/truffle/](../../internal/backend/truffle/)
  is the one core module (nodes, values, runtime, scheduler); the 8 stubs,
  the `isPhase2()` classpath-order guards and the overlay tests are
  deleted; interop builds downstream.

**T1 — architecture (SPEC §3.1–3.3)**: design, then build, the per-package
`-toolexec` compile/link path: `compile` emits typed IR into the package
archive, `link` bundles it into a launcher; the real stdlib compiles through
it; delete `synth_*.go` and the v1 JSON frontend. Design doc first
(`toolexec-design.md`, written by lane T1).

**T2 — language integration**: `TruffleLanguage` registration + polyglot
`Context` (GG-0005/0024), then Go↔Java interop and debugging.

**T3 — modes**: native-image AOT, hybrid, isolates, mode selection
(GG-0006/0049/0050/0060/0025/0026), reachability metadata (GG-0052).

**T4 — remaining pillars**: cgo→Panama (GG-0022), plugin (GG-0051), the
upgrade toolchain (GG-0035/0039–0043/0056), GOTOOLCHAIN/`go env`/vet reuse
(falls out of T1's wrapper driver).

## Working rules (replaces the parts of AGENTS.md that assumed a different box)

- One git worktree + branch per lane under `~/dev/graalgo-wt/`; `master` is
  integration-only (fast-forward).
- Toolchains: `export PATH=~/tools/go/bin:~/tools/graalvm-jdk-25/bin:$PATH
  JAVA_HOME=~/tools/graalvm-jdk-25`.
- Heavy commands under `nice -n 10`.
- Every behavior-affecting change needs a test that fails without it.
