# ADR-0007 addendum — M7+ disk-path asm fail-fast: DESIGN (DRAFT)

**Status:** DRAFT — pending approval. Do not implement until approved.
**Date:** 2026-10-19
**Lane:** `rev-asm-failfast` (independent review 2026-10-19-07, P1)
**Normative parents:** [ADR-0007](ADR-0007-asm-strategy.md) (esp. the M6
remediation note, obligation 1), SPEC §2.8 (GG-0009), ADR-0004 (intrinsic
surface / linkname registry), [docs/arch/frontend-v1.md](../../arch/frontend-v1.md).

## 1. Verdict on the obligation

The obligation is **not obsolete**. The gap is live today, with a concrete
smoking gun:

- `reflect` is disk-compiled (`internal/frontend/synth_machinery.go:111`:
  `"reflect": syntheticReflectSource, // RETIRED - uses disk importer`).
- `reflect/asm_amd64.s` is inventoried `unsupported`
  (`tools/stdlib-fork/asm-inventory-go1.27.1.json`; reason:
  "makeFuncStub/callReflect stubs assume the gc stack layout and register
  ABI; reflect is reimplemented on Truffle in the wave-2 bring-up --
  per-file strategy then").
- `buildDiskPackage` (`internal/frontend/diskimport.go:273`) reads only
  `bp.GoFiles`; `bp.SFiles` (which go/build populates — verified
  empirically: with the forced `purego` tag, `sync/atomic` still reports
  `SFiles=[asm.s]`, `internal/bytealg` 5, `internal/cpu` 2, `hash/crc32` 1,
  `reflect` 1) is never consulted. Zero references to `CheckAsmFiles` /
  `AsmResolution` / `SFiles` in non-test `internal/frontend/*.go` outside
  `p3frontend/` (verified by grep).
- Consequence: `reflect`'s bodyless `makeFuncStub()` / `methodValueCall()`
  (`~/tools/go/src/reflect/makefunc.go:80,143`, implemented by the two
  `TEXT` symbols in `asm_amd64.s`) fall through to the generic
  `"unimplemented"` panic stub
  (`internal/frontend/export.go:6250`) — not the ADR-mandated build-time
  error naming file+package+category+reason. This is exactly the
  silent-ignore failure mode ADR-0007 exists to prevent.

No superseding ADR is needed. This design fulfills the obligation.

## 2. Investigation findings (grounded)

1. **The `purego` tag does not discharge the obligation.** `buildDiskPackage`
   forces `purego` on (`diskimport.go:281-283`), which changes *Go-file*
   selection (pure-Go fallbacks over asm-declared bodies). go/build still
   returns the `.s` files in `bp.SFiles` — gc would still assemble them.
   `purego` is a *selection* mechanism; the inventory is a *disposition*
   mechanism. They compose; one does not replace the other.
2. **The p3frontend mechanism cannot be called directly.** `p3frontend` is a
   separate Go module that consumes gofrontend (`internal/frontend`,
   `package main`) via the frozen CLI contract (`p3frontend/go.mod`,
   `p3frontend/DESIGN.md` §8). The dependency runs p3frontend → gofrontend,
   never the reverse. `buildDiskPackage` needs its own path to the
   inventory logic.
3. **Selection is already done for gofrontend.** `ctxt.ImportDir(dir, 0)`
   returns `bp.SFiles`: the `.s`/`.S`/`.sx` files gc would assemble for the
   build context's GOOS/GOARCH/tags — precisely ADR-0007 D7's "files gc
   would assemble". No new selection logic is needed (unlike p3frontend,
   which reimplements selection because it doesn't use go/build).
4. **Scope must exclude third-party code.** `resolveDiskPackageDir` also
   resolves module-cache/GOPATH dirs. The inventory covers only
   `GOROOT/src` pinned to go1.27.1. Third-party asm (e.g. `segmentio/asm`,
   named in the `GRAALGO_BUILD_TAGS` comment at `diskimport.go:279-281`)
   is a *documented* purego-fallback pattern; failing it fast would regress
   a supported feature. p3frontend's outside-GOROOT fail-fast is correct
   *there* (module-local packages; any asm is unsupported); it is wrong
   *here*.
5. **Blast radius if the check lands today** (probe: every `GOROOT/src`
   dir through the purego-forced `build.Default` context, SFiles resolved
   against the inventory):
   - Would FAIL FAST: `reflect` (`asm_amd64.s`, unsupported),
     `hash/crc32` (`crc32_amd64.s`), `syscall` (`asm_linux_amd64.s`),
     `runtime/cgo` (`gcc_amd64.S`), `vendor/golang.org/x/sys/cpu`,
     plus `cmd/*` toolchain internals (never compiled by graalgo).
   - Would PROCEED: evaporate 28 files, intrinsify 7, emulate-then-intrinsify
     2, jvm-math 6 (across the selected linux/amd64 set).
   - Of the failing set, only `reflect` is disk-compiled today
     (`hash/crc32`, `syscall` are still synthetic; `runtime/cgo` is out of
     scope — cgo banned per SPEC §3.7 GG-0058; `cmd/*`/`vendor/*` are never
     imported). **Enabling the hook as specified turns `reflect`'s build
     red immediately** — which is the fail-fast working as designed, but it
     is a build-breaking change that must be sequenced (§5).
6. **The wire tolerates additive fields.** `Doc` (`export.go:66-84`) is the
   frontend/v1 envelope; `tools/validate_frontend_v1.py` (at
   `internal/frontend/tools/`) only asserts required fields, so a new
   top-level section is wire-compatible. The contract doc is
   `docs/arch/frontend-v1.md` (the `docs/frontend-v1.md` path in comments
   is stale — tree reorganization).
7. **Backend's current bodyless handling** (`NodeFactory.java:649-684`):
   `"linkname"` → `LinknameRootNode` (symbol-keyed dispatch, structural);
   `"unimplemented"` → `UnimplementedRootNode(name, pkgPath)` panic stub.
   No asm awareness anywhere in `internal/backend`.

## 3. Design

### D1. Shared mechanism package: `internal/frontend/asmstrategy`

New package in the `graalgo-frontend` module owning the inventory and the
*resolution* half of the mechanism. Selection stays with each frontend
(p3frontend: `SelectAsmFiles`; gofrontend: `bp.SFiles`) — resolution is
shared data + policy, selection is frontend-specific.

```go
package asmstrategy // internal/frontend/asmstrategy

type AsmCategory string // evaporate | intrinsify | emulate-then-intrinsify | jvm-math | unsupported (+ uncategorized, never stored)
type AsmEntry struct { Path, Package, Category string; Lines int; Replacement, Reason string }
type AsmResolution struct { File, Package, Category, Replacement, Reason string }

type Inventory struct { /* parsed inventory + go version */ }
func LoadEmbedded() (*Inventory, error)          // from zz_asm_inventory_gen.go; asserts go_version == "go1.27.1"
func (in *Inventory) CheckAsmFiles(dir, importPath string, sFiles []string /* basenames */, goroot string) ([]AsmResolution, error)
```

Semantics of `CheckAsmFiles` (ported from p3frontend's `asm.go`, including
its fidelity note on `.S`/`.sx`):

- For each basename in `sFiles`: absolutize under `dir`; require it under
  `<goroot>/src` (else: fail fast, `uncategorized`, outside-GOROOT reason —
  same text as today).
- Resolve the slash-relative path against the inventory: missing entry →
  fail fast `uncategorized` (toolchain drift); `unsupported` → fail fast
  with `entry.Reason`. Error format kept byte-identical to ADR-0007 D7:
  `asm fail-fast: <path> (package "<pkg>"): category "<category>": <reason>`
  (prefix `gofrontend:` / `p3frontend:` per caller — the format after the
  prefix is the contract).
- Replaced categories → `AsmResolution`; additionally **assert
  `Replacement != ""`** (structural integrity: a replaced category with an
  empty replacement pointer is a generator/inventory bug, fail fast).
- The inventory is a constructor parameter in spirit: `LoadEmbedded()` is
  the production constructor; tests build small inventories directly
  (no singleton globals — testability; p3frontend's `sync.OnceValues`
  package-level loader is refactored into this shape).

The generated file `zz_asm_inventory_gen.go` **moves** here (single
generated artifact). The generator (`tools/stdlib-fork/gen-asm-inventory.py`)
gains `--package` / `--out`; its stale `phases/phase3/...` header paths
become `tools/stdlib-fork/...`; `make generate` emits the new location and
`--check` keeps it in sync (existing CI `git diff --exit-code` gate covers
it).

### D2. The `buildDiskPackage` hook + scoping rule

In `buildDiskPackage`, immediately after `ctxt.ImportDir` (diskimport.go,
after line ~293), before any Go file is parsed:

```go
// ADR-0007 (M6 remediation, obligation 1): every .s file gc would assemble
// for this package must resolve to an inventory disposition. purego (forced
// on above) selects Go fallbacks; it does not disposition the .s files in
// bp.SFiles, which gc would still assemble. Unsupported/uncategorized files
// fail the build here -- never silently ignored.
if asmstrategy.IsStdlibDir(dir) { // dir has prefix runtime.GOROOT()/src/ -- structural, see below
    inv, err := asmstrategy.LoadEmbedded()
    if err != nil { return nil, err }
    asmResolutions, err = inv.CheckAsmFiles(dir, importPath, bp.SFiles, runtime.GOROOT())
    if err != nil { return nil, err }
}
```

Scoping rule (structural, not name-keyed): the check applies **iff `dir`
is under `<runtime.GOROOT()>/src/`** — the same GOROOT
`resolveDiskPackageDir` uses for stdlib and the root the inventory is
pinned to. Third-party dirs (module cache, GOPATH) keep today's behavior
(purego fallback selection; bodyless decls → the generic `unimplemented`
marker, loud at call time). Rationale: the inventory cannot disposition
what it does not cover, and failing third-party asm would regress the
documented `GRAALGO_BUILD_TAGS=purego` pattern. This scoping is recorded in
the `buildDiskPackage` comment and in this ADR addendum — it is a
deliberate boundary, not a gap.

`IsStdlibDir` is a path-prefix predicate on the already-resolved `dir`;
no package-name lists, no per-package switches.

### D3. Uncategorized + toolchain-drift guards

- Uncategorized (inventoried-root file, no entry): fail fast, same format,
  category `"uncategorized"` — per ADR-0007 D1 ("the absence of an
  inventory entry ... fails the build identically").
- `LoadEmbedded` asserts `payload.GoVersion == "go1.27.1"`
  (== `runtime.Version()` of the building toolchain); mismatch → error at
  first use. Defense in depth alongside the generator's `--check`; makes
  toolchain drift loud at build time instead of miscategorizing.

### D4. Wire: additive per-package `asm` section

`Doc` gains (export.go):

```go
// Asm lists the Plan 9 assembly dispositions (ADR-0007) for each
// from-disk dependency package that had buildable .s files. Additive;
// absent for synthetic packages and when empty.
Asm []AsmPackage `json:"asm,omitempty"`

type AsmPackage struct {
    Package string       `json:"package"` // import path
    Files   []AsmFileRes `json:"files"`
}
type AsmFileRes struct {
    File        string `json:"file"`        // base name
    Category    string `json:"category"`    // stored category, never "uncategorized"
    Replacement string `json:"replacement"` // inventory replacement pointer
    Reason      string `json:"reason"`
}
```

- The exporter collects each disk package's resolutions during
  `importPkgTree` and emits them sorted by import path (deterministic).
- `docs/arch/frontend-v1.md` documents the section; `validate_frontend_v1.py`
  gains a positive selftest case (validator already ignores unknown fields;
  the doc update is the contract change).
- This is the "payload is carried" half of ADR-0007's M6 note: resolutions
  are no longer discarded at the frontend boundary.

Per-decl `.s` attachment (scanning `TEXT` symbols and mapping them to
bodyless Go decls) was considered and **deferred**: it needs a `.s`
lexical scanner plus a TEXT→Go-symbol mapping rule for edge cases
(linknamed symbols, ABI wrappers). Package-level resolutions give the
backend everything needed for precise diagnostics (§D5) without it.
Revisit with inventory schema v2 (§7).

### D5. Backend consumption (structural — no name-keyed logic)

`NodeFactory` / `BinaryDecoder` (Java):

1. Parse the `asm` section into `Map<String /*pkgpath*/, List<AsmResolution>>`.
2. When building `UnimplementedRootNode` for a bodyless decl, look up
   `fields["pkgpath"]` in that map (data-driven, by the decl's own package
   field — not a package-name switch). If present, the panic message
   becomes:
   `unimplemented: <pkgpath>.<name>: assembly dispositions for this package: [<file>: <category>: <reason>]`
   instead of the generic stub message. The on-call failure now names the
   exact `.s` file, its decided category, and the reason — the diagnostic
   half of "consume the replacement payloads".
3. Program-build-time integrity assertion: every carried resolution has a
   non-empty `replacement` for replaced categories (frontend guarantees;
   backend re-asserts — a dangling replacement pointer is a wave-planning
   bug, fail the program build, not the first call).
4. Resolutions are retained in program metadata for wave tooling (the
   waves' work list, per ADR-0007 D1).

What the backend does **not** do: no per-symbol provider binding from the
free-text `replacement` pointers (they are not machine-readable), no
package-name-keyed branches. Provider binding continues through the
existing structural paths: `//go:linkname` → `LinknameRootNode`
(symbol-keyed registry, ADR-0004), pure-Go fallbacks selected by the
frontend's `purego` context. Machine-verified provider binding needs
inventory schema v2 (structured provider refs) — deferred with trigger
(§7), not parked: the trigger is named.

### D6. Generator changes

`tools/stdlib-fork/gen-asm-inventory.py`:
- `--package` (default `asmstrategy`), `--out` (default
  `internal/frontend/asmstrategy/zz_asm_inventory_gen.go`).
- Header comment paths fixed to `tools/stdlib-fork/` (stale
  `phases/phase3/` references).
- `--check` verifies the new location; `make generate` wired; no other
  behavior change (categories, fail-on-uncategorized stay).

### D7. p3frontend adapter

- `p3frontend/asm.go` keeps its test-pinned API (`Loader.CheckAsmFiles`,
  `Loader.ResolveAsmFile`, `SelectAsmFiles`, `isAsmFile`, the
  `AsmCategory`/`AsmEntry`/`AsmResolution` types re-exported or aliased)
  and delegates resolution to `asmstrategy` (import via the existing
  `replace graalgo-frontend => ../../../internal/frontend`).
- Its `zz_asm_inventory_gen.go` is deleted (single generated artifact lives
  in `asmstrategy`).
- `asm_test.go` keeps passing against the adapter (selection + error-format
  pins stay); resolution table tests move to / are duplicated in
  `asmstrategy`'s own tests.
- Freeze note: `INTERFACE_FREEZE.md` §2.1 freezes the gofrontend *service*
  contract (CLI/JSON). Importing a *new, non-main* library package does not
  touch the frozen surface. Flagged for approver confirmation; fallback is
  dual emission (§6, alternative b).

### D8. Sequencing — the reflect gate (HARD PREREQUISITE)

Enabling the D2 hook as specified makes `reflect`'s disk build fail
(`asm_amd64.s`, `unsupported`, reason: wave-2 reimplementation pending).
The inventory itself defers reflect's per-file strategy to wave-2
("re-test trigger: reflect wave"). Therefore:

**The hook MUST NOT be enabled until the wave-2 reflect strategy for
`makeFuncStub`/`methodValueCall` is decided** (backend ABI0 trampoline
intrinsic? recategorization? deliberate scope cut with a loud error?).
Enabling it undecided turns `make all` red for every reflect-importing
program with no recourse — the fail-fast working as designed, but a
build-breaking change that needs its decision first.

Sequencing: (1) approve this design; (2) decide reflect's disposition
(separate wave-2 design, prerequisite); (3) implementation lane: mechanism
+ hook + wire + backend + tests; (4) flip the switch with reflect decided.
`hash/crc32`/`syscall` are still synthetic today — their waves (D6
triggers) will hit the fail-fast at retirement time, which is the intended
forcing function: retiring a synthetic without deciding its asm is now a
build error, not a silent gap.

### D9. Tests (implementation lane)

- `asmstrategy`: resolution table tests (all five categories,
  uncategorized, outside-GOROOT, empty-replacement rejection,
  go-version mismatch), plus the generator round-trip (`--check`).
- `buildDiskPackage`-level: compile `sync/atomic` and `internal/cpu`
  from disk → success, resolutions carried; `internal/bytealg` likewise.
  Fixture test for the fail-fast: a temp GOROOT-shaped dir is not possible
  against the embedded inventory — instead, test `CheckAsmFiles` with a
  test-constructed `Inventory` containing an `unsupported` entry and
  `sFiles` naming it → exact ADR error format asserted (file+package+
  category+reason).
- p3frontend: existing `TestAsm*` suite green against the adapter.
- Wire: `validate_frontend_v1.py --selftest` extended with an `asm`-section
  positive case; `docs/arch/frontend-v1.md` updated in the same change.
- Red-green: neutering the D2 hook (or the `IsStdlibDir` gate) must turn
  the new fail-fast tests red with "build proceeded silently".
- Differential: `make snapshot-check` — no drift on the previously-green
  corpus (the hook only *adds* failures for previously-silent cases; with
  the reflect gate decided, the corpus is unaffected).

### D10. Structural-compliance statement

No name-keyed shortcuts anywhere in this design: scoping is a GOROOT path
prefix (§D2); categories come from the inventory data (§D1); the backend
looks up resolutions by each decl's own `pkgpath` field (§D5); provider
binding stays in the existing symbol-keyed linkname/intrinsic registry.
The `runtime_` prefix rule in `exportLinknameDecl` is pre-existing and
untouched.

## 4. Alternatives considered (and rejected)

- **(a) Obligation is obsolete / supersede ADR-0007:** rejected — §1 shows
  the gap is live (`reflect` today).
- **(b) Dual-emit the generated inventory (one copy per frontend):**
  fallback if the approver rejects the `asmstrategy` import from p3frontend
  (§D7). Rejected as primary: two copies of generated bytes invite
  "which is canonical" confusion; the generator is already the single
  source, the package should be too.
- **(c) gofrontend shells out to / vendors p3frontend's asm.go:** rejected —
  wrong dependency direction (p3frontend → gofrontend module); copying the
  file duplicates the logic the shared package eliminates.
- **(d) Also scan for `.s` files go/build did *not* select (constraint-
  excluded):** rejected — files failing `shouldBuild` are not "buildable";
  ADR-0007 D7 scopes to files gc would assemble, i.e. `bp.SFiles`. (The
  `.S`-in-non-cgo nuance inherits go/build's behavior, matching gc —
  `IgnoredOtherFiles` are not assembled by gc either.)
- **(e) "Audit mode" (log, don't fail) for rollout:** rejected — a
  non-failing mode is the silent anti-pattern ADR-0007 was written to kill.
  The reflect gate (§D8) is the rollout mechanism, not a weaker check.
- **(f) Per-decl TEXT-symbol scanning now:** deferred (§D4), not rejected —
  revisit with schema v2.

## 5. Explicitly deferred (with triggers, not a parking lot)

1. **Machine-verified provider binding** (inventory schema v2: structured
   `provider_kind`/`provider_symbol` alongside the free-text pointer).
   Trigger: first crypto intrinsify-leg intrinsic lands (ADR-0007 D4) or
   the reflect trampoline design (§D8) — whichever first needs the backend
   to bind a decl to a named provider from inventory data.
2. **Third-party (non-GOROOT) asm strategy.** Today: purego fallbacks +
   generic `unimplemented` (loud at call time). Trigger: first third-party
   dep whose bodyless decl is hit at runtime with no pure-Go fallback.
3. **Recheck of category counts on toolchain upgrade** — already owned by
   ADR-0007 D6 (`gen-asm-inventory.py --check`); the `LoadEmbedded`
   version assertion (§D3) adds build-time defense.

## 6. Decision points for the approver

1. **Shared `asmstrategy` package** (D1) vs dual emission (4b) — imports a
   new non-main package of the frozen gofrontend module from p3frontend.
2. **GOROOT-prefix scoping rule** (D2) — third-party asm stays on
   purego+`unimplemented`; confirm this boundary is acceptable.
3. **Reflect gate** (D8) — confirm the hook stays off until the wave-2
   reflect strategy for `makeFuncStub`/`methodValueCall` is decided, and
   who owns that decision.
4. **Wire shape** (D4) — top-level `asm` section on `Doc`, `omitempty`.
5. **Backend consumption depth** (D5) — diagnostics + integrity assertion +
   metadata now; provider binding deferred to schema v2 (§5.1).

## Appendix: blast-radius probe (2026-10-19)

Method: every `GOROOT/src` dir through `build.Default` + forced `purego`
(linux/amd64), `bp.SFiles` resolved against
`tools/stdlib-fork/asm-inventory-go1.27.1.json` (639 files).

| outcome | packages |
|---|---|
| FAIL FAST (unsupported) | `reflect` (`asm_amd64.s`), `hash/crc32` (`crc32_amd64.s`), `syscall` (`asm_linux_amd64.s`), `runtime/cgo` (`gcc_amd64.S`), `vendor/golang.org/x/sys/cpu`, `cmd/*` internals |
| PROCEED | evaporate 28 files, intrinsify 7, emulate-then-intrinsify 2, jvm-math 6 |

Of the failing set, only `reflect` is disk-compiled today; the rest are
synthetic / out of scope / never imported. Probe script:
`/tmp/blast_probe.go` (ephemeral; method recorded here).
