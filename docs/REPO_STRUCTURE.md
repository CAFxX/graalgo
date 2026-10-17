# Repository structure

graalgo is organized by **function**, not by research phase: nothing in the
tree is named `phase1/2/3`. What graalgo is and how to run it lives in
[README.md](../README.md); how agents work in this repo lives in
[AGENTS.md](../AGENTS.md). This document is the map of where things live and
why, plus the [old → new mapping](#old--new-mapping) for contributors from
the research era.

Related: [Makefile](../Makefile) (root build entry point),
[.github/workflows/](../.github/workflows/) (CI).

## Directory tree

- [README.md](../README.md) — what graalgo is, quickstart (drop-in `go` via
  `alias go=graalgo`).
- [AGENTS.md](../AGENTS.md) — operating manual for agents working in this
  repo.
- [Makefile](../Makefile) — root build entry point. `make all` regenerates
  all generated artifacts and runs the per-push suite; CI then runs
  `git diff --exit-code` — drift fails the build.
- [cmd/](../cmd/)
  - [graalgo/](../cmd/graalgo/) — the product: one binary, one entry point.
    `main.go` implements the driver commands `build`, `run`, `test`, `vet`,
    `fmt` (drop-in for `go`).
- [internal/](../internal/) — private implementation. Nothing outside the
  module imports it (Go `internal` convention); everything user-facing flows
  through [cmd/graalgo/](../cmd/graalgo/).
  - [frontend/](../internal/frontend/) — Go source → IR. Moved here from
    `phases/phase1/frontend/` (code lane 2, 2026-10-13). The main Go module
    (own `go.mod`, `package main`) is the `gofrontend` tool:
    [main.go](../internal/frontend/main.go),
    [export.go](../internal/frontend/export.go) (AST → wire emission),
    [diskimport.go](../internal/frontend/diskimport.go) and
    [mr_disk.go](../internal/frontend/mr_disk.go) (real-stdlib disk imports),
    [mrdesugar_m5.go](../internal/frontend/mrdesugar_m5.go) (multi-result
    lowering), the [`synth_*.go`](../internal/frontend/) synthetic stdlib
    implementations, the `gen_synth_*.py` generators, and the Go test suite.
    Built by [build.sh](../internal/frontend/build.sh); see
    [README.md](../internal/frontend/README.md) and
    [VENDORING.md](../internal/frontend/VENDORING.md). Sibling trees in the
    same directory:
    - [p3frontend/](../internal/frontend/p3frontend/) — separate module (own
      `go.mod`): the phase-3 importer/loader
      ([loader.go](../internal/frontend/p3frontend/loader.go),
      [importer.go](../internal/frontend/p3frontend/importer.go),
      [flatten.go](../internal/frontend/p3frontend/flatten.go),
      [export.go](../internal/frontend/p3frontend/export.go),
      [asm.go](../internal/frontend/p3frontend/asm.go)) with its own test
      suite.
    - [go/](../internal/frontend/go/) — vendored pristine go1.27.1
      `src/go/*`; load-bearing via the `build.sh` overlay, intentional per
      [VENDORING.md](../internal/frontend/VENDORING.md).
    - [javatest/](../internal/frontend/javatest/) — `NodeFactoryTest.java`
      plus `golden_*`/`bad*` fixtures (Java wire-format tests living under
      the Go module dir).
    - [tools/](../internal/frontend/tools/) — `probe/` and
      `validate_frontend_v1.py` (wire-format validation).
    - [parser/](../internal/frontend/parser/),
      [types/](../internal/frontend/types/),
      [desugar/](../internal/frontend/desugar/) — empty placeholders for the
      planned package split (blocked by an exporter/desugar import cycle).
    - [v2/](../internal/frontend/v2/) — separate module (own `go.mod`): the
      frontend v2 rewrite (parser → checker → emitter stages plus the
      Go→Java binary interface in
      [encode/](../internal/frontend/v2/encode/), decoded by
      [BinaryDecoder.java](../internal/backend/truffle/src/main/java/graalgo/frontend/v2/BinaryDecoder.java)
      in the Java backend; wire contract pinned by
      `TestEncodeIntContract`/`TestEncodeGoldenDoc` on the Go side and
      `BinaryDecoderTest` on the Java side; `make test-v2` runs its suite).
  - The IR is the `frontend/v1` JSON wire format: specified in
    [docs/arch/frontend-v1.md](arch/frontend-v1.md), produced by
    [gofrontend](../internal/frontend/) (validated by
    [internal/frontend/tools/validate_frontend_v1.py](../internal/frontend/tools/validate_frontend_v1.py)),
    consumed by `graalgo.frontend.NodeFactory` in the backend. There is no
    separate IR package — the format is the contract.
  - [backend/](../internal/backend/) — IR → Truffle nodes. The Java backend
    landed here in the Phase-4 restructure (lane 3); each module has its own
    `Makefile` (plain `javac`, `-Werror`). The runtime module compiles the
    joint round first (its sources plus the truffle module's — the two form
    a compile cycle), then the truffle module compiles the 8 same-FQN
    shadow classes against it, then interop builds against both; root
    `make build` drives the three in that dependency order.
    - [truffle/](../internal/backend/truffle/) — Truffle AST node
      implementations (the compiled program):
      [src/main/java/](../internal/backend/truffle/src/main/java/) holds
      `graalgo.{nodes,values,frontend}` from `phases/phase1` plus 8
      same-FQN shadow classes
      ([graalgo/runtime/](../internal/backend/truffle/src/main/java/graalgo/runtime/)
      × 7, [graalgo/safepoint/](../internal/backend/truffle/src/main/java/graalgo/safepoint/)).
      The runtime module's phase-2 implementations shadow these at load
      (runtime classes precede truffle classes on the launcher classpaths
      — the same-FQN overlay mechanism). Five are frozen stubs (`Netpoll`,
      `ChanOps`, `Scheduler`, `GcInterop` throw
      `UnsupportedOperationException`; `Safepoint` is a no-op); three are
      full forked implementations with documented deltas (`Panic`: the
      panicnil knob; `GoroutineState`: `ThreadLocal` vthread-scoped holder
      lookup; `Timers`: real `nanotime`, stubbed sleep/timer-heap). The
      Truffle jars live in [lib/](../internal/backend/truffle/lib/); the
      launchers [graalgo-run](../internal/backend/graalgo-run) and
      [graalgo-sync-run](../internal/backend/graalgo-sync-run) sit alongside.
    - [fast-cycle/](../internal/backend/fast-cycle/) — fast debug cycle:
      [FastCycleDaemon.java](../internal/backend/fast-cycle/FastCycleDaemon.java)
      plus the [`fast-cycle`](../internal/backend/fast-cycle/fast-cycle)
      wrapper script; wired into the root Makefile `fast-*` targets
      (`fast-build`, `fast-run`, `fast-test`, `fast-watch`). See
      [README.md](../internal/backend/fast-cycle/README.md).
    - [runtime/](../internal/backend/runtime/) — the Go runtime on the JVM:
      scheduler, netpoller, maps, channels, synchronization:
      [src/main/java/](../internal/backend/runtime/src/main/java/) holds
      phase-1 `graalgo.{runtime,launcher,safepoint}` (minus the stubs, which
      compile in the truffle module) merged with phase-2
      `graalgo.{sched,jfr,runtime,safepoint}` — the scheduler keeps its
      `graalgo.sched` package (sources at `.../src/main/java/graalgo/sched/`).
    - [interop/](../internal/backend/interop/) — Go↔Java interop and
      isolates (also the natural home of Phase 5's privileged runtime
      surface):
      [src/main/java/](../internal/backend/interop/src/main/java/) holds
      phase-3 `graalgo.{iface,p3chan,p3crypto,p3link,p3math,p3net,p3os,
      p3reflect,p3sync,p3time,p3tls,p3unsafe,p3x509}` (stdlib bindings) plus
      [`graalgo.rtstack`](../internal/backend/interop/src/main/java/graalgo/rtstack/)
      (the synthetic `runtime` package's stack-inspection intrinsics:
      `Callers`/`Caller`/`CallersFrames`/`FuncForPC` and the
      `Frames`/`Func` methods) plus
      [`graalgo.rtmemstats`](../internal/backend/interop/src/main/java/graalgo/rtmemstats/)
      (the synthetic `runtime` package's MemStats-family intrinsics:
      `ReadMemStats`/`GOROOT`/`Version`/`Stack`, implemented against JVM
      telemetry) plus
      [`graalgo.p3log`](../internal/backend/interop/src/main/java/graalgo/p3log/)
      (shared diagnostic logging for launch-time warnings; backed by
      `java.util.logging` — raw `System.err` printing is rejected by the
      interop `check` gate).
  - [driver/](../internal/driver/) — artifact emission for `graalgo build`:
    takes the frontend-validated wire JSON and produces a real executable
    binary that embeds it
    ([artifact.go](../internal/driver/artifact.go),
    [artifact_test.go](../internal/driver/artifact_test.go)).
  - [vet/](../internal/vet/) — static analyzers for `graalgo vet`:
    `go vet`-style checks (printf, unreachable, copylocks, etc.) implemented
    against the parsed AST and type information.
- [stdlib/](../stdlib/) — Go standard library support.
  - Synthetic package implementations live in
    [internal/frontend/](../internal/frontend/) (`synth_*.go`, `package main`
    — part of the `gofrontend` binary, not separable packages). Each one
    documents **why** it is synthetic and **what unblocks the real one**
    (root cause in graalgo to fix instead). They are being systematically
    removed per the no-bridges order; see the bridge-removal tracker.
    The 18 generator-produced `synth_*.go` are regenerated by
    [`make gen-synth`](../Makefile) via
    [`internal/frontend/run_gen_synth.py`](../internal/frontend/run_gen_synth.py)
    from the 10 [`gen_synth_*.py`](../internal/frontend/) generators
    (gc 1.27.1 sources, version-pinned); six files additionally apply a
    frontend-constraint adaptation patch from
    [`internal/frontend/gen_synth_adapt/`](../internal/frontend/gen_synth_adapt/).
    The full generator→output mapping lives in the `GENERATED_SYNTH` block in
    the [`Makefile`](../Makefile); CI fails on any drift.
  - [patches/](../stdlib/patches/) — minimal diffs against upstream
    [go/](https://go.dev/dl/) sources. Empty where the real sources compile
    unmodified.
- [test/](../test/) — organized by what each suite proves, not by which phase
  wrote it.
  - [corpus/](../test/corpus/) — differential corpus programs (moved here
    from `phases/phase3/corpus/`, 2026-10-13).
  - [differential/](../test/differential/) — the differential harness: run
    each corpus program under graalgo and under `gc`, byte-compare outputs.
    Moved here from `tools/diffharness/` (2026-10-13).
  - [e2e/](../test/e2e/) — end-to-end: real published Go modules compiled
    as-published (e.g. `github.com/google/uuid`), output matching `go run`.
  - [snapshots/](../test/snapshots/) — committed behavior snapshots
    (`go1.27.1.json`); `make snapshot-check` fails on drift. See
    [test/snapshots/README.md](../test/snapshots/README.md).
  - [regression/](../test/regression/) — regression tests for fixed
    package-level bugs (`pkg031/` covers the PKG-031 strings.Repeat hang);
    run through the graalgo driver + backend via `make test-regression`
    (in `make all`, hence per-push CI).
- [docs/](../docs/)
  - [REPO_STRUCTURE.md](../docs/REPO_STRUCTURE.md) — this map.
  - [arch/](../docs/arch/) — architecture documentation, consolidated from
    the research-era [SPEC.md](arch/SPEC.md), [PHASES.md](arch/PHASES.md), and
    [FEASIBILITY.md](arch/FEASIBILITY.md).
  - [dev/](../docs/dev/) — contributor guide and architecture decision
    records (ADRs; research-era ones moved here from
    [docs/dev/adr/](../docs/dev/adr/)).
  - [spec/](../docs/spec/) — language conformance notes (the research-era
    [spec/](spec/) content, curated).
- [tools/](../tools/) — developer tooling (not the product):
  [stdlib-fork/](../tools/stdlib-fork/) and
  [stdlib-linkdiff/](../tools/stdlib-linkdiff/) (stdlib fork/linkname
  tooling), [run_stdlib_gate.sh](../tools/run_stdlib_gate.sh) and
  [stdlib-excl/](../tools/stdlib-excl/) (wave-corpus differential gates),
  [fuzz-soak/](../tools/fuzz-soak/) and [fuzz/](../tools/fuzz/) (fuzzing),
  [torture/](../tools/torture/) (scheduler torture),
  [godebug-diff/](../tools/godebug-diff/),
  [mutation/](../tools/mutation/), [gate1/](../tools/gate1/),
  [req/](../tools/req/) (requirements CLI), and
  [precommit-check.sh](../tools/precommit-check.sh) (mechanical pre-commit
  guard).
- [requirements/](../requirements/) — live requirements registry
  (`GG-*.yaml`); the [req](../tools/req/) CLI walks up to find it. Moves
  deferred (see the old → new mapping).
- [research/](../research/) — active research tracks:
  [phase5-opt-survey/](../research/phase5-opt-survey/) (Phase-5 work).
  Finished studies are archived under
  [docs/arch/research/](arch/research/).

> Migration complete (2026-10-14): the `phases/` directories have been
> removed from the tracked tree. All active code, docs, and tooling now
> live in the new layout. Historical phase documents are archived under
> [docs/arch/phases/](arch/phases/). Gitignored local build debris (e.g.
> stale `phases/*/out/` on a developer's disk) is not part of the repo and
> is safe to delete.

## Old → new mapping

For contributors from the research era. Old paths are historical references —
they are not links because the moved/deleted paths no longer exist at the
new locations.

| Today | Becomes |
|---|---|
| `phases/phase1/frontend/` | [internal/frontend/](../internal/frontend/) |
| `phases/phase1/src/main/java/{nodes,values,frontend}/` | [internal/backend/truffle/src/main/java/](../internal/backend/truffle/src/main/java/) |
| `phases/phase1/src/main/java/{runtime,launcher,safepoint}/` | [internal/backend/runtime/src/main/java/](../internal/backend/runtime/src/main/java/) |
| the 8 same-FQN shadow classes (`graalgo/runtime/` × 7, `graalgo/safepoint/Safepoint`) | [internal/backend/truffle/src/main/java/](../internal/backend/truffle/src/main/java/) (compile with the phase-1 module; shadowed at load by the runtime module's phase-2 implementations — 5 frozen stubs + 3 forked overlays) |
| `phases/phase2/src/main/java/{sched,jfr,runtime,safepoint}/` | [internal/backend/runtime/src/main/java/](../internal/backend/runtime/src/main/java/) (scheduler keeps package `graalgo.sched`) |
| `phases/phase3/src/main/java/{iface,p3*}/` | [internal/backend/interop/src/main/java/](../internal/backend/interop/src/main/java/) |
| `phases/phase1/lib/` (Truffle jars) | [internal/backend/truffle/lib/](../internal/backend/truffle/lib/) |
| `phases/phase1/graalgo-run`, `phases/phase3/graalgo-sync-run` | [internal/backend/](../internal/backend/) |
| `phases/phase3/stdlib-fork/` + synthetics | [stdlib/](../stdlib/) |
| `phases/phase3/corpus/` | [test/corpus/](../test/corpus/) (landed 2026-10-13) |
| `tools/diffharness/` | [test/differential/](../test/differential/) (landed 2026-10-13) |
| `phases/phase3/src/p3frontend/` | [internal/frontend/p3frontend/](../internal/frontend/p3frontend/) (landed 2026-10-14) |
| `phases/phase3/stdlib-fork/` (tooling) | [tools/stdlib-fork/](../tools/stdlib-fork/) (landed 2026-10-14) |
| `phases/phase3/fuzzing/` | [tools/fuzz-soak/](../tools/fuzz-soak/) (landed 2026-10-14) |
| `phases/phase1/corpus/`, `phases/phase2/corpus/` | [test/corpus/phase1/](../test/corpus/phase1/), [test/corpus/phase2/](../test/corpus/phase2/) (landed 2026-10-14) |
| `phases/phase2/tools/torture/` | [tools/torture/](../tools/torture/) (landed 2026-10-14) |
| `modprobe/` | [test/e2e/](../test/e2e/) (landed 2026-10-14; harness, uuid cases, bug log) |
| `SPEC.md`, `PHASES.md`, `FEASIBILITY.md` | [docs/arch/](../docs/arch/) (one consolidated arch doc) |
| `adr/` | [docs/dev/adr/](../docs/dev/adr/) (landed 2026-10-14) |
| `spikes/` | [docs/arch/spikes/](../docs/arch/spikes/) (landed 2026-10-14; cited by the archived phase docs) |
| `reviews/` | [docs/arch/reviews/](../docs/arch/reviews/) (landed 2026-10-14; cited by the archived phase docs) |
| `research/` study tracks (`T1`–`T8`, OUTLINEs) | [docs/arch/research/](../docs/arch/research/) (landed 2026-10-14); `research/phase5-opt-survey/` stays — active Phase-5 work |
| `requirements/` | still at root — live registry, `tools/req/` walks up to find it; move deferred |
| `spec/` | removed 2026-10-14 — was outline drafts and rework scratch, not curated spec content (which lives in [docs/spec/](../docs/spec/)) |
| `hidden_files/` | removed — evidence lives in commit messages |
| `phases/*/out/`, `*.err`, compiled binaries | removed — build artifacts, covered by [.gitignore](../.gitignore) |
| `phases/` (all) | **removed 2026-10-14** — migration complete, zero tracked files remain |

## Principles

1. **Function, not phase.** Directory names describe what the code does, not
   which milestone produced it. Scaffolding is not architecture.
2. **One binary, one entry point.** [cmd/graalgo/](../cmd/graalgo/) is the
   product; [internal/](../internal/) exists to serve it.
3. **No research debris ships.** Experiment logs, wave reports, scratch
   files → commit messages or deleted. If it does not serve the tool, it does
   not stay in the tree.
4. **Stdlib honesty.** Every synthetic package implementation (in
   [internal/frontend/](../internal/frontend/) as `synth_*.go`) names the root cause that keeps
   the real stdlib source from compiling, and what fixes it. A synthetic
   without that note is a placeholder and gets treated as one.
5. **Tests prove the tool.** [test/](../test/) is organized by what each
   suite proves (corpus programs, differential comparison, end-to-end
   modules), never by authorship.
6. **Every file/dir named in a doc is a link.** Relative paths, resolvable
   from the doc's own directory, mechanically checked by
   [.github/scripts/check_markdown_links.py](../.github/scripts/check_markdown_links.py).

## What's intentionally NOT in the repo

- **Build artifacts** — `out/` directories, `*.err` logs, compiled binaries,
  the gofrontend binary and `overlay.json`. Regenerated by `make all` from
  sources; covered by [.gitignore](../.gitignore).
- **Experiment logs (`hidden_files/`)** — per-run evidence belongs in commit
  messages (which are permanent and searchable), not in untracked scratch
  directories. Must never reappear (see [AGENTS.md](../AGENTS.md)).
- **Vendored copies of upstream Go** —
  [stdlib/patches/](../stdlib/patches/) holds *diffs* against upstream Go
  sources, never forks of the sources themselves. Upstream Go is never
  modified; fixes go into graalgo.
- **Research-era working notes** — wave run logs, per-commit scratch,
  speculative drafts. Consolidated into [docs/arch/](../docs/arch/) and
  [docs/dev/](../docs/dev/), or deleted.
- **Credentials and tokens** — CI uses connector-managed credentials; no
  secrets are ever committed (see [AGENTS.md](../AGENTS.md)).
- **Duplicated sources of truth** — validity rules, generated artifacts, and
  test baselines live in exactly one place each; docs link to the source,
  never copy it.
