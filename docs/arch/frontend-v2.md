# frontend/v2 — proper parser → AST pipeline

**Status:** design / in-progress (long-lived parallel track, started 2026-10-15).
**Goal:** Replace the bodge-y v1 frontend with a clean parser → AST → backend
pipeline. No synthetic packages, no package-name special cases, no stdlib-call
desugaring, no import overlay, no JSON wire format.
**Scope:** The new frontend accepts **ALL Go syntax from the start** — the
complete Go 1.27 language, including generics (type parameters, instantiation,
constraints), not a subset. If `go/parser` accepts it, v2 handles it.
**No JSON:** v2 emits no JSON. The emitter builds an in-memory `*Document`
(stage 3 below), and the Go→Java boundary is a **versioned binary
serialization** of that Document: [`internal/frontend/v2/encode/`](../../internal/frontend/v2/encode/)
encodes it (`"g2bi"` magic, tagged values, zigzag-varint integers,
length-prefixed strings/collections; `FORMAT_VERSION`, currently 1, rejects
skew loudly), and the Java backend decodes it with
[`BinaryDecoder`](../../internal/backend/truffle/src/main/java/graalgo/frontend/v2/BinaryDecoder.java)
before
[`NodeFactoryV2.buildProgram(byte[] data)`](../../internal/backend/truffle/src/main/java/graalgo/frontend/v2/NodeFactoryV2.java)
constructs the Truffle nodes. The binary wire replaced the v1 JSON transfer
across the Go→Java process boundary (his 2026-10-15 order: "that should
obviously skip the json"). It is **not** a direct in-process Go→Java API —
that is a later step, via circular compilation of the v2 Go code so it calls
`NodeFactoryV2` in-process (see "Next" in
[`internal/frontend/v2/PROGRESS.md`](../../internal/frontend/v2/PROGRESS.md)).
The v2 rules below (no synthetic packages, no name-keyed branches) are about
what the emitter produces, not how the Document crosses the boundary: the
binary format is generic (it encodes the Document's map structure without
per-node-kind hardcoding), so it introduces no special cases of its own.

## 1. Why v2

The v1 frontend (`internal/frontend/`, the `gofrontend` tool) grew organically
through Phase 1–3 and accumulated structural debt:

| Problem | v1 reality | v2 rule |
|---|---|---|
| Synthetic stdlib | 142 `synth_*.go` files reimplementing stdlib | Real packages from disk only |
| Import overlay | Synthetic packages shadow real imports | Direct GOROOT/disk resolution |
| Desugaring workarounds | stdlib calls rewritten (e.g. `fmt.Fprint` → `w.Write`) | Emit what the source says |
| Special cases | Package-name-keyed logic in exporter | No name-keyed branches, ever |
| Monolith | `export.go` is 12,108 lines | Split into parser / checker / emitter stages |

v1 keeps working while v2 is built (parallel track). v2 replaces v1 when it
reaches differential parity on the corpus.

## 2. Pipeline stages

```
.go files → [parse] → ast.Package → [check] → typed info → [emit] → in-memory Document → [encode] → binary wire format → [BinaryDecoder + NodeFactoryV2] → backend nodes
```

### Stage 1 — Parse (`v2/parser/`)

- Uses `go/parser` (standard library) via **circular compilation**: graalgo
  compiles the `go/*` packages (parser, ast, token, etc.) itself, then uses
  them. No Java reimplementation of the Go parser — less code to maintain.
  (His 2026-10-15 order: prefer the circular route.)
- Input: list of `.go` files (same package). Output: `*ast.Package` + `*token.FileSet`.
- Handles build constraints via `go/build` (GOOS/GOARCH, build tags) — the
  same mechanism v1's `diskimport.go` uses, minus the synthetic overlay.
- No type information. No import resolution beyond file selection.

### Stage 2 — Check (`v2/checker/`)

- Uses `go/types` (standard library) via **circular compilation** (same as
  Stage 1 — graalgo compiles `go/types` itself). No custom type system,
  no Java reimplementation.
- **Importer**: resolves imports from the real GOROOT and module cache.
  No synthetic packages. No overlay. If a package cannot be found on disk,
  that is a compile error — not a signal to synthesize a fake.
- **go:linkname**: v1 struggles here (linkname is a gc-internal mechanism).
  v2 handles it the same way the real toolchain does: `go/types` ignores
  the directive for type-checking purposes; the backend resolves the
  actual symbol. If the backend cannot resolve it, that is a backend gap
  to fix — not a frontend workaround to add.
- Output: `*types.Package` + `*types.Info` (defs, uses, selections, types).

### Stage 3 — Emit (`v2/emit/`)

- Walks the typed AST and emits an in-memory `*Document` (no JSON). The
  Document crosses the Go→Java boundary as the binary wire format
  ([`encode/`](../../internal/frontend/v2/encode/) on the Go side,
  [`BinaryDecoder`](../../internal/backend/truffle/src/main/java/graalgo/frontend/v2/BinaryDecoder.java)
  / [`NodeFactoryV2`](../../internal/backend/truffle/src/main/java/graalgo/frontend/v2/NodeFactoryV2.java)
  on the Java side) and is turned into backend nodes there.
- **Deterministic emission order.** `pkg.Files` is a Go map; emission
  visits files sorted by name and declarations sorted by source position,
  so the same package always yields the same function order and
  type/object ids (regression test: `emit/determinism_test.go`).
- **One node type → one emit function.** No 12k-line monolith. The emitter
  is organized by AST node kind, each in its own file.
- **Full Go 1.27 syntax from the start.** Every AST node kind that
  `go/parser` produces must have an emitter — including generics
  (`TypeSpec` with type params, `IndexListExpr`, instantiations),
  all statement types, all expression types, all declarations. No subset.
  If a node kind is unhandled, that is a compile error, not a silent skip.
- **No desugaring of stdlib calls.** If the source says `fmt.Fprint`, the IR
  says `fmt.Fprint`. If the backend cannot handle it, fix the backend.
- **Legitimate lowerings only**: language-level desugaring that `go/types`
  itself does not do but which the backend requires (e.g., multi-result
  handling — but done as a general mechanism, not per-package).

## 3. What v2 does NOT do

- **No synthetic packages.** Not even "temporarily." If a stdlib package
  does not compile, v2 reports the error. The fix goes into graalgo
  (frontend generality, backend capability) — never into a fake package.
- **No package-name special cases.** The emitter never branches on
  `pkg.Name() == "fmt"` or similar. Ever.
- **No import overlay.** The importer's job is to find real packages.
- **No Java reimplementation of stdlib logic.** (That is a backend rule,
  stated here because v2 must not create the need for it.)
- **No panic stubs.** Unsupported constructs are compile errors with
  clear messages, not runtime traps.

The single exception is the already-approved temporary synthetic `syscall`
(his 2026-10-15 order, documented in the bridge-removal tracker). v2 does
not add new ones.

## 4. Migration plan

1. **Bootstrap** (this track, done 2026-10-15): skeleton with all three stages,
   emitting an in-memory Document for a hello-world program (serialized to the
   binary wire format for the Java side). Differential test: behavioral —
   programs built via v2 vs gc on the corpus. v2 emits no JSON, so there is no
   v1-JSON output to compare against.
2. **Parity**: extend node coverage until v2 passes the behavioral differential
   corpus ([`test/corpus/`](../../test/corpus/)) with zero drift vs gc.
3. **Cutover**: when parity is reached, v2 becomes the default frontend.
   v1 is deleted (not kept as a fallback).
4. **Cleanup**: the 142 `synth_*.go` files, the import overlay, and the
   desugaring workarounds are deleted with v1.

## 5. Layout

```
internal/frontend/v2/
  go.mod          — separate module (graalgo-frontend-v2)
  parser/         — stage 1: go/parser wrapper
  checker/        — stage 2: go/types wrapper, real importer
  emit/           — stage 3: typed AST → in-memory *Document (no JSON)
  encode/         — Go→Java binary interface: Encode(*Document) → "g2bi" bytes (versioned via FORMAT_VERSION)
  driver/         — main: CLI (mirrors v1 flags for drop-in replacement)
  testdata/       — corpus inputs for differential testing vs v1
```

v2 is a separate Go module so it can be built and tested independently of
v1. It lives under `internal/frontend/` (not a new top-level dir) because
it *is* the frontend — just the next generation.

## 6. Open questions

- **Generics**: v1 has extensive generic-instantiation machinery
  (`mrdesugar_m5.go`, etc.). v2 should use `go/types`'s native generic
  support and instantiate cleanly. Needs investigation.
- **Init order**: v1 handles `init()` and package-level var initialization
  order. v2 must preserve this (it's observable behavior).
- **Cgo**: v1 punts. v2 punts too (documented limitation, not a bodge).

## 7. References

- [frontend-v1.md](frontend-v1.md) — the IR wire format (unchanged).
- [REPO_STRUCTURE.md](../REPO_STRUCTURE.md) — where things live.
- Bridge-removal tracker: `hidden_files/bridge-removal/` (goal workspace).
