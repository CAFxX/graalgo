# P3-M1 design — `p3frontend`: multi-file package compilation for frontend/v1

**Status:** design accepted by implementer 2026-10-06; implementation follows
this document. Normative parents: `docs/arch/phases/phase3/PLAN.md` §§2–5 (WS A),
`docs/spec/INTERFACE_FREEZE.md` (v1, in force), `docs/arch/phases/phase2/DONE-GATES.md`.

## 1. Problem

`frontend.ExportFile` (frozen, Phase 1) type-checks **one** `.go` file whose
only imports are the synthetic `runtime` and `fmt` packages. Phase 3 must
compile whole multi-file, multi-package programs. The Phase-1 freeze
forbids modifying any Phase-1 file, so the exporter cannot be taught about
directories or real imports. `p3frontend` therefore compiles a package
directory into **one merged `package main` source file** — an encoding of
the multi-package program into exactly the single-file language the frozen
exporter already accepts — and delegates to `frontend.ExportFile`.

## 2. Architecture

```
ExportDir(dir)
  ├─ loader.go    SelectFiles(dir): *.go, no *_test.go, build-tag filtered
  │               ResolveImport(fromDir, path): synthetic | stdlib | module-local
  │               LoadClosure(mainDir): dep-ordered local packages, cycle-checked
  ├─ importer.go  p3Importer (types.Importer): synthetic runtime/fmt/sync (exact
  │               frozen signatures, reimplemented); local packages type-checked
  │               recursively with go/types (cached); stdlib → clear scope error
  ├─ flatten.go   rename every dep package-level identifier with
  │               p3i_<sha256hex8(importpath)>_; rewrite selectors via go/types
  │               Info.Uses (shadowing-safe); drop dep import specs; emit one
  │               merged `package main` file (deps in dependency order, then main)
  └─ export.go    ExportDir = LoadClosure → type-check → Flatten → go/format →
                  write temp file (fixed basename) → frontend.ExportFile → JSON
```

Each layer is independently testable: the loader against the `go/build`
oracle, the importer against synthetic-signature equality, the flattener
differentially against stock gc.

### 2.1 loader.go — file selection and import resolution

**`SelectFiles(dir) ([]string, error)`** returns sorted basenames of the
`.go` files that the Go toolchain would build for linux/amd64:

1. Skip names beginning with `_` or `.`, and `*_test.go` (test files are
   never part of a build; the M1 corpus has no test driver — that is a
   later milestone).
2. File-name suffix filtering mirroring `go/build.goodOSArchFile`:
   strip the extension, ignore everything before the first `_`, drop a
   trailing `test` segment, then require a trailing `_GOOS`, `_GOARCH`,
   or `_GOOS_GOARCH` (against the known-OS/arch tables from go1.27.1) to
   match linux/amd64.
3. Build-constraint filtering with our own evaluator over the file's
   leading comment block: a `//go:build` line, if present, controls
   exclusively (multiple lines are an error, as in `go/build`), and is
   honored wherever it appears in the header — even with no blank line
   after it. Otherwise every `// +build` line in the trimmed header
   region must evaluate true; the region ends at the last blank line
   before the package clause, so a `// +build` line with no following
   blank line is ignored — exactly `go/build`'s `parseFileHeader`/`shouldBuild`
   behavior, verified file-for-file against the `go/build` oracle.
   Expressions are parsed with
   the standard `go/build/constraint` package (which handles both syntaxes)
   and evaluated against the tag set
   `{linux, amd64, gc, unix} ∪ {go1.1 … go1.27}`.
   Legacy `// +build` lines are supported (same parser; `constraint.Parse`
   accepts both forms, and the space after `//` is optional).

**`Loader`** carries the GOROOT (default `~/tools/go-1.27.1`, overridable
via `GRAALGO_GOROOT`) and two caches: import-path → resolution, and
directory → module root.

**`ResolveImport(fromDir, path) (*PackageRef, error)`** classifies:

- `"runtime"`, `"fmt"`, `"sync"` → synthetic marker (never flattened; the import spec
  is kept and resolved by the frozen overlay inside `ExportFile`).
- Otherwise, if `$GOROOT/src/<path>` is a directory → stdlib marker with
  the resolved dir + selected file list (resolution is proven; *compiling*
  it is a scope error — see §2.2).
- Otherwise, module mode: walk up from the main package dir to the nearest
  `go.mod`, read the module path, and map `modpath/rest` → `<modroot>/rest`
  (must exist). Anything else is an unresolvable-import error naming the
  path.

**`LoadClosure(mainDir)`** parses only the import specs of the main
package's files and walks local imports recursively, returning packages in
**dependency order** (post-order DFS; ties broken by import-path sort for
determinism). A package found twice on the current DFS stack is a fail-fast
error containing `import cycle` and naming the full path
(`a -> b -> a`); the error can never hang because the walk is iterative
over a visited set plus an explicit stack.

### 2.2 importer.go — the graduated overlayImporter

`p3Importer` implements `types.Importer`:

- `"runtime"` → synthetic package with the exact frozen signatures
  (`Goexit()`, `SetFinalizer(obj, finalizer any)`, `KeepAlive(x any)`,
  `GC()`), reimplemented from the frozen spec (export.go).
- `"fmt"` → synthetic package with exactly the seven D4 symbols and their
  exact variadic signatures (`Print/Printf/Println` → `(int, error)`,
  `Sprint/Sprintf/Sprintln` → `string`, `Errorf` → `error`).
- `"sync"` → synthetic package with the exact P3-M2b signatures
  (ADR-0002): `Mutex`/`RWMutex`/`WaitGroup`/`Once` with pointer-receiver
  methods (`Lock/Unlock`, `RLock/RUnlock`, `Add(int)/Done/Wait`,
  `Do(func())`) plus the defined-but-out-of-scope `TryLock`/`TryRLock`/
  `RLocker`/`WaitGroup.Go`. The mirror is pinned by
  `TestSyncSyntheticSignatures`/`TestSyncMethodSetComplete`; the frozen
  exporter (not p3frontend) lowers the calls to `syncnew`/`syncop` wire
  nodes and raises the named `not in M2 scope` errors
  (`TestSyncScopeErrorParity`).
- `"errors"` → synthetic package with the exact gc 1.27.1 signatures
  (P3-M7 wave-1): real-Go `New`/`Is`/`Unwrap`/`Join`/`AsType`; `As`
  keeps its exact signature with a fail-fast body (needs `reflect`).
  Pinned by `TestErrorsSyntheticSignatures` (drift vs the phase-1 source,
  including the generic `AsType`).
- `"sort"` → synthetic package with the exact gc 1.27.1 signatures
  (P3-M7 wave-1): faithful pdqsort/stable port plus search; `Find`/
  `Slice`/`SliceStable`/`SliceIsSorted` keep exact signatures with
  fail-fast bodies (WS D multi-result / `reflect`). Pinned by
  `TestSortSyntheticSignatures`.
- Local packages → resolved via the loader and type-checked with
  `go/types` recursively (results cached by import path; the recursion
  carries its own cycle stack as defense in depth behind `LoadClosure`).
- Stdlib packages → resolution succeeds (dir + file list, proving the
  GOROOT mapping), but `Import` returns a clear scope error:
  `stdlib package compilation lands in the P3-M7+ waves`. Compiling the
  real stdlib is out of M1 scope by plan; failing fast with a named error
  beats a confusing `types.Config` failure.

The importer records the `types.Info` (Defs/Uses) per type-checked package;
the flattener consumes it.

### 2.3 flatten.go — package flattening (the export encoding)

For each non-main dependency in dependency order, **every** package-level
identifier — types (including method receivers, which ride along with
their renamed receiver type), funcs, vars, consts — is renamed to
`p3i_<first-8-hex-of-sha256(importpath)>_<name>`. The 8-hex prefix is a
content-derived namespace: deterministic, collision-resistant across
packages, and human-debuggable (`p3i_9f2ac41b_Parse`).

All uses are rewritten using `go/types` `Info.Uses`, which makes the
rewrite **shadowing-correct**:

- A bare identifier whose resolved object is a renamed package-level
  object is renamed in place — this covers dot-imports
  (`import . "x"`: `F()` → `p3i_…_F()`).
- A selector `pkg.F` whose `Sel` resolves to a renamed object has the
  whole selector replaced by the renamed identifier (`pkg.F` →
  `p3i_…_F`). Selectors resolving to anything else (methods, struct
  fields) keep their selector; their `X` operand is still visited, so
  `T.M` becomes `p3i_…_T.M` when `T` is renamed.
- Declaration identifiers (`Info.Defs`) of renamed objects are renamed.
- Local identifiers that merely shadow a package-level name resolve to
  their local object and are untouched.
- Embedded fields (`struct{ T }`): go/types records the field's type
  identifier in both `Defs` (the synthetic field `*types.Var`) and `Uses`
  (the `*types.TypeName`) — the rewriter prefers the type, so the
  declaration is renamed; selectors naming the field (`o.T`, including
  promoted selectors) and keyed literal elements (`Outer{T: ...}`) resolve
  in `Uses` to the field `*types.Var` and are renamed to the renamed type
  name. Without this, an embedded field would silently rebind to a
  colliding local type of the same name.

Import specs for flattened packages are dropped (blank imports
`import _ "x"` are flattened for side effects, then dropped; the M1
`init()` rule in §4 makes this honest). `runtime`/`fmt` specs are kept and
deduplicated.

The merged file is one `*ast.File`: `package main`, the kept imports
(sorted, deduplicated), then dep declarations in dependency order (files
sorted within each package, declarations in source order), then main's
declarations. Global initializers are emitted in this order; the frozen
exporter's global-init pass is topological and stable by declaration
order — i.e. it reproduces gc's source-order tie-breaking — so emitting
deps-before-main in dependency order reproduces gc's package
initialization order for the programs in M1 scope. One explicit
non-guarantee: the relative initialization order of side-effecting
initializers in mutually-unconstrained packages follows import-path sort,
which matches gc's OBSERVED behavior, not a spec guarantee (the Go spec
leaves it unspecified).

Determinism: every ordering input is sorted, the prefix is a pure
function of the import path, and the file is rendered with `go/format`.

### 2.4 export.go — `ExportDir`

```
ExportDir(dir):
  1. SelectFiles(dir); parse package clauses → must be exactly `package main`
     (a dep named "main", or a main dir not named "main", is a scope error).
  2. LoadClosure(dir) → dependency-ordered local packages.
  3. Type-check every package with p3Importer (records Info).
  4. Reject any `func init()` in any package — see §4.
  5. Flatten → merged source bytes (already gofmt'd).
  6. Write to <tmpdir>/p3merged.go (fixed basename → deterministic
     `package.files`; the tmpdir itself never enters the document).
  7. Return frontend.ExportFile(tmpfile).
```

`MergeDir(dir) (string, error)` exposes steps 1–5 for tests that need the
merged source (differential runs).

## 3. Why flattening (and not modifying Phase 1)

1. **The freeze.** `INTERFACE_FREEZE.md` v1 is in force: Phase-1 files are
   never modified, and every FROZEN change needs an ADR plus a version
   bump. Teaching the exporter about directories or real imports would be
   exactly such a change.
2. **Flattening is a post-typecheck encoding, not a language change.**
   The merged file is ordinary Go in the exporter's existing single-file
   scope; no wire-format, CLI, or semantic change is involved.
3. **Differential validation.** Because the merged program is type-checked
   twice (once per package with full import resolution, once as the merged
   file by the frozen exporter), any encoding bug surfaces as a gc
   divergence in the differential tests — the encoding is *falsifiable*,
   which a modified exporter would not be without re-running the whole
   Phase-1 suite.

## 4. Empirical finding: `func init()` is accepted but never executed

Probed 2026-10-06 with the frozen toolchain: `gofrontend` type-checks
`package main` with `func init()` (exit 0; `init` appears as an ordinary
`funcdecl` in the document), but `graalgo-run` prints `INIT DID NOT RUN`
where stock gc 1.27.1 prints `INIT RAN` (probe: `/tmp/initprobe2.go`;
evidence in `hidden_files/m1/`). The launcher only looks up a funcdecl
named `main`; nothing invokes `init`. This is a **silent gc divergence**,
so M1 fails fast instead of flattening through it:

- Any `func init()` in the main package or any flattened dependency is a
  scope error: `p3frontend: package "…": func init() is not supported in
  M1 (frontend/v1 exports init functions but the runtime never executes
  them)`.
- Consequently blank imports are supported only for their (non-init)
  global-initializer side effects; a blank-imported package containing
  `init` hits the same error. This is the documented M1 limitation —
  a clear scope error, never a silent gap.

## 5. GOROOT vs vendoring (M1 note)

M1 resolves stdlib import paths against the **go1.27.1 source tree**
(default `~/tools/go-1.27.1`, `GRAALGO_GOROOT` override) purely to prove
the resolution mapping (dir + file list) and to fail fast with the named
scope error when a program tries to *compile* a stdlib package. The
in-repo vendoring pipeline (stdlib re-vendored atomically with `go/*`
per SPEC §3.2) is P3-M6 work; M1 must not invent its own vendored copy.

## 6. Scope errors (fail-fast, named)

| Condition | Error |
|---|---|
| import cycle (a↔b, self-import) | `p3frontend: import cycle: a -> b -> a` |
| main dir's package clause ≠ `main` | `p3frontend: …: expected package main, found "x"` |
| flattened dep named `main` | `p3frontend: dependency "…" declares package main` |
| unresolvable import | `p3frontend: cannot resolve import "…" (not stdlib, not in module "…")` |
| compiling a stdlib package | `p3frontend: import "strings": stdlib package compilation lands in the P3-M7+ waves` |
| `func init()` anywhere | `p3frontend: package "…": func init() not supported in M1 …` |
| multiple `//go:build` lines | `p3frontend: …: multiple //go:build lines` (mirrors go/build) |

## 7. Testing

- `TestBuildTagFiltering` (`verifies: GG-0023`): synthetic corpus +
  real `src/os`, `src/strings` — our `SelectFiles` must equal the
  `go/build` oracle file-for-file.
- `TestImportCycle` (`verifies: GG-0012`): a↔b and self-import cycles →
  error contains `import cycle`; wall-clock timeout proves fail-fast.
- `TestSyntheticBootstrapRetained` (`verifies: GG-0012`): single-file
  dir importing `runtime`+`fmt` — `ExportDir` doc deep-equals
  `ExportFile` on the same file (modulo `package.files` basenames).
- `TestTwoPackageDifferential` (`verifies: GG-0012`): `testdata/twopkg`
  (`main` + two-file `helper` with linux-tagged and windows-excluded
  files; `main` also carries a `T` colliding with `helper.T` and a
  package-level `Counter` shadowing `helper.Counter`) — a built gc binary
  vs `graalgo-run --gmp=1` on the merged file: byte-identical stdout,
  same exit code; panic variant → identical `panic:` line (stacks differ
  by design — JVM Unsafe warnings on stderr), exit 2.
- `TestNoLanguagePinning` (`verifies: GG-0013`): `//go:build go1.27`
  included by the evaluator; generics in a `go 1.27` module pass
  `ExportDir` — no version gating anywhere.
- `TestEmbeddedFieldRebinding`: embedded-field fixtures colliding with
  main's own `T` — direct selectors, promoted selectors, keyed literals;
  asserts the exact `h.T` output plus gc byte-equality (a rebinding bug
  is silent: both legs exit 0).
- `TestGofrontendTimeout`: a never-exiting stand-in binary proves
  `runGofrontend` kills the exporter after `gofrontendTimeout` with a
  named timeout error instead of hanging.
- `TestGofrontendErrorPrefix`: exit-2 stderr surfaces with the frozen
  binary's duplicate `gofrontend: ` wrapper prefix collapsed.
- `TestDeterministic`: `ExportDir` twice → byte-identical.
- `TestInitRejected`: `func init()` in main and in a dep → the named
  "not supported in M1" scope error (never a silent divergence).
- `TestStdlibScopeError`: `import "strings"` resolves via `ResolveImport`
  (proving the GOROOT mapping) then fails fast with the "P3-M7+ waves"
  scope error.
- `TestResolveImportNestedModules`: the resolve cache is keyed by
  (fromDir, path) — a nested-module resolution must not leak into the
  parent module.
- `TestMultipleGoBuildLines`: two `//go:build` lines are an error,
  mirroring `go/build`.
- Red-green-first (evidence in `hidden_files/m1/redgreen.md`): (a) break
  the selector rewrite → differential test must fail with a gc
  divergence; (b) force-include `*_windows.go` → build-tag test must
  fail vs the oracle.


## 8. Design deviation (recorded)

The task brief says "call frozen `frontend.ExportFile`". The
`graalgo-frontend` module is `package main` **by construction** — it is the
`gofrontend` CLI service and therefore unimportable by any Go module (the
Go toolchain forbids importing main packages). Reaching `ExportFile` as a
Go call would require either modifying Phase-1 sources (forbidden by the
freeze) or vendoring its 3400-line exporter (forbidden by the brief).

`p3frontend` therefore consumes the frozen Go API through its frozen CLI
contract (`INTERFACE_FREEZE.md` §2.1): the `gofrontend` binary's stdout is
`ExportFile`'s return value byte-for-byte (`main.go` is a thin
`fmt.Println(ExportFile(arg))` wrapper; exit 2 surfaces the stderr with
one layer prefix — the frozen `main.go` wraps `ExportFile`'s error as
`gofrontend: <err>` while `ExportFile`'s own errors already start with
`gofrontend: `, so `runGofrontend` collapses the duplicate wrapper prefix
and reports `p3frontend: gofrontend: <err>`). A hung exporter is killed
after a generous timeout (`gofrontendTimeout`, 10 minutes) with a named
timeout error rather than hanging `ExportDir` forever. Binary resolution
mirrors `graalgo.launcher.ToolPaths`:
`GRAALGO_GOFRONTEND`, then `<repo>/internal/frontend/gofrontend`,
then `PATH`. The `require graalgo-frontend` + `replace` in `go.mod`
records the dependency explicitly for readers and `go mod graph`; note
that `go mod tidy` drops the `require` (the module is `package main` and
never imported) while keeping the `replace` — that is expected, and the
durable record of the binary dependency is this section, not `go.mod`.
This is the same consumption surface the Phase-2 launcher uses — no new
coupling is introduced.