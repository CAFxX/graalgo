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

## State (2026-11-18)

Integration branch: `wt/recovery/integration`.

| Lane | State |
|---|---|
| T0.A runtime lifecycle (`wt/recovery/runtime`) | landed |
| T0.B JIT enablement (`wt/recovery/jit`) | landed |
| T0.C test infrastructure (`wt/recovery/testinfra`) | landed |
| T1 design ([toolexec-design.md](toolexec-design.md)), T2 design ([truffle-design.md](truffle-design.md)) | landed |
| ABI spike ([abi-spike.md](abi-spike.md): P1 verbatim reflect, P2 syscall boundary) | completed, both **GO**; amendments applied |
| SPEC amendments (`wt/recovery/spec-amend`) | landed ([ADR-0009](../dev/adr/ADR-0009-recovery-spec-amendments.md)) |
| toolexec W1 launcher + W2 GIR (`wt/recovery/gir-launcher`) | landed |
| T0.D + TR-0 + TR-1 backend structure (`wt/recovery/backend-structure`) | landed |

## Decision log

| Date | Decision | Where recorded |
|---|---|---|
| 2026-11-18 | W4 builds a new idiomatic Truffle node package (DSL specializations, typed families, `TruffleLanguage`-native); v1 node classes retired at M3; +6–10 agent-days on M1a | toolexec-design D12, §14; truffle-design T13 |
| 2026-11-18 | Goroutines in image modes (A1) and GG-0050's "performant without the JIT" (A2): **measure first** — gating work items P-D (`engine.Compilation=false`, 0.5 day) and P-F (native-image thread behaviour, 1 day) before TR-12/T3; SPEC carries both as `[OPEN]` | SPEC §3.5, §3.9; truffle-design §13.2, §14 |
| 2026-11-18 | Host access for `graalgo/java` (A4): embedder's `HostAccess` when embedded; allow-listed public access when standalone and the package is imported. A3, A5, A6 stay pending | SPEC §2.4, §4.3; truffle-design §5.6, §14 |
| 2026-11-18 | Netpoller: runtime epoll poller through the Panama syscall executor, goroutines park via the slot-releasing path; `GG-0018` superseded by GG-0063 | SPEC §3.5, §7.4 |
| 2026-11-18 | Value model flat with per-type leaf tables; `unsafe.Pointer` = (object, gc byte offset), in-object arithmetic defined, escapes are gc `checkptr` fatal errors; `GG-0015`/`GG-0016` superseded by GG-0061/GG-0062 | SPEC §3.4, §7.3 |
| 2026-11-18 | Process creation: `posix_spawn` helper binary shipped in the distribution (GG-0064) | SPEC §3.8 |
| 2026-11-18 | abi-spike §3's ten toolexec-design amendments applied; R1/R2 resolved GO with residual risks; new R15 (memory-unsafe syscall table) | toolexec-design §10, §14 |

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
`Context` (GG-0005/0024), then Go↔Java interop and debugging. TR-0 (tool/TCK
jars, `make pe-check`) and TR-1 (language shell; the product launcher runs
programs through `Context.eval`) landed 2026-11-18 — see
[truffle-design.md §13.4](truffle-design.md).

**T3 — modes** (gated on the P-D and P-F measurements, truffle-design
§13.2): native-image AOT, hybrid, isolates, mode selection
(GG-0006/0049/0050/0060/0025/0026), reachability metadata (GG-0052).

**T4 — remaining pillars**: the syscall executor's poller (GG-0063) and
spawn helper (GG-0064) land with toolexec W8/M4; cgo→Panama (GG-0022), plugin (GG-0051), the
upgrade toolchain (GG-0035/0039–0043/0056), GOTOOLCHAIN/`go env`/vet reuse
(falls out of T1's wrapper driver).

## Working rules (replaces the parts of AGENTS.md that assumed a different box)

- One git worktree + branch per lane under `~/dev/graalgo-wt/`; `master` is
  integration-only (fast-forward).
- Toolchains: `export PATH=~/tools/go/bin:~/tools/graalvm-jdk-25/bin:$PATH
  JAVA_HOME=~/tools/graalvm-jdk-25`.
- Heavy commands under `nice -n 10`.
- Every behavior-affecting change needs a test that fails without it.
