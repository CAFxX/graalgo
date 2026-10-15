# Bridge Inventory & Phased Removal Plan

**Date:** 2026-10-15
**Directive:** [No hacks, no bridges — proper full implementations only](~/MEMORY.md) (2026-10-15).
The compiler and runtime stop adding bridges and start removing them.
**Status:** Planning document. No code changes. Not integrated to master.

A **bridge** is anything that is not the real implementation: synthetic stdlib
packages, import overlays, package-name-keyed special cases, workarounds, shims.
This document inventories every bridge in the tree, names the root-cause gap
forcing each one, and orders their removal by leverage, feasibility, and
dependencies.

Inventory method: three parallel tracks over worktree HEAD `1b615875`
(read-only). Raw tables: `/tmp/bridge-inv-synth.md` (Track A),
`/tmp/bridge-inv-java.md` (Track B), `/tmp/bridge-inv-overlay.md` (Track C).

---

## 1. Executive summary

| Bridge class | Count | Size |
|---|---|---|
| Synthetic Go stdlib packages (registry `syntheticSources`) | 133 paths | **162,408 lines** of Go in 130 `synth_*.go` files |
| Types-level stubs in `export.go` (`fmt`, `sync`, `sync/atomic`, `runtime`, `unsafe`) | 5 | signature-only, no bodies |
| Java backend pure reimplementations of stdlib logic (Category A) | 7 areas | **≈ 8,639 lines** |
| Java JDK-delegating bridges (Category B) | 7 classes | **≈ 5,759 lines** |
| Already deleted on the real-fmt lane, pending merge (Category C) | 6 files | 4,739 lines |
| Borderline runtime-package ports (Category D) | 3 files | ≈ 413 lines |
| Import overlay switch cases | 137 | 5 parallel hand-maintained allow-list copies |
| Package-name-keyed special-case sites | ~20 | frontend + backend |
| Live workarounds/shims | ~10 | test driver, godebug, WS-D, AES/SHA3/JSON |

**Combined substitution surface: ≈ 162k lines of Go + ≈ 14.4k lines of Java
standing in for the real Go 1.27.1 standard library.**

The single dominant forcing gap is **WS-D multi-result** (~105 packages):
the frontend v1 wire format cannot emit or call multi-result functions, so
every 2+-result API is carried by synthetic ports plus the Appendix-M desugar
machinery. The linchpin unlocks are **first-class multi-result** and **real
reflect**.

---

## 2. Bridge catalog

### 2A. Synthetic Go stdlib packages (frontend)

Registry: `syntheticSources` in
[internal/frontend/synth_machinery.go](internal/frontend/synth_machinery.go)
(133 paths). Payloads: 130 `internal/frontend/synth_*.go` files, 162,408 lines.
Five more packages resolve through hand-minted types-level stubs in
[internal/frontend/export.go](internal/frontend/export.go):
`syntheticFmtPackage`, `syntheticSyncPackage`, `syntheticAtomicPackage`,
`syntheticRuntimePackage`, `syntheticUnsafePackage` (empty).

Grouped by forcing gap (full per-package table in `/tmp/bridge-inv-synth.md`):

| Forcing gap | Packages | What proper implementation requires |
|---|---|---|
| WS-D multi-result / Appendix-M desugar | ~105 | First-class N-result functions in the frontend wire format + backend codegen; then compile real gc sources |
| Scope-limited / fail-fast subsets | ~56 | Fill the real package scope (io, reflect, godebug, DNS resolver, …) |
| reflect/unsafe-dependent surface | ~38 | Real reflect (in flight); expanded `unsafe` intrinsic coverage |
| asm in gc sources | ~36 | Assembler for the IR, or compile gc's pure-Go generic fallbacks via build tags |
| `internal/*` not admitted by overlay | ~35 | General package loading for all paths; delete the synthetic/disk split |
| `//go:linkname` | ~12 | General linkname support for real runtime symbols |
| Java delegation (tls, x509, http client, time Timer/Ticker, os/syscall/net traps) | 6 | Runtime hooks (poll, timers, nanotime) — or conscious policy keep per 2026-10-10 directive |

Dead on disk (unregistered, never referenced): `synth_filepath.go`
(`path/filepath`, 1,355 lines) and `synth_math_rand.go` (superseded minimal
`math/rand` stub). Delete immediately.

24 payload files are machine-generated (`gen_synth_*.py` × 20,
`assemble_synth.py`, `gen_tables.py`, `gen_sort_variants.py`, 5 upstream
generators). Regeneration is mechanical once the real sources compile.

### 2B. Java backend reimplementations

**Category A — pure Java ports of stdlib logic (delete; ≈ 8,639 lines):**

| Area | Classes | Lines | Blocked on |
|---|---|---|---|
| `reflect` | `p3reflect/ReflectTypes, ReflectOpNode, ReflectRefs, ReflectValues, ReflectCalls` | 4,505 | Real reflect compiling (Phase 2 in flight) |
| `time` | `p3time/GoTimerT, P3TimeRuntime, TimeDescriptors, TimeChan, GoTickerT, TimeWire` + 8 wire nodes | 1,275 | Runtime nanotime/timer hooks |
| `sync` / `sync/atomic` | `runtime/sync/GoMutex, GoRWMutex, GoWaitGroup, GoOnce, GoPool, GoSyncMap`, atomic cells, `p3sync` dispatch nodes | 1,881 | Runtime semaphore/parking hooks |
| `strconv`/`unicode` fragments | `runtime/GoQuote, GoIsPrint, GoFloatFmt` | 978 | Real `strconv` + `unicode/utf8` compiling |

**Category B — JDK-delegating bridges (≈ 5,759 lines, no logic duplicated):**
`p3os/GoOs` (1,634), `p3net/GoNet` (991), `p3net/GoNetpoll` (956),
`p3x509/GoX509` (939), `p3tls/TlsRuntime` (797), `p3crypto/GoCrypto` (261),
`p3net/GoHttp` (181). Authorized by the 2026-10-10 directive ("prefer
delegating to JDK libraries when semantically compatible"). Removal is a
**policy decision**, not just a compile fix — see §4 Phase 4.

**Category C — already deleted on the real-fmt lane (4,739 lines, pending merge):**
`runtime/Fmt.java`, `runtime/FmtScan.java`, `frontend/FmtCallNode.java`,
`iface/FmtHandleMethods.java`, `iface/FmtMethodDispatch.java`,
`FmtVerbMatrixTest.java` (commit `6eb1695e`, real-fmt2 branch).

**Category D — borderline runtime ports (≈ 413 lines):** `BuiltinPrint`,
`GoErrors`, `PanicRendering` (ports of gc `runtime/print.go`, `panic.go`,
`error.go`). The graalgo runtime *is* Java; these are arguably the runtime
itself. Document as permanent, not bridges.

### 2C. Import overlay

`overlayImporter.Import` ([export.go:348–691](internal/frontend/export.go)):
137 switch cases — 5 special-built stubs + 132 `getSyntheticPackage` ports.
Gated by `needsOverlayImporter`, `checkImportAllowed`, a qualified-ident
expression allow-list, and `exportSyntheticPkgCall` — **the same path list in
five parallel hand-maintained copies**. Every new path (most recently
`crypto/x509/pkix` in `1b615875`) is hand-added to all of them: a drift
factory, and the exact "adding bridges" pattern the directive bans.

Proper implementation: general package loading for ALL paths (stdlib +
modules), deleting the synthetic/disk split in
[internal/frontend/diskimport.go](internal/frontend/diskimport.go).
First step (mechanical, immediate): collapse the five copies into one table.

### 2D. Special cases (package-name-keyed logic)

~20 sites. Full table in `/tmp/bridge-inv-overlay.md` §2. Notable:

- `fmt.X` → `fmtcall` wire node; `atomic.*` → `atomicaddr` node; `runtime.*`
  call-shape switch (`Goexit`, `SetFinalizer`, `NumCPU`, `GOMAXPROCS`, …)
- `(*runtime.Func).Name()` → hard-coded panic
- Per-package intrinsic name tables (`time`, `math`, `reflect`, `sync`,
  fips140-`alias`, `internal/abi`) intercepted in **two** call paths
- `context`/`runtime`/`time` multi-result plans in `classifyMultiResultCall`;
  six multi-result desugar tables keyed by ~35 package paths
  (`m7MultiResultFuncs`, `m8MultiResultFuncs`, `osMultiResultFuncs`,
  `m9MultiResultFuncs`, `ioMultiResultFuncs`, `m8IfaceMultiResultMethods`)
- `nistCurve` func-field table for `crypto/ecdh` (general mechanism landed;
  table must go — hunt-special-cases lane)
- `sync.Map` multi-result scope rule; `sync`/`sync/atomic` type-name
  classifiers; backend `NodeFactory.java` type-name switch on `sync.*`

The multi-result desugar tables are **language-construct ABI handling** (same
function, different calling convention) — legitimate class per standing order —
but they are still per-path tables that vanish with real N-result support.

### 2E. Live workarounds & shims

- `graalgo test` driver synthesizes a `graalgoT` **testing.T shim** (strips the
  real `testing` import, AST-rewrites `testing.T` → `graalgoT`) plus a minimal
  `sprintf`/`joinArgs` fmt reimplementation. Proper impl: real `testing`
  package support in the test driver.
- `godebug` shims pinning defaults in ed25519/rsa/mathrand/neturl synthetics.
- WS-D workarounds inside synthetic AES/SHA3/JSON.
- `1b615875` **deleted** `crypto.SignMessage` from the synthetic package as a
  workaround for unsupported 2-result interface calls — deleting API surface
  to dodge a compiler gap is a bridge; the gap (interface multi-result in all
  positions) must be fixed instead.
- `__cut` / `__urlPathJoin` stdlib reimplementations inside synthetic net/url.
- Killed already: the banned `fmt.Fprint` → `w.Write` desugar (`41ad340`);
  only an unused struct field + comment fossil remain.

---

## 3. Root-cause forcing gaps (why the bridges exist)

| # | Gap | Bridges forced | Proper fix |
|---|---|---|---|
| G1 | **WS-D: no first-class multi-result** in frontend wire format / backend | ~105 synthetic packages; Appendix-M desugar; 6 per-path MR tables; `context`/`runtime`/`time` MR plans; `SignMessage` deletion | N-result functions end-to-end in wire format + codegen |
| G2 | **reflect is stubs** | ~38 packages (encoding/*, errors.As, sort.Slice*, fmt); 4,505-line Java reflect port | Real reflect (Phase 1 landed, Phase 2 active) |
| G3 | **asm in gc sources** | ~36 packages (math, crypto hashes/AES, hash/*, bytealg SIMD) | IR assembler, or build-tag selection of gc's pure-Go generic fallbacks |
| G4 | **unsafe intrinsic coverage** | strconv bit casts, strings/bytes/slices fast paths | Expand `exportUnsafeIntrinsic` to cover gc's unsafe patterns |
| G5 | **no runtime hooks** (semaphore/parking, nanotime/timers, poll) | Java sync (1,881), Java time (1,275), os/syscall/net/poll delegation | Runtime parking, timer heap integration, netpoller hooks |
| G6 | **synthetic/disk package-loading split** | 137-case overlay; 5 parallel allow-lists; `internal/*` exclusion (~35 pkgs) | General loader for all paths; admit `internal/*` normally |
| G7 | **linkname to real runtime symbols** | ~12 packages; Java trap tables | General `//go:linkname` resolution against the real runtime |

---

## 4. Phased removal plan

Ordered by (a) leverage — unblocks the most bridges, (b) feasibility,
(c) dependencies. Each phase is a set of parallel lanes; phases sequence on
genuine dependencies only.

### Phase 0 — land what's in flight (no new work)

- Merge real-fmt2 (deletes 4,739 Java lines + synthetic fmt stub +
  `internal/graalgo/fmtstate`; real `fmt` compiles). Blocked only on the
  `%p`/D5 design decision.
- Complete real reflect Phase 2 (unblocks G2).
- hunt-special-cases lane: remove `nistCurve` table, revert the
  `crypto/x509/pkix` 3-site carve-out (`1b615875`), kill remaining
  name-keyed interception.

### Phase 1 — the linchpins (highest leverage)

1. **G1: first-class multi-result** in the frontend wire format + backend
   codegen. Retires the Appendix-M desugar, the 6 per-path MR tables, the
   per-package MR plans, and unblocks ~105 synthetic packages. Effort: L.
2. **G2: real reflect end-to-end** (finish Phase 2, compile real `reflect`
   unmodified, delete the 4,505-line Java port). Unlocks encoding/*, errors,
   sort, fmt fully. Effort: L (in flight).

### Phase 2 — mechanical broad-stdlib (parallelizable per-package lanes)

3. Collapse the five allow-list copies into **one table** (S, immediate —
   stops drift while the overlay still exists).
4. **G3: asm strategy.** For each of ~36 packages: compile the real gc
   source selecting pure-Go generic fallbacks via build tags (the pattern
   the synthetics already validated); build a real assembler only where gc
   has no fallback. Per-package lanes, highly parallel. Effort: M total.
5. **G4: unsafe intrinsic coverage** for gc's unsafe patterns (strconv,
   strings/bytes/slices). Effort: M.
6. Delete dead synthetics: `synth_filepath.go`, `synth_math_rand.go` (S,
   immediate).

### Phase 3 — runtime-deep packages

7. **G5a: sync/sync/atomic** — runtime semaphore/parking hooks; delete
   1,881 Java lines + synthetic stubs. Effort: XL.
8. **G5b: time** — runtime nanotime/timer hooks; delete 1,275 Java lines +
   synthetic port. Effort: L.
9. **G5c: os/syscall/net/internal/poll** — runtime poll hooks; delete GoOs /
   GoNet / GoNetpoll delegation. Effort: XL. (Alternative: consciously keep
   JDK delegation per the 2026-10-10 directive — but then document it as
   permanent architecture, not a bridge.)

### Phase 4 — policy, tail, and shims

10. **JDK-delegation decision** (tls, x509, http client, crypto hashes): keep
    as authorized permanent strategy or compile real Go. His call.
11. **runtime/unsafe stubs**: document as permanent implementation-defined
    surface (like any Go implementation's runtime package), not bridges.
12. **Test-driver `testing.T` shim**: real `testing` support in the driver;
    delete `graalgoT` + `sprintf`/`joinArgs`.
13. **Godebug shims, WS-D workarounds** (AES/SHA3/JSON), `__cut` /
    `__urlPathJoin`: remove as G1/G2/G4 close the underlying gaps.
14. **G6/G7**: general package loader (delete the 137-case overlay and the
    synthetic/disk split); general linkname resolution.

### Completion criteria

- `syntheticSources` registry is empty; the overlay `Import` switch is deleted.
- No package-name-keyed conditionals in `internal/frontend/` or
  `internal/backend/` (mechanical grep gate in `make check`).
- `make all` green with real Go 1.27.1 stdlib sources compiled unmodified.
- `STDLIB_DUPLICATION.md` retires to "no open items".

---

## 5. What is NOT a bridge (do not touch)

- **Language-construct desugarings** (for-range, multi-return ABI lowering,
  `go f(args)`, channel range): lowering Go syntax to IR core is legitimate
  compiler work.
- **Runtime machinery**: scheduler, GC interop, channels, defer, intrinsics
  (`Intrinsics.java`), `p3unsafe`, `p3math` asm-analogue nodes, linkname
  dispatcher, `rtstack`.
- **`graalgo/fronted/Json.java`**: wire-format reader, not `encoding/json`.
- **Category D runtime ports** (`BuiltinPrint`, `GoErrors`, `PanicRendering`):
  the graalgo runtime is Java; porting gc's `runtime/*.go` is the runtime
  itself.

---

## 6. Process guardrails (from the directive)

1. **No new bridges.** When something doesn't work: fix the compiler/runtime
   properly, never add a bridge. Every lane brief carries this line.
2. **No new special cases** in frontend/backend for stdlib or user code.
3. **Fixes are structural, never name-keyed** ([AGENTS.md](AGENTS.md) §4).
4. **Zero duplication of stdlib logic** — everywhere ([AGENTS.md](AGENTS.md)).
5. Each retired bridge deletes its code in the same change that lands the
   proper implementation; `make snapshot-check` guards behavior drift.
