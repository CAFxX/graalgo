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
    `phases/phase1/frontend/` (code lane 2, 2026-10-13); currently a single
    Go package (the `gofrontend` tool). The package split below is a
    follow-up refactor lane (blocked by an exporter/desugar import cycle).
    - [parser/](../internal/frontend/parser/) — future home: the gofrontend
      JSON consumer (`export.go`).
    - [types/](../internal/frontend/types/) — future home: type checking,
      type tables.
    - [desugar/](../internal/frontend/desugar/) — future home: multi-result
      lowering, generics instantiation, synthetic package construction.
  - [ir/](../internal/ir/) — the intermediate representation: JSON schema and
    AST node definitions shared by frontend and backend.
  - [backend/](../internal/backend/) — IR → Truffle nodes. The Java backend
    landed here in the Phase-4 restructure (lane 3); each module has its own
    `Makefile` (plain `javac`, `-Werror`). The runtime module compiles the
    joint round first (its sources plus the truffle module's — the two form
    a compile cycle), then the truffle module compiles the 8 frozen stubs
    against it, then interop builds against both; root `make build` drives
    the three in that dependency order.
    - [truffle/](../internal/backend/truffle/) — Truffle AST node
      implementations (the compiled program):
      [src/main/java/](../internal/backend/truffle/src/main/java/) holds
      `graalgo.{nodes,values,frontend}` from `phases/phase1` plus the 8
      frozen phase-1 stubs (`graalgo/runtime/` × 7,
      `graalgo/safepoint/Safepoint`); the Truffle jars live in
      [lib/](../internal/backend/truffle/lib/); the launchers
      [graalgo-run](../internal/backend/graalgo-run) and
      [graalgo-sync-run](../internal/backend/graalgo-sync-run) sit alongside.
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
      p3reflect,p3sync,p3time,p3tls,p3unsafe,p3x509}` (stdlib bindings).
  - [driver/](../internal/driver/) — build orchestration: module-aware
    builds, build caching (toolexec-equivalent), package resolution. This is
    what makes `graalgo build` a drop-in for `go build` in Phase 4.
- [stdlib/](../stdlib/) — Go standard library support.
  - [synthetic/](../stdlib/synthetic/) — synthetic package implementations.
    Every package documents **why** it is synthetic and **what unblocks the
    real one** (root cause in graalgo to fix instead).
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
- [docs/](../docs/)
  - [REPO_STRUCTURE.md](../docs/REPO_STRUCTURE.md) — this map.
  - [arch/](../docs/arch/) — architecture documentation, consolidated from
    the research-era [SPEC.md](../SPEC.md), [PHASES.md](../PHASES.md), and
    [FEASIBILITY.md](../FEASIBILITY.md).
  - [dev/](../docs/dev/) — contributor guide and architecture decision
    records (ADRs; research-era ones move here from
    [adr/](../adr/) and [phases/phase3/adr/](../phases/phase3/adr/)).
  - [spec/](../docs/spec/) — language conformance notes (the research-era
    [spec/](../spec/) content, curated).

> Migration note: the code-move lanes have not landed yet, so the legacy
> phase-organized layout ([phases/](../phases/), [tools/](../tools/),
> [requirements/](../requirements/), [adr/](../adr/),
> [research/](../research/), [reviews/](../reviews/),
> [spikes/](../spikes/) and the root-era docs [SPEC.md](../SPEC.md),
> [PHASES.md](../PHASES.md), [FEASIBILITY.md](../FEASIBILITY.md)) still
> coexists with this skeleton. New work starts in the new layout; no new
> files go into `phases/`.

## Old → new mapping

For contributors from the research era. Old paths are historical references —
they are not links because the moved/deleted paths no longer exist at the
new locations.

| Today | Becomes |
|---|---|
| `phases/phase1/frontend/` | [internal/frontend/](../internal/frontend/) |
| `phases/phase1/src/main/java/{nodes,values,frontend}/` | [internal/backend/truffle/src/main/java/](../internal/backend/truffle/src/main/java/) |
| `phases/phase1/src/main/java/{runtime,launcher,safepoint}/` | [internal/backend/runtime/src/main/java/](../internal/backend/runtime/src/main/java/) |
| the 8 frozen phase-1 stubs (`graalgo/runtime/` × 7, `graalgo/safepoint/Safepoint`) | [internal/backend/truffle/src/main/java/](../internal/backend/truffle/src/main/java/) (compile with the phase-1 module; shadowed at load by the phase-2 replacements) |
| `phases/phase2/src/main/java/{sched,jfr,runtime,safepoint}/` | [internal/backend/runtime/src/main/java/](../internal/backend/runtime/src/main/java/) (scheduler keeps package `graalgo.sched`) |
| `phases/phase3/src/main/java/{iface,p3*}/` | [internal/backend/interop/src/main/java/](../internal/backend/interop/src/main/java/) |
| `phases/phase1/lib/` (Truffle jars) | [internal/backend/truffle/lib/](../internal/backend/truffle/lib/) |
| `phases/phase1/graalgo-run`, `phases/phase3/graalgo-sync-run` | [internal/backend/](../internal/backend/) |
| `phases/phase3/stdlib-fork/` + synthetics | [stdlib/](../stdlib/) |
| `phases/phase3/corpus/` | [test/corpus/](../test/corpus/) (landed 2026-10-13) |
| `tools/diffharness/` | [test/differential/](../test/differential/) (landed 2026-10-13) |
| `phases/phase3/src/p3frontend/` | [internal/backend/interop/p3frontend/](../internal/backend/interop/p3frontend/) (landed 2026-10-14) |
| `phases/phase3/stdlib-fork/` (tooling) | [tools/stdlib-fork/](../tools/stdlib-fork/) (landed 2026-10-14) |
| `phases/phase3/fuzzing/` | [tools/fuzz-soak/](../tools/fuzz-soak/) (landed 2026-10-14) |
| `phases/phase1/corpus/`, `phases/phase2/corpus/` | [test/corpus/phase1/](../test/corpus/phase1/), [test/corpus/phase2/](../test/corpus/phase2/) (landed 2026-10-14) |
| `phases/phase2/tools/torture/` | [tools/torture/](../tools/torture/) (landed 2026-10-14) |
| `SPEC.md`, `PHASES.md`, `FEASIBILITY.md` | [docs/arch/](../docs/arch/) (one consolidated arch doc) |
| `requirements/`, `adr/`, `spikes/`, `reviews/`, `research/` | [docs/dev/](../docs/dev/) (ADRs kept) or archived out of the repo |
| `spec/` | [docs/spec/](../docs/spec/) (curated) |
| `hidden_files/` | removed — evidence lives in commit messages |
| `phases/*/out/`, `*.err`, compiled binaries | removed — build artifacts, covered by [.gitignore](../.gitignore) |
| `phases/` (all) | **removed 2026-10-14** — migration complete, zero files remain |

## Principles

1. **Function, not phase.** Directory names describe what the code does, not
   which milestone produced it. Scaffolding is not architecture.
2. **One binary, one entry point.** [cmd/graalgo/](../cmd/graalgo/) is the
   product; [internal/](../internal/) exists to serve it.
3. **No research debris ships.** Experiment logs, wave reports, scratch
   files → commit messages or deleted. If it does not serve the tool, it does
   not stay in the tree.
4. **Stdlib honesty.** Every package under
   [stdlib/synthetic/](../stdlib/synthetic/) names the root cause that keeps
   the real stdlib source from compiling, and what fixes it. A synthetic
   package without that note is a placeholder and gets treated as one.
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
