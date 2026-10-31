> # ⚠️ EXPERIMENTAL — DO NOT USE ⚠️
>
> **This is experimental, pre-alpha software, written by AI agents.**
> It is unfinished, unreviewed, and may be broken in ways that are not obvious.
> **Do not use it yet.**

---

# graalgo

graalgo compiles Go to GraalVM Truffle AST nodes — a JIT-compiled Go
toolchain that is a drop-in replacement for `go`.

Quickstart:

```sh
alias go=graalgo
go build ./...
go run main.go
go test ./...
```

`graalgo` supports the `build`, `run`, `test`, `vet`, and `fmt` commands
(see [cmd/graalgo/](cmd/graalgo/) and [internal/driver/](internal/driver/)).

## Repository layout

The repo is organized by **function**, not by research phase. The full map —
directory tree with one-line descriptions, the old→new mapping table, layout
principles, and what is intentionally not in the repo — is
[docs/REPO_STRUCTURE.md](docs/REPO_STRUCTURE.md).

The short version:

- [cmd/graalgo/](cmd/graalgo/) — the binary: `build`, `run`, `test`, `vet`,
  `fmt`.
- [internal/](internal/) — the implementation: `frontend/` (Go source → IR),
  `backend/` (`truffle/`, `runtime/`, `interop/`), `driver/` (build
  orchestration), `vet/`.
- [stdlib/](stdlib/) — stdlib support: `patches/` (minimal diffs vs
  upstream Go). Synthetic stdlib implementations do not live here — they
  are the `synth_*.go` files in [internal/frontend/](internal/frontend/)
  (see [docs/REPO_STRUCTURE.md](docs/REPO_STRUCTURE.md)).
- [test/](test/) — `corpus/`, `differential/` (run vs `gc`, byte-compare),
  `e2e/` (real published modules), `snapshots/` (behavior baselines).
- [docs/](docs/) — `arch/`, `dev/`, `spec/`, and the map itself.

Build and test with [Makefile](Makefile) (`make all`); the agent operating
manual is [AGENTS.md](AGENTS.md).
