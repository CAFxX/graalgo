> # ⚠️ EXPERIMENTAL — DO NOT USE ⚠️
>
> **This is experimental, pre-alpha software, written by AI agents.**
> It is unfinished, unreviewed, and may be broken in ways that are not obvious.
> **Do not use it yet.**

---

# graalgo

graalgo compiles Go to GraalVM Truffle AST nodes — a JIT-compiled Go
toolchain that is a drop-in replacement for `go`.
Its goal: run long-running Go programs, from CPU-bound utilities to large
servers, significantly faster than gc does
([docs/arch/north-star.md](docs/arch/north-star.md)).

Quickstart:

```sh
alias go=graalgo
go build ./...
go run main.go
go test ./...
```

Today `graalgo` is the **v1** driver, a custom reimplementation of the
`build`, `run`, `test`, `vet`, and `fmt` commands (see
[cmd/graalgo/](cmd/graalgo/) and [internal/driver/](internal/driver/)). It is
pending replacement at M3 by a thin launcher that runs the stock `go`
command with a `-toolexec` wrapper
([internal/toolexec/](internal/toolexec/),
[docs/arch/toolexec-design.md](docs/arch/toolexec-design.md)); the recovery
plan is [docs/arch/RECOVERY.md](docs/arch/RECOVERY.md).

## Repository layout

**Start at [docs/MAP.md](docs/MAP.md)** — where to look for each common
task, the data flow of the new path, and which directories are legacy v1.

The repo is organized by **function**, not by research phase. The full map —
directory tree with one-line descriptions, the old→new mapping table, layout
principles, and what is intentionally not in the repo — is
[docs/REPO_STRUCTURE.md](docs/REPO_STRUCTURE.md).

The short version:

- [cmd/graalgo/](cmd/graalgo/) — the v1 driver binary: `build`, `run`,
  `test`, `vet`, `fmt` (replaced at M3 by the toolexec launcher).
- [internal/](internal/) — the implementation:
  [toolexec/](internal/toolexec/) (the `-toolexec` wrapper, launcher, and the
  GIR typed IR), [frontend/](internal/frontend/) (v1: Go source → JSON IR,
  incl. the frozen `synth_*.go` stdlib replacements),
  [backend/](internal/backend/) ([truffle/](internal/backend/truffle/) —
  nodes + runtime, [gir/](internal/backend/gir/) — Java GIR codec,
  [interop/](internal/backend/interop/)), [driver/](internal/driver/) (v1
  build orchestration), [vet/](internal/vet/) (v1 `graalgo vet` analyzers).
- [stdlib/](stdlib/) — the new path's std fork: `fork/` (replaced
  packages: the facade `runtime`) and `patches/` (minimal diffs vs upstream Go).
- [test/](test/) — `corpus/`, `differential/` (run vs `gc`, byte-compare),
  `e2e/` (real published modules), `snapshots/` (gc behavior baselines),
  `regression/`, `jit/`, `java/`.
- [docs/](docs/) — `arch/`, `dev/`, `spec/`, and the map itself.

Build and test with [Makefile](Makefile) (`make all`); the agent operating
manual is [AGENTS.md](AGENTS.md).
